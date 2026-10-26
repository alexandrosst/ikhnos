package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ikhnos/internal/relay"
)

// ConditionWorkloadsReady is true when every pod the release asks for is running and rolled out. It is not part of Ready: Ready says the
// release is installed as the spec asks, and a pod held back on one node does not make that untrue; it makes this false.
const ConditionWorkloadsReady = "WorkloadsReady"

// Default intervals at which the controller looks at what the release runs: slowly while all is well, soon while something is not ready.
const (
	DefaultObserveEvery = time.Minute
	observeWhileWaiting = 15 * time.Second
	// maxWorkloads bounds what is written to the resource.
	maxWorkloads = 16
	// maxWorkloadMessage bounds the condition's message.
	maxWorkloadMessage = 400
)

// Workload is what the status says of one thing the release runs (see package relay).
type Workload = relay.Workload

func done(w Workload) bool { return !w.RollingOut && w.Ready >= w.Wanted }

func (r *Reconciler) observeEvery() time.Duration {
	if r.ObserveEvery < 0 {
		return 0
	}
	if r.ObserveEvery == 0 {
		return DefaultObserveEvery
	}
	return r.ObserveEvery
}

// observe reads what the release runs: the Deployments and DaemonSets it made, by the labels the chart puts on them. It never fails the
// reconcile: what cannot be read is not guessed at (the second result is false), and the caller keeps what the status said before rather than
// writing something untrue or taking the account away for a moment.
func (r *Reconciler) observe(ctx context.Context) ([]Workload, bool) {
	opts := metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + ReleaseName + ",app.kubernetes.io/part-of=ikhnos"}
	ds, err := r.Kube.AppsV1().Deployments(r.Namespace).List(ctx, opts)
	if err != nil {
		r.log().Debug("cannot list the release's Deployments", "err", err)
		return nil, false
	}
	sets, err := r.Kube.AppsV1().DaemonSets(r.Namespace).List(ctx, opts)
	if err != nil {
		r.log().Debug("cannot list the release's DaemonSets", "err", err)
		return nil, false
	}
	var out []Workload
	for _, d := range ds.Items {
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		s := d.Status
		out = append(out, Workload{Name: d.Name, Kind: "Deployment", Component: d.Labels["app.kubernetes.io/component"], Wanted: want,
			Ready:      min(s.UpdatedReplicas, s.AvailableReplicas),
			RollingOut: d.Generation > s.ObservedGeneration || s.Replicas != want})
	}
	for _, d := range sets.Items {
		s := d.Status
		out = append(out, Workload{Name: d.Name, Kind: "DaemonSet", Component: d.Labels["app.kubernetes.io/component"], Wanted: s.DesiredNumberScheduled,
			Ready:      min(s.UpdatedNumberScheduled, s.NumberReady),
			RollingOut: d.Generation > s.ObservedGeneration})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind == "Deployment"
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > maxWorkloads {
		out = out[:maxWorkloads]
	}
	return out, true
}

// workloadsCondition says in one sentence whether every workload has its pods; nil when there is nothing to say.
func workloadsCondition(obj int64, ws []Workload) *Condition {
	if len(ws) == 0 {
		return nil
	}
	var not []string
	for _, w := range ws {
		switch {
		case w.RollingOut:
			not = append(not, fmt.Sprintf("%s: a change is being rolled out (%d of %d pods ready)", w.Name, w.Ready, w.Wanted))
		case w.Ready < w.Wanted:
			not = append(not, fmt.Sprintf("%s: %d of %d pods ready", w.Name, w.Ready, w.Wanted))
		}
	}
	if len(not) == 0 {
		return &Condition{Type: ConditionWorkloadsReady, Status: "True", Reason: "AllPodsReady", Message: "every pod the release asks for is ready", ObservedGeneration: obj}
	}
	msg := strings.Join(not, "; ")
	msg = cutRunes(msg, maxWorkloadMessage-1)
	return &Condition{Type: ConditionWorkloadsReady, Status: "False", Reason: "PodsNotReady", Message: msg, ObservedGeneration: obj}
}

// allDone is whether every workload has its pods.
func allDone(ws []Workload) bool {
	for _, w := range ws {
		if !done(w) {
			return false
		}
	}
	return true
}
