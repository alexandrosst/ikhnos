package flow

import (
	"log/slog"
	"net/http"
	"regexp"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/agent/collect"
	"ikhnos/internal/flow/wire"
	"ikhnos/internal/link"
	"ikhnos/internal/probe"

	"google.golang.org/protobuf/encoding/protojson"
)

// The wire constants live in package wire, which the collector links without the rest of this package; they are named here for the agent's side.
const (
	PathReport  = wire.PathReport
	MaxBody     = wire.MaxBody
	MaxRawFlows = wire.MaxRawFlows
)

var nodeName = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]{0,251}[a-zA-Z0-9])?$`)

// Pipeline is the agent's side of traffic observation: it verifies collectors' reports (signed with a
// secret only the flow collectors hold, not the one the node probe uses), attributes them to workloads
// and gathers them into windows for the server.
type Pipeline struct {
	secret []byte
	// Window is how far a report's timestamp may be from this clock; zero means probe.MaxClockSkew (5 minutes).
	Window time.Duration
	replay *probe.ReplayCache
	limit  *probe.SourceLimit
	log    *slog.Logger
	now    func() time.Time

	Resolver   *Resolver
	Aggregator *Aggregator
	// Links, when set, also labels each report's rows by the peer cluster they went to or came from (see package link).
	// It only sees reports that were not thrown away because the aggregator is paused.
	Links *link.Meter
}

func NewPipeline(secret []byte, source func() *collect.Index, log *slog.Logger) *Pipeline {
	if log == nil {
		log = slog.Default()
	}
	return &Pipeline{secret: secret, replay: probe.NewReplayCache(0), limit: probe.DefaultSourceLimit(), log: log, now: time.Now, Resolver: NewResolver(source), Aggregator: NewAggregator()}
}

// Handler serves POST /v1/flows.
func (p *Pipeline) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+PathReport, func(w http.ResponseWriter, req *http.Request) {
		_, body, status, err := probe.ReadSigned(w, req, probe.Signed{Secret: p.secret, MaxBody: MaxBody, Window: p.Window, Replay: p.replay, NodeOK: nodeName.MatchString, Now: p.now(), Context: wire.SignContext, Limit: p.limit})
		if err != nil {
			if status == http.StatusUnauthorized || status == http.StatusConflict {
				p.log.Warn("rejected a flow report", "reason", err.Error(), "from", req.RemoteAddr)
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		var rep ikhnosv1.FlowReport
		if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(body, &rep); err != nil || len(rep.Flows) > MaxRawFlows {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if rep.Method != "ebpf" && rep.Method != "conntrack" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if p.Ingest(&rep) {
			w.Header().Set(probe.HeaderPaused, "1")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// SetSourceLimit replaces how fast one source address may send (nil: no limit). The default is probe.DefaultSourceLimit.
func (p *Pipeline) SetSourceLimit(l *probe.SourceLimit) { p.limit = l }

// SetPaused stops (or resumes) taking reports in. While paused a report is read, verified and thrown away, nothing
// is held and nothing is sent: this is what "the server asked the agent to stop observing traffic" does.
func (p *Pipeline) SetPaused(paused bool) {
	if p != nil {
		p.Aggregator.SetPaused(paused)
	}
}

// Ingest attributes one report and adds it to the current window. It reports whether the aggregator
// was paused at the time, i.e. this report's data was read and verified but thrown away rather than
// kept - the handler uses this to tell the collector it was ignored for that reason (see
// probe.HeaderPaused), not merely accepted.
func (p *Pipeline) Ingest(rep *ikhnosv1.FlowReport) bool {
	paused := p.Aggregator.Paused()
	p.Aggregator.Seen(rep.Node, rep.Method, rep.BytesKnown, rep.LinkSaturation, rep.SnatExhaustion, rep.ThermalThrottle)
	p.Aggregator.AddLost(rep.Lost)
	if !paused && p.Links != nil {
		// The link meter counts the traffic of the peer clusters' addresses, whatever the resolver can attribute, but not what
		// the resolver is told to leave out (an excluded namespace) or cannot read (a malformed protocol or port).
		kept := make([]*ikhnosv1.RawFlow, 0, len(rep.Flows))
		for _, raw := range rep.Flows {
			if raw != nil && p.Resolver.Admits(raw) {
				kept = append(kept, raw)
			}
		}
		p.Links.ObserveRows(kept, rep.BytesKnown)
	}
	for _, raw := range rep.Flows {
		if f, ok := p.Resolver.Resolve(raw, rep.Method, rep.BytesKnown); ok {
			p.Aggregator.Add(f)
		}
	}
	return paused
}
