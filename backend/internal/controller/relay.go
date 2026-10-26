package controller

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"ikhnos/internal/install"
	"ikhnos/internal/relay"
)

// The telemetry relay, the controller's part. The agent writes what the Ikhnos server asked of the cluster under status.requested (package
// relay); the controller reads it, and either applies it or says why not under status.relay.
//
// A request is an overlay on the owner's spec.telemetry: the set of signals and the share of the traces, and nothing else. Where the data
// goes, and with what credential, is the owner's and stays in the resource; a request has no word for it.
//
// What is installed is a pure function of the spec and the status: the spec with the request over it when the request was applied (its
// answer is "applied"), and the spec alone otherwise. So there is nothing to retry: a request that was refused, or put back, stays so until
// the server asks again with a higher revision, and a controller that is stopped in the middle of one finds the same answer when it starts.

// AgentDeployment is the agent's Deployment in the release, the one that has to be ready for a change to count as made.
const AgentDeployment = "ikhnos-agent"

// DefaultRelayDeadline is how long the agent has to be ready again after a request was applied, before the cluster is put back.
const DefaultRelayDeadline = 3 * time.Minute

// relayPlan is what one pass of apply installs, and what it says about the request.
type relayPlan struct {
	spec    Spec           // what to install from
	overlay bool           // the request is part of spec
	fresh   *relay.Request // a request that has not been answered yet, nil when there is none
	refused string         // why a fresh request is not applied
}

// answer is what status.relay says of a fresh request when it is installed as it should be.
func (p relayPlan) answer() *relay.Result {
	switch {
	case p.fresh == nil:
		return nil
	case p.refused != "":
		return &relay.Result{Revision: p.fresh.Revision, State: relay.Refused, Reason: p.refused}
	case !p.fresh.Asks():
		return &relay.Result{Revision: p.fresh.Revision, State: relay.Applied, Reason: "the request was taken back: the cluster's own settings are in force"}
	}
	return &relay.Result{Revision: p.fresh.Revision, State: relay.Applied}
}

// statusOf reads the status of a resource as far as the controller writes and keeps it.
func statusOf(obj *unstructured.Unstructured) Status {
	var st Status
	if raw, ok := obj.Object["status"].(map[string]any); ok {
		_ = runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &st)
	}
	return st
}

// planRelay decides what to install from. A controller that was not asked to relay (relay.telemetry in its chart) reads no request.
func (r *Reconciler) planRelay(name string, obj *unstructured.Unstructured, spec Spec, token string) relayPlan {
	p := relayPlan{spec: spec}
	st := statusOf(obj)
	req := st.Requested
	if req == nil || !r.RelayTelemetry {
		return p
	}
	if st.Relay != nil && st.Relay.Revision == req.Revision {
		// Answered. An applied request that asks for something stays in force; anything else (refused, put back, or taken back) is not.
		// It is held to what a fresh one is: if the spec has changed since, or the controller no longer has the permissions, so that the
		// request can no longer be installed, the spec alone is in force; a stale request must not keep an edit of the owner's from taking effect.
		if st.Relay.State == relay.Applied && req.Asks() {
			if merged := withRequest(spec, *req); r.admits(name, merged, token) == "" {
				p.spec, p.overlay = merged, true
			}
		}
		return p
	}
	p.fresh = req
	if !req.Asks() {
		return p
	}
	merged := withRequest(spec, *req)
	if why := r.admits(name, merged, token); why != "" {
		p.refused = why
		return p
	}
	p.spec, p.overlay = merged, true
	return p
}

// admits is why the spec with a request over it cannot be installed by this controller, or "" when it can.
func (r *Reconciler) admits(name string, merged Spec, token string) string {
	if merged.AsksForTelemetry() && !r.AllowTelemetry {
		return "this cluster's controller was not given the permissions telemetry needs (allowTelemetry in its chart), so it cannot turn a signal on"
	}
	if _, err := r.values(name, merged, token); err != nil {
		return "this cluster cannot take it: " + reasonOf(err)
	}
	return ""
}

// withRequest is the spec as a request would have it: the signals and the share of the traces replaced, the rest the owner's. A request
// that turns nothing on in a spec with no telemetry leaves the spec as it is.
func withRequest(spec Spec, req relay.Request) Spec {
	var t install.ResourceTelemetry
	if spec.Telemetry != nil {
		t = *spec.Telemetry
	}
	if req.Signals != nil {
		t.Signals = install.ResourceSignals(*req.Signals)
	}
	if req.TracesSamplingPercentage != nil {
		var p install.ResourceProcessing
		if t.Processing != nil {
			p = *t.Processing
		}
		pct := float64(*req.TracesSamplingPercentage)
		p.TracesSamplingPercent = &pct
		t.Processing = &p
	}
	if spec.Telemetry == nil && t.Signals == (install.ResourceSignals{}) {
		return spec
	}
	spec.Telemetry = &t
	return spec
}

// reasonOf words an error for status.relay.
func reasonOf(err error) string {
	msg := err.Error()
	if pe, ok := err.(*install.ProblemsError); ok {
		msg = strings.Join(pe.Problems, "; ")
	}
	return cutRunes(msg, 450)
}

// maxRelayReason is the longest status.relay.reason the definition accepts (characters, not bytes).
const maxRelayReason = 500

// cutRunes cuts s to at most n characters, with an ellipsis when it was cut. It never splits a character, so what it returns is valid text
// the API server accepts, and it counts characters, which is how the definition's maxLength counts.
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// settle waits for what the release runs to be ready after a change: the agent and the telemetry collectors, each rolled out with no old
// pod left. The collectors matter most, since a telemetry change is a change of their configuration. An error says what was seen last.
func (r *Reconciler) settle(ctx context.Context) error {
	deadline := r.RelayDeadline
	if deadline <= 0 {
		deadline = DefaultRelayDeadline
	}
	poll := r.RelayPoll
	if poll <= 0 {
		poll = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	opts := metav1.ListOptions{LabelSelector: "app.kubernetes.io/instance=" + ReleaseName + ",app.kubernetes.io/part-of=ikhnos"}
	last := "the agent's Deployment was not found"
	for {
		if waiting, err := r.notReady(ctx, opts); err != nil {
			if ctx.Err() == nil {
				last = err.Error()
			}
		} else if waiting == "" {
			return nil
		} else {
			last = waiting
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("the agent was not ready %s after the change (%s)", deadline.Round(time.Second), last)
		case <-time.After(poll):
		}
	}
}

// notReady is "" when everything the release runs is rolled out, else what is not. The agent's own Deployment has to be there.
func (r *Reconciler) notReady(ctx context.Context, opts metav1.ListOptions) (string, error) {
	ds, err := r.Kube.AppsV1().Deployments(r.Namespace).List(ctx, opts)
	if err != nil {
		return "", err
	}
	sets, err := r.Kube.AppsV1().DaemonSets(r.Namespace).List(ctx, opts)
	if err != nil {
		return "", err
	}
	found := false
	for _, d := range ds.Items {
		found = found || d.Name == AgentDeployment
		want := int32(1)
		if d.Spec.Replicas != nil {
			want = *d.Spec.Replicas
		}
		s := d.Status
		switch {
		case d.Generation > s.ObservedGeneration:
			return fmt.Sprintf("%s: the change has not been rolled out", d.Name), nil
		case s.UpdatedReplicas != want || s.AvailableReplicas != want || s.Replicas != want:
			return fmt.Sprintf("%s: %d of %d pods updated and ready", d.Name, min(s.UpdatedReplicas, s.AvailableReplicas), want), nil
		}
	}
	if !found {
		return "the agent's Deployment was not found", nil
	}
	for _, d := range sets.Items {
		s := d.Status
		switch {
		case d.Generation > s.ObservedGeneration:
			return fmt.Sprintf("%s: the change has not been rolled out", d.Name), nil
		case s.UpdatedNumberScheduled != s.DesiredNumberScheduled || s.NumberReady != s.DesiredNumberScheduled:
			return fmt.Sprintf("%s: %d of %d pods updated and ready", d.Name, min(s.UpdatedNumberScheduled, s.NumberReady), s.DesiredNumberScheduled), nil
		}
	}
	return "", nil
}
