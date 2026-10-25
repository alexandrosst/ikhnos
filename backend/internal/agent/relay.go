package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/relay"
)

// The telemetry relay, the agent's part. The server may ask this cluster to collect something (Config.telemetry_request: which signals,
// how much of the traces). The agent does not apply it and cannot: it holds almost no rights in the cluster. It hands the request to the
// controller that keeps it installed, by writing it under status.requested of its own IkhnosAgent, and reads back under status.relay what
// the controller made of it, which it tells the server in every heartbeat. The controller checks the request against what the cluster's
// owner allowed, applies it with Helm, or says why not. Nothing else passes: not where data goes, and no credential (package relay).

// RelayStore is where the agent puts a request and where it reads what became of it.
type RelayStore interface {
	// Request writes the request where the controller reads it, and says so as an error only if it did not stay.
	Request(ctx context.Context, req relay.Request) error
	// Result is what the controller last said, or nil if it has said nothing yet.
	Result(ctx context.Context) (*relay.Result, error)
}

// ResourceRelayStore is the store in the status of the IkhnosAgent called name in namespace. It writes and reads that subresource of that one
// object and nothing else: the chart gives the agent the right to patch it, and to get it only when the relay is on.
func ResourceRelayStore(kube kubernetes.Interface, namespace, name string) RelayStore {
	return &resourceRelay{kube: kube, path: fmt.Sprintf("/apis/%s/namespaces/%s/ikhnosagents/%s/status", resourceAPIVersion, namespace, name), name: namespace + "/" + name}
}

type resourceRelay struct {
	kube kubernetes.Interface
	path string
	name string
}

func (s *resourceRelay) Request(ctx context.Context, req relay.Request) error {
	body, err := json.Marshal(map[string]any{"status": map[string]any{relay.StatusRequested: requestPatch(req)}})
	if err != nil {
		return err
	}
	raw, err := s.kube.CoreV1().RESTClient().Patch(types.MergePatchType).AbsPath(s.path).Body(body).DoRaw(ctx)
	if err != nil {
		return fmt.Errorf("patching the status of the IkhnosAgent %s: %w", s.name, err)
	}
	// What the API server answers is checked: a definition of the resource from before status.requested drops it without a word (Helm
	// never updates a CRD it installed).
	var got struct {
		Status struct {
			Requested *relay.Request `json:"requested"`
		} `json:"status"`
	}
	if json.Unmarshal(raw, &got) != nil || got.Status.Requested == nil || got.Status.Requested.Revision != req.Revision {
		return fmt.Errorf("the IkhnosAgent %s did not keep status.requested: its definition in this cluster is older than the controller's; apply crds/ikhnosagents.yaml from the controller's chart", s.name)
	}
	return nil
}

// fullSignals is relay.Signals with every signal written, the ones that are off too (relay.Signals leaves them out).
type fullSignals struct {
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

// requestPatch is the request as a merge patch that REPLACES the last one. A merge patch merges: a request that asks for less than the last
// (a signal turned off, no share of the traces, taken back) would keep what it no longer asks for, so every key it does not have is
// written as null, and every signal is written, off ones as false.
func requestPatch(req relay.Request) map[string]any {
	p := map[string]any{"revision": req.Revision, "signals": nil, "tracesSamplingPercentage": nil}
	if req.Signals != nil {
		p["signals"] = fullSignals(*req.Signals)
	}
	if req.TracesSamplingPercentage != nil {
		p["tracesSamplingPercentage"] = *req.TracesSamplingPercentage
	}
	return p
}

func (s *resourceRelay) Result(ctx context.Context) (*relay.Result, error) {
	raw, err := s.kube.CoreV1().RESTClient().Get().AbsPath(s.path).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the status of the IkhnosAgent %s: %w", s.name, err)
	}
	var got struct {
		Status struct {
			Relay *relay.Result `json:"relay"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		return nil, fmt.Errorf("the status of the IkhnosAgent %s cannot be read: %w", s.name, err)
	}
	return got.Status.Relay, nil
}

// relayState is what the agent knows of the request it was last sent and what became of it.
type relayState struct {
	mu      sync.Mutex
	req     *relay.Request // the latest request, nil before the server has sent one
	pending bool           // it has not yet been written where the controller reads it
	status  *ikhnosv1.TelemetryRelayStatus
	wake    chan struct{} // the loop has something to hand on
	changed chan struct{} // what the heartbeat says has changed: the stream sends one now, not at the next beat
}

func newRelayState() *relayState {
	return &relayState{wake: make(chan struct{}, 1), changed: make(chan struct{}, 1)}
}

// say sets what the heartbeat says and lets the stream know. The caller holds st.mu.
func (st *relayState) say(s *ikhnosv1.TelemetryRelayStatus) {
	st.status = s
	select {
	case st.changed <- struct{}{}:
	default:
	}
}

// relayChanges is signalled when the status the heartbeat carries has changed; nil (never ready) in a runner with no relay state.
func (r *runner) relayChanges() <-chan struct{} {
	if r.relay == nil {
		return nil
	}
	return r.relay.changed
}

// requestOf is a Config's request in the words of the status, nil if the server sent none.
func requestOf(c *ikhnosv1.Config) *relay.Request {
	t := c.GetTelemetryRequest()
	if t == nil {
		return nil
	}
	out := &relay.Request{Revision: t.Revision}
	if s := t.Signals; s != nil {
		out.Signals = &relay.Signals{
			ResourceUsage: s.ResourceUsage, Energy: s.Energy, KubernetesState: s.KubernetesState, NodeRuntime: s.NodeRuntime,
			NetworkLatency: s.NetworkLatency, ClusterLinks: s.ClusterLinks, ApplicationMetrics: s.ApplicationMetrics, SystemLogs: s.SystemLogs,
			KubernetesEvents: s.KubernetesEvents, ApplicationLogs: s.ApplicationLogs, Traces: s.Traces, Accelerators: s.Accelerators,
		}
	}
	if p := t.TracesSamplingPercentage; p != nil {
		v := int(*p)
		out.TracesSamplingPercentage = &v
	}
	return out
}

// noteRelayRequest is what the agent does with the request in a Config it applied: nothing if there is none or it is the one it has, else
// it takes the request and, when this cluster has the relay on, hands it on. A cluster that has not turned the relay on says so, at once
// and in words, instead of leaving the request waiting for ever.
func (r *runner) noteRelayRequest(c *ikhnosv1.Config) {
	req := requestOf(c)
	if req == nil {
		return
	}
	st := r.relay
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.req != nil && st.req.Revision == req.Revision {
		return
	}
	st.req = req
	switch {
	case r.cfg.ControllerResource == "":
		st.pending = false
		st.say(&ikhnosv1.TelemetryRelayStatus{Revision: req.Revision, State: relay.Refused, Reason: "nothing keeps this agent installed, so there is nothing here to apply a request: it is changed with helm, or by an IkhnosAgent"})
	case !r.cfg.RelayTelemetry || r.cfg.Relay == nil:
		st.pending = false
		st.say(&ikhnosv1.TelemetryRelayStatus{Revision: req.Revision, State: relay.Refused, Reason: "this cluster has not turned the relay on (relay.telemetry in the controller's chart), so it keeps its own settings"})
	default:
		st.pending = true
		st.say(nil) // not said until it is written
		select {
		case st.wake <- struct{}{}:
		default:
		}
	}
}

// relayStatus is what the heartbeat says about the request, nil while there is nothing to say.
func (r *runner) relayStatus() *ikhnosv1.TelemetryRelayStatus {
	st := r.relay
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.status == nil {
		return nil
	}
	c := st.status
	return &ikhnosv1.TelemetryRelayStatus{Revision: c.Revision, State: c.State, Reason: c.Reason}
}

// relayLoop hands a pending request on, and follows what the controller says about it. It does nothing at all in a cluster that has not
// turned the relay on, and nothing while there is no request.
func (r *runner) relayLoop(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.relay.wake:
		}
		r.relayStep(ctx)
	}
}

// relayStep is one pass of relayLoop.
func (r *runner) relayStep(ctx context.Context) {
	st := r.relay
	st.mu.Lock()
	req, pending := st.req, st.pending
	st.mu.Unlock()
	if req == nil || r.cfg.Relay == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if pending {
		err := r.cfg.Relay.Request(c, *req)
		st.mu.Lock()
		defer st.mu.Unlock()
		if st.req == nil || st.req.Revision != req.Revision {
			return // a newer request came while this was being written: the next pass writes that one
		}
		if err != nil {
			r.log.Warn("could not hand the telemetry request to the controller", "revision", req.Revision, "err", err)
			st.say(&ikhnosv1.TelemetryRelayStatus{Revision: req.Revision, State: relay.Refused, Reason: "the agent could not hand the request to the controller: " + clipMsg(err.Error(), 280)})
			return // still pending: tried again on the next pass
		}
		st.pending = false
		st.say(&ikhnosv1.TelemetryRelayStatus{Revision: req.Revision, State: relay.Written})
		r.log.Info("handed the telemetry request to the controller", "revision", req.Revision)
		return
	}
	res, err := r.cfg.Relay.Result(c)
	if err != nil {
		r.log.Warn("could not read what the controller made of the telemetry request", "revision", req.Revision, "err", err)
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if res == nil || st.req == nil || res.Revision != st.req.Revision {
		return // nothing yet about this revision: it stays as it was said
	}
	switch res.State {
	case relay.Applied, relay.Refused, relay.Reverted:
		st.say(&ikhnosv1.TelemetryRelayStatus{Revision: res.Revision, State: res.State, Reason: clipMsg(res.Reason, 300)})
	}
}
