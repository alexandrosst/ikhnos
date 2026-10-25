package agents

import (
	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/kernel"
)

// What an administrator may ask a cluster to collect. It is the "what" of the telemetry: which signals, and how much of the traces. It is
// never the "where": the destination and its credentials stay in the cluster, so nothing here can send data anywhere new. The cluster's
// owner has the last word in two places, not this server: the agent hands the request to the controller that keeps it installed, and
// the controller applies it only inside what it was installed to allow (allowTelemetry and the access tier), or says why not.

// TelemetrySignals are the signals of the telemetry, named as the IkhnosAgent resource names them (spec.telemetry.signals).
type TelemetrySignals struct {
	ResourceUsage      bool `json:"resourceUsage"`
	Energy             bool `json:"energy"`
	KubernetesState    bool `json:"kubernetesState"`
	NodeRuntime        bool `json:"nodeRuntime"`
	NetworkLatency     bool `json:"networkLatency"`
	ClusterLinks       bool `json:"clusterLinks"`
	ApplicationMetrics bool `json:"applicationMetrics"`
	SystemLogs         bool `json:"systemLogs"`
	KubernetesEvents   bool `json:"kubernetesEvents"`
	ApplicationLogs    bool `json:"applicationLogs"`
	Traces             bool `json:"traces"`
	Accelerators       bool `json:"accelerators"`
}

// TelemetryRequest is what has been asked of one agent's cluster. It is kept per agent and survives restarts of the server. Revision rises
// with every change, taking the request back included, so what is in force can always be told from what was asked.
type TelemetryRequest struct {
	Revision uint64 `json:"revision"`
	// Signals is the whole set of signals to collect (a signal not true is off). Nil: no request about signals.
	Signals *TelemetrySignals `json:"signals,omitempty"`
	// TracesSamplingPercent is how much of the traces to keep (1-100). Nil: no request, the cluster's own value stands.
	TracesSamplingPercent *int `json:"tracesSamplingPercent,omitempty"`
}

// Requests is whether the request asks for anything. One that does not is the request taken back, and the cluster's own settings stand.
func (t TelemetryRequest) Requests() bool { return t.Signals != nil || t.TracesSamplingPercent != nil }

// TelemetryRequestKey is where an agent's telemetry request is stored: next to its snapshot, under a name no agent id can have.
func TelemetryRequestKey(agentID string) string { return agentID + "#telemetry-request" }

// CleanTelemetryRequest checks what an administrator asked and returns it in the form that is kept. The revision is the server's, not
// the caller's, so it is dropped here.
func CleanTelemetryRequest(in TelemetryRequest) (TelemetryRequest, error) {
	out := TelemetryRequest{Revision: in.Revision}
	if in.Signals != nil {
		s := *in.Signals
		out.Signals = &s
	}
	if p := in.TracesSamplingPercent; p != nil {
		if *p < 1 || *p > 100 {
			return TelemetryRequest{}, kernel.Errf(kernel.KindInvalid, "the share of traces to keep is a whole number from 1 to 100, not %d", *p)
		}
		if out.Signals != nil && !out.Signals.Traces {
			return TelemetryRequest{}, kernel.Errf(kernel.KindInvalid, "a share of traces to keep needs the traces signal to be on")
		}
		v := *p
		out.TracesSamplingPercent = &v
	}
	return out, nil
}

// TelemetryWidens is whether next collects more than cur: a signal turned on that was not, or a larger share of the traces. A request
// that only turns things off or down is open to an editor; one that turns anything on takes an administrator. With no request in force
// the cluster's own settings are not known here, so every signal counts as off and the share of traces as all of them: the first request
// to turn a signal on is a widening, and the first to keep less is not.
func TelemetryWidens(cur, next TelemetryRequest) bool {
	var was TelemetrySignals
	if cur.Signals != nil {
		was = *cur.Signals
	}
	if next.Signals != nil {
		n, w := *next.Signals, was
		if n.ResourceUsage && !w.ResourceUsage || n.Energy && !w.Energy || n.KubernetesState && !w.KubernetesState ||
			n.NodeRuntime && !w.NodeRuntime || n.NetworkLatency && !w.NetworkLatency || n.ClusterLinks && !w.ClusterLinks ||
			n.ApplicationMetrics && !w.ApplicationMetrics || n.SystemLogs && !w.SystemLogs || n.KubernetesEvents && !w.KubernetesEvents ||
			n.ApplicationLogs && !w.ApplicationLogs || n.Traces && !w.Traces || n.Accelerators && !w.Accelerators {
			return true
		}
	}
	share := func(t TelemetryRequest) int {
		if t.TracesSamplingPercent == nil {
			return 100
		}
		return *t.TracesSamplingPercent
	}
	return next.TracesSamplingPercent != nil && share(next) > share(cur)
}

// SameTelemetryRequest is whether two requests ask for the same thing, whatever their revisions.
func SameTelemetryRequest(a, b TelemetryRequest) bool {
	if (a.Signals == nil) != (b.Signals == nil) || a.Signals != nil && *a.Signals != *b.Signals {
		return false
	}
	if (a.TracesSamplingPercent == nil) != (b.TracesSamplingPercent == nil) {
		return false
	}
	return a.TracesSamplingPercent == nil || *a.TracesSamplingPercent == *b.TracesSamplingPercent
}

// Proto is the request as the agent is sent it.
func (t TelemetryRequest) Proto() *ikhnosv1.TelemetryRequest {
	out := &ikhnosv1.TelemetryRequest{Revision: t.Revision}
	if s := t.Signals; s != nil {
		out.Signals = &ikhnosv1.TelemetrySignals{
			ResourceUsage: s.ResourceUsage, Energy: s.Energy, KubernetesState: s.KubernetesState, NodeRuntime: s.NodeRuntime,
			NetworkLatency: s.NetworkLatency, ClusterLinks: s.ClusterLinks, ApplicationMetrics: s.ApplicationMetrics, SystemLogs: s.SystemLogs,
			KubernetesEvents: s.KubernetesEvents, ApplicationLogs: s.ApplicationLogs, Traces: s.Traces, Accelerators: s.Accelerators,
		}
	}
	if p := t.TracesSamplingPercent; p != nil {
		v := uint32(*p)
		out.TracesSamplingPercentage = &v
	}
	return out
}

// The states of a request, as the document says them.
const (
	// TelemetryWaiting: nothing has been heard about this revision yet.
	TelemetryWaiting = "waiting"
	// The rest are what the cluster said (TelemetryRelayStatus.state).
	TelemetryWritten  = "written"
	TelemetryApplied  = "applied"
	TelemetryRefused  = "refused"
	TelemetryReverted = "reverted"
)

// TelemetryRelay is what an agent last said became of a request, as the server holds it (in memory: the agent says it again in every
// heartbeat).
type TelemetryRelay struct {
	Revision uint64
	State    string
	Reason   string
}

// TelemetryRequestDoc is a request next to what the cluster said became of it.
type TelemetryRequestDoc struct {
	Revision              uint64            `json:"revision"`
	Signals               *TelemetrySignals `json:"signals,omitempty"`
	TracesSamplingPercent *int              `json:"tracesSamplingPercent,omitempty"`
	// Requested is whether there is a request in force at all. False: the cluster's own settings stand.
	Requested bool `json:"requested"`
	// State is where the latest revision got to: waiting, written, applied, refused or reverted. Never from what the server last sent
	// alone: "applied" is only said when the cluster said it about this revision.
	State string `json:"state"`
	// Reason is the cluster's own sentence for a refusal or a revert.
	Reason string `json:"reason,omitempty"`
	// SetAt is when the request was last changed, present only while the cluster has not yet applied it.
	SetAt string `json:"setAt,omitempty"`
}

// TelemetryRequestDocOf is the document for a request and what the agent last said; setAt is zero if the request never changed.
func TelemetryRequestDocOf(t TelemetryRequest, relay *TelemetryRelay, setAt string) *TelemetryRequestDoc {
	d := &TelemetryRequestDoc{Revision: t.Revision, Signals: t.Signals, TracesSamplingPercent: t.TracesSamplingPercent, Requested: t.Requests(), State: TelemetryWaiting}
	if relay != nil && relay.Revision == t.Revision && t.Revision > 0 {
		d.State, d.Reason = relay.State, relay.Reason
	}
	if d.State != TelemetryApplied && setAt != "" {
		d.SetAt = setAt
	}
	return d
}

// CleanRelayState keeps what an agent said about a request to what a page can show: the state to one of the known words (anything else
// is shown as waiting, never as success), the reason cut short and made printable.
func CleanRelayState(r *ikhnosv1.TelemetryRelayStatus) *TelemetryRelay {
	if r == nil {
		return nil
	}
	state := r.State
	switch state {
	case TelemetryWritten, TelemetryApplied, TelemetryRefused, TelemetryReverted:
	default:
		state = TelemetryWaiting
	}
	return &TelemetryRelay{Revision: r.Revision, State: state, Reason: kernel.Printable(r.Reason, 300)}
}
