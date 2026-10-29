package agent

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/agent/collect"
	"ikhnos/internal/flow"
	"ikhnos/internal/link"
	"ikhnos/internal/linkhub"
)

var hubTestSecret = []byte("hub-secret-0123456789abcdef0123")

type hubRig struct {
	r     *runner
	hub   *linkhub.Hub
	meter *link.Meter
	f     *linkhub.Forwarder
}

func newHubRig(t *testing.T, iperf bool) *hubRig {
	t.Helper()
	m := link.NewMeter(link.NewPeers())
	m.ServeIperf([]byte("iperf-secret-0123456789abcdef"))
	m.ServePeers([]byte("peers-secret-0123456789abcdef"))
	hub := linkhub.New(hubTestSecret, m, nil)
	srv := httptest.NewServer(hub.Handler())
	t.Cleanup(srv.Close)
	f := linkhub.NewForwarder(srv.URL, hubTestSecret, nil, nil)
	r := newTestRunner()
	r.cfg.LinkHub = &LinkHub{Forwarder: f, Install: link.Install{Probe: true, Capacity: true, TelemetryExport: true}, Iperf: iperf}
	return &hubRig{r: r, hub: hub, meter: m, f: f}
}

func hubConfig() *ikhnosv1.Config {
	return &ikhnosv1.Config{
		ClusterId: "cl-here",
		LinkPeers: []*ikhnosv1.LinkPeer{
			{ClusterId: "cl-a", Name: "Athens", Addresses: []string{"203.0.113.9"}, Throughput: true, IperfClient: &ikhnosv1.LinkIperfClient{PublicKeyPem: "pub", Username: "ik-test0001", Password: "pw-0123456789abcdef"}},
		},
	}
}

// send runs the forwarder's loop for as long as it takes to deliver, which is within a second.
func (h *hubRig) flushed(t *testing.T, ok func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.f.Run(ctx)
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if ok() {
			return
		}
	}
	t.Fatalf("not delivered: %+v", h.f.Stats())
}

// The peers the server names reach the hub under the rules the agent applied itself: nothing below tier 2 and nothing while the flow collector
// is paused.
func TestThePeersAreSentToTheHubUnderTheRulesOfTheAgent(t *testing.T) {
	h := newHubRig(t, true)
	h.r.applyLinks(hubConfig(), resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 2}, 2))
	h.flushed(t, func() bool { return len(h.meter.Peers.List()) == 1 })
	// (The hub checks the credentials as the agent did: these are not real keys, so it holds none. What is sent is what is asserted.)
	cfg := hubConfig()
	cfg.LinkIperfServer = &ikhnosv1.LinkIperfServer{PrivateKeyPem: "k", UsersCsv: "u"}
	if sent := h.r.linkHubSlice(cfg, cfg.LinkPeers, cfg.LinkIperfServer); sent.LinkIperfServer == nil || sent.LinkPeers[0].IperfClient == nil {
		t.Error("the credentials of the throughput tests were not passed on by an install that hands them out")
	}
	for name, o := range map[string]overrides{
		"tier 1":                    resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 1}, 2),
		"the flow collector paused": resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 2, PausedCollectors: []string{collectorFlow}}, 2),
	} {
		h.r.applyLinks(hubConfig(), o)
		h.flushed(t, func() bool { return len(h.meter.Peers.List()) == 0 })
		_ = name
		h.r.applyLinks(hubConfig(), resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 2}, 2))
		h.flushed(t, func() bool { return len(h.meter.Peers.List()) == 1 })
	}
}

func TestAnInstallThatHandsOutNoCredentialsPassesNoneToTheHub(t *testing.T) {
	h := newHubRig(t, false)
	cfg := hubConfig()
	cfg.LinkIperfServer = &ikhnosv1.LinkIperfServer{PrivateKeyPem: "k", UsersCsv: "u"}
	h.r.applyLinks(cfg, resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 2}, 2))
	if sent := h.r.linkHubSlice(cfg, cfg.LinkPeers, cfg.LinkIperfServer); sent.LinkIperfServer != nil || sent.LinkPeers[0].IperfClient != nil {
		t.Error("a credential that no pod of the install will ask for was sent to the hub")
	}
	// The agent's own copy of the Config is not changed by leaving the credentials out of what is sent.
	if cfg.LinkPeers[0].IperfClient == nil || cfg.LinkIperfServer == nil {
		t.Error("the agent's Config lost its credentials")
	}
}

// The rows the flow pipeline admits reach the hub, and the series are what the agent would have made.
func TestAdmittedRowsReachTheHubThroughTheFlowPipeline(t *testing.T) {
	h := newHubRig(t, false)
	p := flow.NewPipeline([]byte("flow-secret-0123456789abcdef0123"), func() *collect.Index { return nil }, nil)
	p.Links = h.f
	h.r.applyLinks(hubConfig(), resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 2}, 2))
	p.Ingest(&ikhnosv1.FlowReport{Method: "ebpf", Node: "n1", BytesKnown: true, Flows: []*ikhnosv1.RawFlow{
		{Client: true, LocalIp: "10.42.0.7", PeerIp: "203.0.113.9", Port: 443, Protocol: "tcp", Connections: 4, BytesOut: 100, BytesIn: 200},
	}})
	h.flushed(t, func() bool {
		var b strings.Builder
		h.meter.Write(&b)
		return strings.Contains(b.String(), `ikhnos_link_connections_total{cluster="cl-here",peer="cl-a"} 4`)
	})
}

func TestTheCapabilitiesComeFromTheInstallAndFromTheHubsLastAnswer(t *testing.T) {
	h := newHubRig(t, true)
	c := h.r.linkCapabilities()
	if !c.LinkMetrics || !c.ProbeEnabled || !c.CapacityEnabled || !c.TelemetryExport || c.ProberSeenUnix != 0 {
		t.Fatalf("capabilities = %v", c)
	}
	h.r.applyLinks(hubConfig(), resolveOverrides(&ikhnosv1.Config{ApprovedAccessTier: 2}, 2))
	h.flushed(t, func() bool { return len(h.meter.Peers.List()) == 1 })
	// The prober asks the hub; the agent learns of it from the hub's next answer.
	pods := httptest.NewServer(h.meter.Handler())
	defer pods.Close()
	if _, err := link.FetchPeers(context.Background(), pods.Client(), pods.URL, []byte("peers-secret-0123456789abcdef"), time.Now()); err != nil {
		t.Fatal(err)
	}
	h.flushed(t, func() bool { return h.r.linkCapabilities().ProberSeenUnix != 0 && h.f.Stats().Reached })
	if got := h.r.linkCapabilities().ProberSeenUnix; time.Since(time.Unix(got, 0)) > time.Minute {
		t.Errorf("prober seen = %v", time.Unix(got, 0))
	}
	// Without a hub and without a meter, Cluster links is off.
	if c := newTestRunner().linkCapabilities(); c.LinkMetrics {
		t.Error("links reported on with nothing to make them")
	}
}

func TestAHubThatDoesNotAnswerIsSaidSoOnceItHasNotAnsweredForAWhile(t *testing.T) {
	r := newTestRunner()
	f := linkhub.NewForwarder("http://127.0.0.1:1", hubTestSecret, nil, nil)
	r.cfg.LinkHub = &LinkHub{Forwarder: f}
	now := time.Now()
	if msg := r.linkHubProblem(now); msg != "" {
		t.Fatalf("a hub that was never asked yet: %s", msg)
	}
	msg := r.linkHubProblem(now.Add(linkHubUnreachableAfter + time.Second))
	if !strings.Contains(msg, "cluster-link hub has not answered") || !strings.Contains(msg, "kubectl") {
		t.Fatalf("message = %q", msg)
	}
	r.diagnostics()
	if p := codesOf(r)[CodeLinkHubUnreachable]; p != nil {
		t.Errorf("raised at once: %v", p)
	}
	var none *runner = newTestRunner()
	if none.linkHubProblem(now) != "" {
		t.Error("a runner with no hub has no hub problem")
	}
}
