package controller

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// Workload is one thing the release runs (the agent, a collector, Kepler, the node probe) and how many of its pods are ready.
type Workload struct {
	Name string `json:"name"`
	// Kind is Deployment or DaemonSet.
	Kind string `json:"kind"`
	// Component is the part of the release it is, from its app.kubernetes.io/component label (the agent's own Deployment has none).
	Component string `json:"component,omitempty"`
	// Wanted is how many pods it asks for: its replicas, or the nodes a DaemonSet is to run on.
	Wanted int32 `json:"wanted"`
	// Ready is how many of them are running, updated and available.
	Ready int32 `json:"ready"`
	// RollingOut says the last change to it has not been carried out yet.
	RollingOut bool `json:"rollingOut,omitempty"`
}

func (w Workload) done() bool { return !w.RollingOut && w.Ready >= w.Wanted }

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
// reconcile: what cannot be read is left out, and the status says nothing of it rather than something untrue.
func (r *Reconciler) observe(ctx context.Context) []Workload {
	opts := metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + ReleaseName + ",app.kubernetes.io/part-of=ikhnos"}
	ds, err := r.Kube.AppsV1().Deployments(r.Namespace).List(ctx, opts)
	if err != nil {
		r.log().Debug("cannot list the release's Deployments", "err", err)
		return nil
	}
	sets, err := r.Kube.AppsV1().DaemonSets(r.Namespace).List(ctx, opts)
	if err != nil {
		r.log().Debug("cannot list the release's DaemonSets", "err", err)
		return nil
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
	return out
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
	if len(msg) > maxWorkloadMessage {
		msg = msg[:maxWorkloadMessage-1] + "…"
	}
	return &Condition{Type: ConditionWorkloadsReady, Status: "False", Reason: "PodsNotReady", Message: msg, ObservedGeneration: obj}
}

// allDone is whether every workload has its pods.
func allDone(ws []Workload) bool {
	for _, w := range ws {
		if !w.done() {
			return false
		}
	}
	return true
}
