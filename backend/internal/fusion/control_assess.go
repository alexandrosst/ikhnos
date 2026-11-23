package fusion

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"ikhnos/internal/kernel"
)

// storeRead is what one workload's reads found.
type storeRead struct {
	w      kernel.KubeWorkload
	err    error
	reason string
	hard   bool
}

// readStatus reads the workloads, all at once, and works out the state. Anything it cannot read - no permission, the
// workloads are not there (an older server chart) - is reported as unavailable with the reason, never as an error: the
// screen shows it as a fact.
func (f *FusionControl) readStatus(ctx context.Context) FusionStatus {
	stores := f.stores()
	reads := make([]storeRead, len(stores))
	var wg sync.WaitGroup
	for i, s := range stores {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reads[i] = f.readStore(ctx, s)
		}()
	}
	wg.Wait()

	st := FusionStatus{Available: true}
	var comps []FusionComponent
	for i, s := range stores {
		r := reads[i]
		switch {
		case s.Optional && (errors.Is(r.err, kernel.ErrKubeNotFound) || errors.Is(r.err, kernel.ErrKubeForbidden)):
			continue // Grafana is off in this install (or this Role predates it): the rest of FUSION is unaffected
		case errors.Is(r.err, kernel.ErrKubeNotFound):
			return FusionStatus{State: StateOff, Reason: ReasonNotInstalled, Message: "FUSION's workloads are not in this release. Upgrade the server chart (fusion.enabled=true) to get them."}
		case errors.Is(r.err, kernel.ErrKubeForbidden):
			return FusionStatus{State: StateOff, Reason: ReasonNoAccess, Message: "This server is not allowed to read or scale FUSION's workloads. Check fusionControl.enabled in the server chart."}
		case r.err != nil:
			st.State, st.Message = StateAttention, "The Kubernetes API did not answer: "+r.err.Error()
			return st
		}
		comps = append(comps, FusionComponent{Component: s.Component, Label: s.Label, Desired: r.w.Desired, Ready: r.w.Ready,
			Reason: r.reason, hard: r.hard, optional: s.Optional})
	}
	st = f.assess(comps)
	if st.ComponentUp(ComponentMetrics) || st.ComponentUp(ComponentLogs) || st.ComponentUp(ComponentTraces) {
		st.LastDataAt, st.LastData, st.Traffic = f.lastData(ctx, st)
	}
	return st
}

// readStore reads one workload and, if it has not come up, the reason why.
func (f *FusionControl) readStore(ctx context.Context, s fusionStore) storeRead {
	cctx, cancel := context.WithTimeout(ctx, fusionKubeTimeout)
	defer cancel()
	w, err := f.Kube.Workload(cctx, s.Kind, s.Name)
	r := storeRead{w: w, err: err}
	if err == nil && w.Desired > w.Ready && s.Kind == "statefulsets" {
		dctx, dcancel := context.WithTimeout(ctx, fusionKubeTimeout)
		defer dcancel()
		r.reason, r.hard = f.whyNotReady(dctx, s)
	}
	return r
}

// A store's one pod, and its volume, are named by the chart's own rule (a StatefulSet's pod is <name>-0, its claim
// <template>-<pod>, and the chart's template is "data"): the server's Role grants exactly those names, and the chart
// test in internal/chart pins them to what the chart renders.
func storePod(s fusionStore) string { return s.Name + "-0" }

func storeClaim(s fusionStore) string { return "data-" + storePod(s) }

// whyNotReady says in plain words why a store's pod is not ready: what Kubernetes is waiting on, from the pod and its
// volume. Every failure to find out - no such pod yet, a Role from before these reads existed, the API slow - is just
// "no reason": the count of parts up is still right.
func (f *FusionControl) whyNotReady(ctx context.Context, s fusionStore) (reason string, hard bool) {
	pod, err := f.Kube.Pod(ctx, storePod(s))
	if err != nil {
		return "", false
	}
	// Only a pod that has not started, or is being created, can be held up by its volume.
	var claim string
	if pod.Unschedulable || pod.WaitingReason == "ContainerCreating" || (pod.Phase == "Pending" && pod.WaitingReason == "") {
		claim, _ = f.Kube.ClaimPhase(ctx, storeClaim(s))
	}
	return ExplainPod(pod, claim)
}

// explainPod turns Kubernetes' reason codes into a short sentence for a person. hard is a fault that will not clear on
// its own. A code it does not know says nothing rather than show a raw identifier.
func ExplainPod(p kernel.KubePod, claimPhase string) (reason string, hard bool) {
	switch p.WaitingReason {
	case "ErrImagePull", "ImagePullBackOff":
		return "Cannot download the image", true
	case "InvalidImageName":
		return "The image name is not valid", true
	case "CrashLoopBackOff":
		if p.LastTerminatedReason == "OOMKilled" {
			return "Ran out of memory and keeps restarting", true
		}
		return "Keeps stopping and restarting", true
	case "CreateContainerConfigError":
		return "Its configuration is incomplete", true
	case "CreateContainerError", "RunContainerError":
		return "The container could not be started", true
	case "ContainerCreating":
		if claimPhase == "Pending" {
			return "Waiting for a volume", true
		}
		return "Starting the container", false
	case "PodInitializing":
		return "Starting up", false
	case "":
		switch {
		case claimPhase == "Pending" || (p.Unschedulable && strings.Contains(p.ScheduleMessage, "PersistentVolumeClaim")):
			return "Waiting for a volume", true
		case p.Unschedulable && strings.Contains(p.ScheduleMessage, "Insufficient"):
			return "No node has room", true
		case p.Unschedulable:
			return "No suitable node", true
		}
	}
	return "", false
}

// assess works out the state from the workloads' counts, and keeps the clocks that say how long they have not been up.
func (f *FusionControl) assess(comps []FusionComponent) FusionStatus {
	st := FusionStatus{Available: true, Components: comps}
	var desired, ready int
	hard := false
	var reasons []string
	for _, c := range comps {
		if c.Reason != "" && c.Desired > c.Ready {
			reasons = append(reasons, c.Label+": "+strings.ToLower(c.Reason[:1])+c.Reason[1:])
			hard = hard || c.hard
		}
		if !c.optional { // Grafana starting late does not make FUSION "starting": what is sent is already being kept
			desired += c.Desired
			ready += c.Ready
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case desired == 0:
		st.State = StateOff
		f.since, f.notReady = time.Time{}, time.Time{}
	case ready >= desired:
		st.State = StateRunning
		f.notReady = time.Time{}
	default:
		st.State = StateStarting
		if f.since.IsZero() { // the server restarted while FUSION was starting: count from now
			f.since = f.Clock()
		}
		if f.notReady.IsZero() { // first seen not ready (a fresh start, a restart of the server, or a pod that fell over)
			f.notReady = f.Clock()
		}
		grace := fusionStartGrace
		if hard {
			grace = fusionHardGrace
		}
		if waited := f.Clock().Sub(f.notReady); waited > grace {
			if len(reasons) > 0 {
				st.State, st.Message = StateAttention, fmt.Sprintf("%d of %d parts are up after %s. %s. Details: kubectl -n %s get pods", ready, desired, waitedFor(waited), strings.Join(reasons, ". "), f.Namespace)
			} else {
				st.State, st.Message = StateAttention, fmt.Sprintf("%d of %d parts are up after %s. Check the pods (kubectl -n %s get pods) - most often an image that cannot be pulled or a volume that cannot be bound.", ready, desired, waitedFor(waited), f.Namespace)
			}
		}
	}
	if !f.since.IsZero() {
		t := f.since
		st.Since = &t
	}
	return st
}

func waitedFor(d time.Duration) string {
	if d < 2*time.Minute {
		return strconv.Itoa(int(d.Seconds())) + " seconds"
	}
	return strconv.Itoa(int(d.Minutes())) + " minutes"
}
