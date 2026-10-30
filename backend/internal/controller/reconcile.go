package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"

	"ikhnos/internal/install"
	"ikhnos/internal/relay"
)

// Phases an IkhnosAgent is in.
const (
	PhasePending    = "Pending"    // waiting for something the person has to provide (the token Secret)
	PhaseInstalling = "Installing" // the release is being installed or changed
	PhaseReady      = "Ready"      // the release is installed as the spec asks
	PhaseFailed     = "Failed"     // the installer failed; it is tried again
	PhaseInvalid    = "Invalid"    // the spec cannot be installed from
	PhaseBlocked    = "Blocked"    // another IkhnosAgent in this namespace owns the release
	PhaseDeleting   = "Deleting"   // the release is being uninstalled
)

// Status is what the controller writes back.
type Status struct {
	Phase              string      `json:"phase,omitempty"`
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
	Release            *ReleaseRef `json:"release,omitempty"`
	// ValuesHash is the digest of the values the release was last installed with (see Hash).
	ValuesHash string      `json:"valuesHash,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`
	// Revoked is written by the agent, not the controller, when the server revokes it (see install.RevokedAnnotation for the way older
	// agents said it). The controller keeps it when it writes its own fields.
	Revoked *Revoked `json:"revoked,omitempty"`
	// Requested is the telemetry the server asked this cluster for, written by the agent (see package relay). The controller reads it
	// (relay.go) and keeps it when it writes its own fields.
	Requested *relay.Request `json:"requested,omitempty"`
	// Relay is what became of the request, written by the controller (relay.go); it keeps whatever is there when it has nothing new to say.
	Relay *relay.Result `json:"relay,omitempty"`
	// Workloads is what the release runs and how many pods of each are ready, as the controller last looked (see observe). Only of a release that
	// is installed as the spec asks.
	Workloads []Workload `json:"workloads,omitempty"`
}

// Revoked is the agent's word that the server revoked it.
type Revoked struct {
	Reason string `json:"reason,omitempty"`
}

// ReleaseRef is the installed release.
type ReleaseRef struct {
	Name         string `json:"name"`
	Revision     int    `json:"revision"`
	ChartVersion string `json:"chartVersion"`
}

// Condition follows Kubernetes' own: Ready is true when the release is installed as the spec asks.
type Condition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	Message            string `json:"message,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
}

// Reconciler brings one namespace's IkhnosAgent and release into agreement.
type Reconciler struct {
	Dyn       dynamic.Interface
	Kube      kubernetes.Interface
	Installer Installer
	Namespace string
	// Defaults are Helm values every release gets under whatever the resource says: where this build's images are, for one. They are
	// part of what the release is compared with, so a change in them upgrades it like a change in the resource does.
	Defaults map[string]any
	// MaxTier is the highest access tier this controller installs an agent at (nil: 2, the highest there is). It is the ceiling of the
	// permissions the controller was given: to create the agent's ClusterRoles it must hold what they grant (Kubernetes lets nobody
	// hand out what they do not have), so a controller that is to install only tier 0 or 1 agents is given no more than those need.
	MaxTier *int
	// AllowTelemetry is whether this controller was given the permissions the agent's telemetry needs (cluster-wide reads of what the
	// collectors watch: allowTelemetry in its chart). A resource that turns a signal on is refused without it, for the same reason a tier
	// above MaxTier is: Kubernetes would refuse to create the ClusterRole.
	AllowTelemetry bool
	// RelayTelemetry is whether this controller acts on the telemetry the server asks of the cluster (relay.telemetry in its chart): the
	// agents it installs are told to pass the requests on, and it reads them (see relay.go). Off, a request is answered by the agent at once
	// and the controller reads none.
	RelayTelemetry bool
	// RelayDeadline is how long the agent has to be ready after a request was applied (0: DefaultRelayDeadline); RelayPoll how often that
	// is looked at (0: 2 seconds).
	RelayDeadline, RelayPoll time.Duration
	// ObserveEvery is how often the controller looks at the pods of an installed release (0: DefaultObserveEvery; negative: it looks only when
	// something else makes it look at the resource).
	ObserveEvery time.Duration
	// BootstrapConfigMap names the ConfigMap the chart wrote the first resource's spec into (empty: none; see Bootstrap).
	BootstrapConfigMap string
	Log                *slog.Logger
	Now                func() time.Time

	// warnedRevision is one more than the revision of an old definition the log already said so for (0: none) (so it is said once, not at every look).
	warnedRevision int
}

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// WaitingForSecret is how long to wait before looking again for a token Secret that is not there yet.
const WaitingForSecret = 30 * time.Second

// Reconcile looks at the IkhnosAgent called name and does what it needs. A non-zero duration asks to be called again after it; an error
// is retried with backoff.
func (r *Reconciler) Reconcile(ctx context.Context, name string) (time.Duration, error) {
	res := r.Dyn.Resource(GVR).Namespace(r.Namespace)
	obj, err := res.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	list, err := res.List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}
	if owner := ownerOf(list.Items); owner != obj.GetName() {
		return 0, r.blocked(ctx, obj, owner)
	}
	if obj.GetDeletionTimestamp() != nil {
		return 0, r.finish(ctx, obj)
	}
	if _, revoked := revokedReason(obj); revoked {
		return 0, r.removeRevoked(ctx, obj)
	}
	return r.apply(ctx, obj)
}

// removeRevoked is what the controller does for an agent that says the server revoked it: delete the resource. The finalizer then takes
// the usual way out (finish): the release is uninstalled, and its identity with it. Revoking in Ikhnos leaves nothing of the agent.
func (r *Reconciler) removeRevoked(ctx context.Context, obj *unstructured.Unstructured) error {
	reason, _ := revokedReason(obj)
	r.log().Info("the agent was revoked by the server; removing it", "name", obj.GetName(), "reason", reason)
	err := r.Dyn.Resource(GVR).Namespace(r.Namespace).Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

// revokedReason is whether the agent says the server revoked it, and why: status.revoked, which is where an agent now says it (it may write the
// status of its resource and nothing else of it), or the annotation that an agent from before that said it on the resource itself.
func revokedReason(obj *unstructured.Unstructured) (string, bool) {
	if m, found, _ := unstructured.NestedMap(obj.Object, "status", "revoked"); found {
		reason, _ := m["reason"].(string)
		return reason, true
	}
	reason, ok := obj.GetAnnotations()[install.RevokedAnnotation]
	return reason, ok
}

// ownerOf is the resource that owns the release when there are several: the oldest, then the first by name. A release is one thing, so
// the others wait; they must not fight over it, and deleting one of them must not uninstall what is not theirs.
func ownerOf(items []unstructured.Unstructured) string {
	if len(items) == 0 {
		return ""
	}
	sorted := append([]unstructured.Unstructured(nil), items...)
	sort.Slice(sorted, func(i, j int) bool {
		a, b := sorted[i].GetCreationTimestamp(), sorted[j].GetCreationTimestamp()
		if !a.Equal(&b) {
			return a.Before(&b)
		}
		return sorted[i].GetName() < sorted[j].GetName()
	})
	return sorted[0].GetName()
}

func (r *Reconciler) blocked(ctx context.Context, obj *unstructured.Unstructured, owner string) error {
	// It never held the finalizer, so deleting it is free; a finalizer left from an earlier ownership is let go.
	if hasFinalizer(obj) && obj.GetDeletionTimestamp() != nil {
		if err := r.setFinalizer(ctx, obj, false); err != nil {
			return err
		}
		return nil
	}
	return r.setStatus(ctx, obj, Status{Phase: PhaseBlocked, ObservedGeneration: obj.GetGeneration()},
		r.cond(obj, "False", "Blocked", fmt.Sprintf("%s owns the agent release in this namespace; there is one agent per namespace", owner)))
}

func (r *Reconciler) apply(ctx context.Context, obj *unstructured.Unstructured) (time.Duration, error) {
	if !hasFinalizer(obj) {
		if err := r.setFinalizer(ctx, obj, true); err != nil {
			return 0, err
		}
	}
	spec, err := parseSpec(obj)
	if err != nil {
		return 0, r.setStatus(ctx, obj, Status{Phase: PhaseInvalid, ObservedGeneration: obj.GetGeneration()},
			r.cond(obj, "False", "InvalidSpec", err.Error()))
	}
	token, wait, err := r.token(ctx, spec)
	if err != nil {
		return 0, err
	}
	cur, err := r.Installer.Current(ctx, r.Namespace, ReleaseName)
	if err != nil {
		return 0, err
	}
	if wait != "" && cur == nil {
		return WaitingForSecret, r.setStatus(ctx, obj, Status{Phase: PhasePending, ObservedGeneration: obj.GetGeneration()},
			r.cond(obj, "False", "WaitingForToken", wait))
	}
	plan := r.planRelay(obj.GetName(), obj, spec, token)
	values, err := r.values(obj.GetName(), plan.spec, token)
	if err != nil {
		st := Status{Phase: PhaseInvalid, ObservedGeneration: obj.GetGeneration()}
		if plan.fresh != nil { // a request cannot be applied to what cannot be installed; it is told, not left waiting
			st.Relay = &relay.Result{Revision: plan.fresh.Revision, State: relay.Refused, Reason: "the resource cannot be installed as it is, so nothing can be changed on it: " + reasonOf(err)}
		}
		return 0, r.setStatus(ctx, obj, st, r.cond(obj, "False", "InvalidSpec", err.Error()))
	}
	want := Hash(values)
	chartVersion := r.Installer.ChartVersion()
	if cur != nil && cur.Deployed && cur.ChartVersion == chartVersion && Hash(cur.Values) == want {
		st := r.ready(obj, cur, want)
		st.Relay = plan.answer() // a request that is already in force (the controller stopped before it said so) or that was refused
		again := r.watch(ctx, obj, &st)
		return again, r.setStatus(ctx, obj, st, r.cond(obj, "True", "Installed", "the release is installed as the spec asks"))
	}
	r.noteOutOfBand(obj, cur, values, want, chartVersion)
	// An attempt that has failed before, for this very resource and these very values, is tried again with its failure still on the resource:
	// writing "Installing" over it for the length of each attempt leaves the reason there for a moment and gone the rest of the time.
	if !failedBefore(obj, want) {
		if err := r.setStatus(ctx, obj, Status{Phase: PhaseInstalling, ObservedGeneration: obj.GetGeneration(), ValuesHash: want},
			r.cond(obj, "False", "Installing", "the release is being installed or changed")); err != nil {
			return 0, err
		}
	}
	rel, err := r.Installer.Apply(ctx, r.Namespace, ReleaseName, values)
	if err == nil && plan.overlay && plan.fresh != nil {
		// A change the server asked for has to leave the agent running; if it does not, the cluster is put back as its owner has it.
		if err = r.settle(ctx); err != nil {
			return r.putBack(ctx, obj, spec, token, plan, err)
		}
	}
	if err != nil {
		if plan.overlay && plan.fresh != nil {
			return r.putBack(ctx, obj, spec, token, plan, err)
		}
		_ = r.setStatus(ctx, obj, Status{Phase: PhaseFailed, ObservedGeneration: obj.GetGeneration(), ValuesHash: want},
			r.cond(obj, "False", "InstallFailed", err.Error()))
		return 0, fmt.Errorf("installing the agent release: %w", err)
	}
	r.log().Info("agent release applied", "release", ReleaseName, "revision", rel.Revision, "chart", rel.ChartVersion)
	st := r.ready(obj, rel, want)
	st.Relay = plan.answer()
	again := r.watch(ctx, obj, &st)
	return again, r.setStatus(ctx, obj, st, r.cond(obj, "True", "Installed", "the release is installed as the spec asks"))
}

// noteOutOfBand says in the log when the release is not what this controller last installed for this very resource: someone changed it
// from outside (helm upgrade or rollback by hand, a failed manual upgrade), and the controller is about to put it back. Nothing here changes
// what is done; it only makes the repair readable afterwards. A release that is simply behind (the spec was edited, the chart or this build's
// defaults moved on) is the controller's own work and is not said. Only the names of the values that differ are logged, never the values:
// they hold the enrolment token.
func (r *Reconciler) noteOutOfBand(obj *unstructured.Unstructured, cur *Release, want map[string]any, wantHash, chartVersion string) {
	if cur == nil {
		return
	}
	st := statusOf(obj)
	if st.Phase != PhaseReady || st.Release == nil || st.ValuesHash != wantHash || st.ObservedGeneration != obj.GetGeneration() {
		return // not installed yet, failed before (said then), or the resource or the defaults changed since: the controller's own change
	}
	if Hash(cur.Values) == wantHash && cur.Deployed {
		return // the same values, completed: at most a newer chart, which is the controller's to roll out
	}
	changed, more := differingValues(cur.Values, want, 12)
	r.log().Warn("the agent release was changed outside the controller; putting it back to what the resource asks",
		"name", obj.GetName(), "release", ReleaseName,
		"revision", cur.Revision, "installed_revision", st.Release.Revision,
		"deployed", cur.Deployed, "chart", cur.ChartVersion, "controller_chart", chartVersion,
		"differing_values", changed, "more_differing_values", more)
}

// differingValues are the paths (a.b.c) at which two sets of values differ, sorted, at most max of them, and how many more there were. Maps
// are compared key by key; a list or a scalar as one value. Numbers are compared as JSON gives them, so an int and the float a stored
// release returns are the same.
func differingValues(have, want map[string]any, max int) ([]string, int) {
	var paths []string
	var walk func(prefix string, a, b any)
	walk = func(prefix string, a, b any) {
		am, aok := a.(map[string]any)
		bm, bok := b.(map[string]any)
		if aok && bok {
			keys := map[string]bool{}
			for k := range am {
				keys[k] = true
			}
			for k := range bm {
				keys[k] = true
			}
			for k := range keys {
				p := k
				if prefix != "" {
					p = prefix + "." + k
				}
				av, ain := am[k]
				bv, bin := bm[k]
				switch {
				case ain && bin:
					walk(p, av, bv)
				default:
					paths = append(paths, p)
				}
			}
			return
		}
		if !sameJSON(a, b) {
			paths = append(paths, prefix)
		}
	}
	walk("", have, want)
	sort.Strings(paths)
	if len(paths) > max {
		return paths[:max], len(paths) - max
	}
	return paths, 0
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// watch adds to the status of an installed release what it runs and whether every pod is there, and says when to look again.
func (r *Reconciler) watch(ctx context.Context, obj *unstructured.Unstructured, st *Status) time.Duration {
	every := r.observeEvery()
	if every == 0 {
		return 0
	}
	ws, ok := r.observe(ctx)
	if !ok {
		// The API did not answer: what the status said last stands, instead of an account that is gone until the next look.
		old := statusOf(obj)
		st.Workloads = old.Workloads
		for _, c := range old.Conditions {
			if c.Type == ConditionWorkloadsReady {
				st.Conditions = append(st.Conditions, c)
			}
		}
		return min(every, observeWhileWaiting)
	}
	st.Workloads = ws
	if c := workloadsCondition(obj.GetGeneration(), st.Workloads); c != nil {
		st.Conditions = append(st.Conditions, *c)
	}
	if !allDone(st.Workloads) {
		return min(every, observeWhileWaiting)
	}
	return every
}

// putBack installs the spec alone again after a request that was applied did not leave the agent running, and says so under status.relay.
// The answer is written before anything else can go wrong, so that a controller that cannot put the cluster back does not try the request
// again and again: it goes on trying the spec, as it does with any release that fails.
func (r *Reconciler) putBack(ctx context.Context, obj *unstructured.Unstructured, spec Spec, token string, plan relayPlan, cause error) (time.Duration, error) {
	why := "putting it back"
	res := &relay.Result{Revision: plan.fresh.Revision, State: relay.Reverted, Reason: reasonOf(fmt.Errorf("%v; the cluster was put back to its own settings", cause))}
	r.log().Warn("a telemetry request did not leave the agent running; putting the cluster back", "revision", plan.fresh.Revision, "err", cause)
	values, err := r.values(obj.GetName(), spec, token)
	if err == nil {
		var rel *Release
		if rel, err = r.Installer.Apply(ctx, r.Namespace, ReleaseName, values); err == nil {
			st := r.ready(obj, rel, Hash(values))
			st.Relay = res
			return 0, r.setStatus(ctx, obj, st, r.cond(obj, "True", "Installed", "the release is installed as the spec asks"))
		}
	}
	res.Reason = reasonOf(fmt.Errorf("%v; and %s failed: %v", cause, why, err))
	_ = r.setStatus(ctx, obj, Status{Phase: PhaseFailed, ObservedGeneration: obj.GetGeneration(), Relay: res},
		r.cond(obj, "False", "InstallFailed", err.Error()))
	return 0, fmt.Errorf("putting the agent release back after a telemetry request: %w", err)
}

// failedBefore is whether the resource, as it is now, already says that installing these values failed (the values are what makes it the
// same attempt: an edit of the resource that comes to the same values is the same attempt).
func failedBefore(obj *unstructured.Unstructured, hash string) bool {
	raw, ok := obj.Object["status"].(map[string]any)
	if !ok {
		return false
	}
	var st Status
	if runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &st) != nil {
		return false
	}
	return st.Phase == PhaseFailed && st.ValuesHash == hash
}

// ceiling is the highest tier this controller may install.
func (r *Reconciler) ceiling() int {
	if r.MaxTier != nil {
		return *r.MaxTier
	}
	return DefaultTier
}

// values are the spec's Helm values over the defaults. A spec that does not name a tier gets the highest this controller may give
// (the chart's own default, 2, when it has no lower ceiling); one that names a tier above it is refused, not trimmed.
func (r *Reconciler) values(name string, spec Spec, token string) (map[string]any, error) {
	if spec.AccessTier == nil && r.ceiling() < DefaultTier {
		t := r.ceiling()
		spec.AccessTier = &t
	}
	if spec.AccessTier != nil && *spec.AccessTier > r.ceiling() {
		return nil, &install.ProblemsError{Problems: []string{fmt.Sprintf("spec.accessTier %d is above the %d this controller was given the permissions for (maxAccessTier in its chart)", *spec.AccessTier, r.ceiling())}}
	}
	if spec.AsksForTelemetry() && !r.AllowTelemetry {
		return nil, &install.ProblemsError{Problems: []string{"spec.telemetry turns telemetry on, and this controller was not given the permissions for it (allowTelemetry in its chart)"}}
	}
	v, err := spec.Values(token)
	if err != nil {
		return nil, err
	}
	// The agent is told which resource keeps it, so that a revoked agent can say so on it (see install.RevokedAnnotation).
	ctl := map[string]any{"resource": name}
	if r.RelayTelemetry {
		ctl["relayTelemetry"] = true // only when on: a release of a controller that never heard of it is not changed by it
	}
	return Merge(Merge(r.Defaults, v), map[string]any{"controller": ctl}), nil
}

func (r *Reconciler) ready(obj *unstructured.Unstructured, rel *Release, hash string) Status {
	return Status{Phase: PhaseReady, ObservedGeneration: obj.GetGeneration(), ValuesHash: hash,
		Release: &ReleaseRef{Name: ReleaseName, Revision: rel.Revision, ChartVersion: rel.ChartVersion}}
}

// token reads the enrolment token from its Secret. The second result says what is missing when there is no token to give yet.
func (r *Reconciler) token(ctx context.Context, spec Spec) (token, missing string, err error) {
	ref := spec.Enrollment.TokenSecretRef
	if ref == nil {
		return "", "spec.enrollment.tokenSecretRef is not set", nil
	}
	sec, err := r.Kube.CoreV1().Secrets(r.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", fmt.Sprintf("the Secret %q does not exist in this namespace", ref.Name), nil
	}
	if err != nil {
		return "", "", err
	}
	return tokenOf(sec, ref)
}

func tokenOf(sec *corev1.Secret, ref *SecretKeyRef) (string, string, error) {
	b, ok := sec.Data[ref.TokenKey()]
	if !ok {
		return "", fmt.Sprintf("the Secret %q has no key %q", ref.Name, ref.TokenKey()), nil
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Sprintf("the key %q of the Secret %q is empty", ref.TokenKey(), ref.Name), nil
	}
	return tok, "", nil
}

// finish uninstalls the release of a resource that is being deleted, and then lets it go.
func (r *Reconciler) finish(ctx context.Context, obj *unstructured.Unstructured) error {
	if !hasFinalizer(obj) {
		return nil
	}
	if obj.GetAnnotations()[SkipUninstall] == "true" {
		r.log().Warn("letting an IkhnosAgent go without uninstalling its release", "name", obj.GetName())
		return r.setFinalizer(ctx, obj, false)
	}
	_ = r.setStatus(ctx, obj, Status{Phase: PhaseDeleting, ObservedGeneration: obj.GetGeneration()},
		r.cond(obj, "False", "Deleting", "the release is being uninstalled"))
	cur, err := r.Installer.Current(ctx, r.Namespace, ReleaseName)
	if err != nil {
		return err
	}
	if cur != nil {
		if err := r.matchPurge(ctx, obj, cur); err != nil {
			return err
		}
		if err := r.Installer.Uninstall(ctx, r.Namespace, ReleaseName); err != nil {
			if namespaceIsGoing(err) {
				// The whole namespace is being deleted, and with it the release and everything it made: there is nothing left to uninstall,
				// and trying again would fail the same way for ever (the identity-purge Job cannot be created in a terminating
				// namespace) and hold the namespace in Terminating. The resource is let go.
				r.log().Warn("letting an IkhnosAgent go: its namespace is being deleted", "name", obj.GetName(), "err", err)
				return r.setFinalizer(ctx, obj, false)
			}
			return fmt.Errorf("uninstalling the agent release: %w", err)
		}
	}
	return r.setFinalizer(ctx, obj, false)
}

// namespaceIsGoing says whether an error is the cluster refusing to create something because the namespace is being deleted.
func namespaceIsGoing(err error) bool {
	if apierrors.HasStatusCause(err, corev1.NamespaceTerminatingCause) {
		return true
	}
	return strings.Contains(err.Error(), "because it is being terminated")
}

// matchPurge makes the installed release agree with the spec on deleting the identity before it is uninstalled: the hook that does it
// is part of the release as installed, so a spec that asks for it after the install has to be applied first.
func (r *Reconciler) matchPurge(ctx context.Context, obj *unstructured.Unstructured, cur *Release) error {
	spec, err := parseSpec(obj)
	if err != nil {
		return nil // a spec that cannot be read says nothing about it: the release goes as it is
	}
	// A revoked agent's identity is of no use to anyone, so it goes with the agent whatever the spec says about keeping it.
	if _, revoked := revokedReason(obj); revoked {
		spec.PurgeIdentityOnDelete = true
	}
	if purgeOf(cur.Values) == spec.PurgeIdentityOnDelete {
		return nil
	}
	token, _, err := r.token(ctx, spec)
	if err != nil {
		return err
	}
	values, err := r.values(obj.GetName(), spec, token)
	if err != nil {
		return nil
	}
	if _, err := r.Installer.Apply(ctx, r.Namespace, ReleaseName, values); err != nil {
		return fmt.Errorf("applying the identity setting before uninstalling: %w", err)
	}
	return nil
}

func purgeOf(values map[string]any) bool {
	id, _ := values["identity"].(map[string]any)
	b, _ := id["purgeOnUninstall"].(bool)
	return b
}

func hasFinalizer(obj *unstructured.Unstructured) bool {
	for _, f := range obj.GetFinalizers() {
		if f == Finalizer {
			return true
		}
	}
	return false
}

func (r *Reconciler) setFinalizer(ctx context.Context, obj *unstructured.Unstructured, on bool) error {
	res := r.Dyn.Resource(GVR).Namespace(r.Namespace)
	fresh, err := res.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var fs []string
	for _, f := range fresh.GetFinalizers() {
		if f != Finalizer {
			fs = append(fs, f)
		}
	}
	if on {
		fs = append(fs, Finalizer)
	}
	fresh.SetFinalizers(fs)
	_, err = res.Update(ctx, fresh, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (r *Reconciler) cond(obj *unstructured.Unstructured, status, reason, message string) Condition {
	return Condition{Type: "Ready", Status: status, Reason: reason, Message: message, ObservedGeneration: obj.GetGeneration()}
}

// setStatus writes the status when it differs from what is there. A condition keeps its time of transition while its status does not
// change, so an unchanged resource is not written to again and again.
func (r *Reconciler) setStatus(ctx context.Context, obj *unstructured.Unstructured, st Status, c Condition) error {
	res := r.Dyn.Resource(GVR).Namespace(r.Namespace)
	fresh, err := res.Get(ctx, obj.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var old Status
	if raw, ok := fresh.Object["status"].(map[string]any); ok {
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &old)
	}
	if dc := r.definitionCondition(ctx, obj.GetGeneration()); dc != nil {
		st.Conditions = append(st.Conditions, *dc)
	}
	conds := append([]Condition{c}, st.Conditions...)
	for i := range conds {
		conds[i].LastTransitionTime = r.now().UTC().Format(time.RFC3339)
		for _, o := range old.Conditions {
			if o.Type == conds[i].Type && o.Status == conds[i].Status {
				conds[i].LastTransitionTime = o.LastTransitionTime
			}
		}
	}
	st.Conditions = conds
	st.Revoked = old.Revoked // the agent's word, not the controller's: writing the status must not take it away
	st.Requested = old.Requested
	if st.Relay == nil {
		st.Relay = old.Relay // the answer stands until the controller gives another
	}
	if st.Relay != nil && utf8.RuneCountInString(st.Relay.Reason) > maxRelayReason {
		rel := *st.Relay // whatever worded it, the definition would refuse the whole status for a reason longer than it allows
		rel.Reason = cutRunes(rel.Reason, maxRelayReason-1)
		st.Relay = &rel
	}
	if equalStatus(old, st) {
		return nil
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&st)
	if err != nil {
		return err
	}
	fresh.Object["status"] = m
	_, err = res.UpdateStatus(ctx, fresh, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func equalStatus(a, b Status) bool {
	ma, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&a)
	mb, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(&b)
	return fmt.Sprint(ma) == fmt.Sprint(mb)
}

// parseSpec reads the spec of a resource. A spec that does not parse, or that has problems, is told in full.
func parseSpec(obj *unstructured.Unstructured) (Spec, error) {
	var s Spec
	raw, _ := obj.Object["spec"].(map[string]any)
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &s); err != nil {
		return Spec{}, fmt.Errorf("the spec cannot be read: %w", err)
	}
	if p := s.Problems(); len(p) > 0 {
		return Spec{}, &install.ProblemsError{Problems: p}
	}
	return s, nil
}
