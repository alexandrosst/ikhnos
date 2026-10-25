package agent_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"ikhnos/internal/accounts"
	"ikhnos/internal/agent"
	"ikhnos/internal/agents"
	"ikhnos/internal/relay"
	"ikhnos/internal/testkit"
)

// memRelay is a status: what the agent wrote, and what a controller (the test) says back.
type memRelay struct {
	mu     sync.Mutex
	wrote  []relay.Request
	answer *relay.Result
}

func (m *memRelay) Request(_ context.Context, r relay.Request) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wrote = append(m.wrote, r)
	return nil
}

func (m *memRelay) Result(context.Context) (*relay.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.answer, nil
}

func (m *memRelay) say(res relay.Result) { m.mu.Lock(); m.answer = &res; m.mu.Unlock() }
func (m *memRelay) writes() []relay.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]relay.Request(nil), m.wrote...)
}

// The whole way, with a real server and a real agent: the administrator asks, the agent hands the request on, the controller (the test)
// answers, and the page of the server says what became of it. A cluster where the relay is off says so instead.
func TestTheServerAsksTheAgentHandsOnTheControllerAnswersAndTheServerSaysWhatBecameOfIt(t *testing.T) {
	r := newERig(t, nil)
	ids := agent.FileStore{Dir: filepath.Join(t.TempDir(), "id")}
	log, _ := newLog()
	store := &memRelay{}
	cfg := r.cfg(ids, r.token("e2e"), log)
	cfg.ControllerResource, cfg.RelayTelemetry, cfg.Relay, cfg.RelayEvery = "this-cluster", true, store, 100*time.Millisecond
	stop, _ := r.start(cfg)
	defer stop()
	code := codeOf(t, ids)
	waitFor(t, "pending agent", 10*time.Second, func() bool { return len(r.agents()) == 1 })
	id := r.agents()[0].ID
	if err := r.core.Agents.Approve(context.Background(), "test", id, code, 2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first sync", 15*time.Second, r.synced)

	state := func() string {
		for _, a := range r.agents() {
			if a.ID == id && a.Telemetry != nil {
				return a.Telemetry.State
			}
		}
		return ""
	}
	pct := 25
	if _, err := r.hub.SetTelemetryRequest(testkit.AsRole(accounts.RoleAdmin), "ada", id, agents.TelemetryRequest{Signals: &agents.TelemetrySignals{Traces: true}, TracesSamplingPercent: &pct}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the agent to write the request", 15*time.Second, func() bool { return len(store.writes()) == 1 })
	if w := store.writes()[0]; w.Revision != 1 || w.Signals == nil || !w.Signals.Traces || w.TracesSamplingPercentage == nil || *w.TracesSamplingPercentage != 25 {
		t.Fatalf("the agent wrote %+v", w)
	}
	waitFor(t, "the page to say written", 15*time.Second, func() bool { return state() == agents.TelemetryWritten })

	store.say(relay.Result{Revision: 1, State: relay.Applied})
	waitFor(t, "the page to say applied", 15*time.Second, func() bool { return state() == agents.TelemetryApplied })
	if n := len(store.writes()); n != 1 {
		t.Fatalf("the request was written %d times", n)
	}
}

func TestAClusterWithTheRelayOffTellsTheServerWhyAndWritesNothing(t *testing.T) {
	r := newERig(t, nil)
	ids := agent.FileStore{Dir: filepath.Join(t.TempDir(), "id")}
	log, _ := newLog()
	store := &memRelay{}
	cfg := r.cfg(ids, r.token("e2e"), log)
	cfg.ControllerResource, cfg.RelayTelemetry, cfg.Relay = "this-cluster", false, store // the controller keeps it; the relay is not on
	stop, _ := r.start(cfg)
	defer stop()
	code := codeOf(t, ids)
	waitFor(t, "pending agent", 10*time.Second, func() bool { return len(r.agents()) == 1 })
	id := r.agents()[0].ID
	if err := r.core.Agents.Approve(context.Background(), "test", id, code, 2); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "first sync", 15*time.Second, r.synced)
	if _, err := r.hub.SetTelemetryRequest(testkit.AsRole(accounts.RoleAdmin), "ada", id, agents.TelemetryRequest{Signals: &agents.TelemetrySignals{Traces: true}}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the page to say refused, in words", 15*time.Second, func() bool {
		for _, a := range r.agents() {
			if a.ID == id && a.Telemetry != nil && a.Telemetry.State == agents.TelemetryRefused && len(a.Telemetry.Reason) > 20 {
				return true
			}
		}
		return false
	})
	if n := len(store.writes()); n != 0 {
		t.Fatalf("a cluster with the relay off had a request written: %d", n)
	}
}
