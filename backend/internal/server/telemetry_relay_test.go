package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/accounts"
	"ikhnos/internal/agents"
	"ikhnos/internal/testkit"
)

// ---- asking a cluster to collect more or less (the server's half of the telemetry relay) ----

func TestAskingAClusterToCollectIsCheckedByWhoAsksAndPersisted(t *testing.T) {
	a := newAdminRig(t)
	_, admin := a.user(t, "ada", accounts.RoleAdmin)
	_, editor := a.user(t, "edith", accounts.RoleEditor)
	_, viewer := a.user(t, "vic", accounts.RoleViewer)
	id, _, _ := a.approvedAgent(t, testkit.FP)
	ask := func(cookie string, body any) resp {
		return a.do("POST", "/api/v1/agents/"+id+"/telemetry", body, withCookie(cookie))
	}
	on := map[string]any{"signals": map[string]any{"resourceUsage": true, "traces": true}, "tracesSamplingPercent": 20}

	if c := ask(viewer, on).Code; c != 403 {
		t.Fatalf("a viewer: %d", c)
	}
	for name, body := range map[string]map[string]any{
		"a share of 0":                                   {"tracesSamplingPercent": 0},
		"a share above 100":                              {"tracesSamplingPercent": 101},
		"a share without the traces":                     {"signals": map[string]any{"resourceUsage": true}, "tracesSamplingPercent": 10},
		"a destination, which the body has no place for": {"destination": map[string]any{"endpoint": "evil.example.com:4317"}},
	} {
		if code := ask(admin, body).Code; code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}

	// Turning anything on takes an administrator; an editor may only ask for less.
	if c := ask(editor, on).Code; c != 403 {
		t.Fatalf("an editor turning signals on: %d", c)
	}
	r := ask(admin, on)
	if r.Code != 200 {
		t.Fatalf("an administrator: %d %s", r.Code, r.Body.String())
	}
	var doc agents.TelemetryRequestDoc
	if err := json.Unmarshal(r.Body.Bytes(), &doc); err != nil || !doc.Requested || doc.State != agents.TelemetryWaiting || doc.TracesSamplingPercent == nil || *doc.TracesSamplingPercent != 20 || doc.SetAt == "" {
		t.Fatalf("answer %+v %v", doc, err)
	}
	first := doc.Revision

	// The same request again changes nothing: no new revision, so nothing is sent again.
	if r := ask(admin, on); r.Code != 200 {
		t.Fatal(r.Body.String())
	} else if json.Unmarshal(r.Body.Bytes(), &doc); doc.Revision != first {
		t.Fatalf("the same request made revision %d after %d", doc.Revision, first)
	}
	// An editor may ask for less.
	if r := ask(editor, map[string]any{"signals": map[string]any{"traces": true}, "tracesSamplingPercent": 5}); r.Code != 200 {
		t.Fatalf("an editor asking for less: %d %s", r.Code, r.Body.String())
	} else if json.Unmarshal(r.Body.Bytes(), &doc); doc.Revision != first+1 || doc.Signals.ResourceUsage {
		t.Fatalf("answer %+v", doc)
	}
	// And take the request back, which is a revision of its own with nothing in it.
	if r := ask(editor, map[string]any{}); r.Code != 200 {
		t.Fatalf("taking it back: %d %s", r.Code, r.Body.String())
	} else if json.Unmarshal(r.Body.Bytes(), &doc); doc.Revision != first+2 || doc.Requested {
		t.Fatalf("answer %+v", doc)
	}

	evs, _ := a.st.ListAudit(a.ctx, "org-1", 50)
	n := 0
	for _, e := range evs {
		if e.Action == "agent-telemetry-requested" {
			n++
			if strings.Contains(e.Detail, "evil.example.com") {
				t.Fatalf("a destination reached the audit trail: %s", e.Detail)
			}
		}
	}
	if n != 3 {
		t.Fatalf("%d audit rows: %+v", n, evs)
	}

	// Only people who may change an agent's access see it, and it outlives the server.
	if d := agentDoc(t, a, viewer, id); d.Telemetry != nil {
		t.Fatalf("a viewer sees %+v", d.Telemetry)
	}
	if d := agentDoc(t, a, editor, id); d.Telemetry == nil || d.Telemetry.Revision != first+2 {
		t.Fatalf("an editor sees %+v", d.Telemetry)
	}
	tn, err := NewPlatform(a.base, nil).Tenant(a.ctx, "org-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := tn.Hub.TelemetryRequestOf(id); got.Revision != first+2 {
		t.Fatalf("after a restart the revision is %d, not %d: a revision must never be used twice", got.Revision, first+2)
	}
}

// A request reaches an agent that is connected, in its Config, and the agent's word about it comes back into the page.
func TestTheRequestReachesTheAgentAndTheAnswerComesBack(t *testing.T) {
	r := newHubRig(t)
	f := r.newFakeAgent(t, fakeAgentCtx(t))
	if first := f.Configs[0]; first.TelemetryRequest != nil {
		t.Fatalf("a cluster nobody asked anything of was sent a request: %v", first.TelemetryRequest)
	}
	one := 30
	asAdmin := testkit.AsRole(accounts.RoleAdmin)
	if _, err := r.hub.SetTelemetryRequest(asAdmin, "ada", f.ID, agents.TelemetryRequest{Signals: &agents.TelemetrySignals{Traces: true}, TracesSamplingPercent: &one}); err != nil {
		t.Fatal(err)
	}
	cfg := f.NextConfig(5 * time.Second)
	if cfg == nil || cfg.TelemetryRequest.GetRevision() != 1 || !cfg.TelemetryRequest.GetSignals().GetTraces() || cfg.TelemetryRequest.GetTracesSamplingPercentage() != 30 {
		t.Fatalf("the agent was sent %v", cfg)
	}

	state := func() *agents.TelemetryRequestDoc {
		d := r.agentDoc(t, f.ID)
		return d.Telemetry
	}
	if d := state(); d == nil || d.State != agents.TelemetryWaiting {
		t.Fatalf("before the agent says anything: %+v", d)
	}
	beat := func(rs *ikhnosv1.TelemetryRelayStatus) {
		t.Helper()
		if err := f.Send(&ikhnosv1.AgentMessage{Msg: &ikhnosv1.AgentMessage_Heartbeat{Heartbeat: &ikhnosv1.Heartbeat{AppliedConfigDigest: cfg.Digest, TelemetryRelay: rs}}}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor := func(state string) {
		t.Helper()
		f.WaitUntil("the page to say "+state, func() bool { d := state_(r, f.ID); return d != nil && d.State == state })
	}
	beat(&ikhnosv1.TelemetryRelayStatus{Revision: 1, State: "written"})
	waitFor(agents.TelemetryWritten)
	beat(&ikhnosv1.TelemetryRelayStatus{Revision: 1, State: "refused", Reason: "the relay is not enabled in this cluster"})
	waitFor(agents.TelemetryRefused)
	if d := state(); d.Reason != "the relay is not enabled in this cluster" {
		t.Fatalf("the cluster's own reason is not shown: %+v", d)
	}
	// An answer about an older revision is not an answer about this one, and a word the server does not know is never success.
	beat(&ikhnosv1.TelemetryRelayStatus{Revision: 0, State: "applied"})
	f.WaitUntil("the page to forget", func() bool { d := state_(r, f.ID); return d != nil && d.State == agents.TelemetryWaiting })
	beat(&ikhnosv1.TelemetryRelayStatus{Revision: 1, State: "thriving"})
	f.WaitUntil("an unknown state to show as waiting", func() bool { d := state_(r, f.ID); return d != nil && d.State == agents.TelemetryWaiting })
	beat(&ikhnosv1.TelemetryRelayStatus{Revision: 1, State: "applied"})
	waitFor(agents.TelemetryApplied)
	if d := state(); d.SetAt != "" {
		t.Fatalf("an applied request still says it is waiting since %s", d.SetAt)
	}

	// A restarted agent is sent the request in its first Config, so a cluster that missed it while it was away is not left out.
	if again := f.restart(); again.TelemetryRequest.GetRevision() != 1 {
		t.Fatalf("a reconnecting agent was sent %v", again.TelemetryRequest)
	}
	// Taking it back is sent too: an agent that was asked must be told that it no longer is.
	if _, err := r.hub.SetTelemetryRequest(asAdmin, "ada", f.ID, agents.TelemetryRequest{}); err != nil {
		t.Fatal(err)
	}
	if back := f.NextConfig(5 * time.Second); back == nil || back.TelemetryRequest == nil || back.TelemetryRequest.Revision != 2 || back.TelemetryRequest.Signals != nil {
		t.Fatalf("the agent was sent %v", back)
	}
}

func state_(r *hubRig, id string) *agents.TelemetryRequestDoc {
	s, err := r.hub.State(context.Background())
	if err != nil {
		return nil
	}
	for _, a := range s.Agents {
		if a.ID == id {
			return a.Telemetry
		}
	}
	return nil
}
