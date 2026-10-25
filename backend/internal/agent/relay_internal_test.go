package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/relay"
)

type fakeRelay struct {
	mu      sync.Mutex
	asked   []relay.Request
	failing bool
	result  *relay.Result
}

func (f *fakeRelay) Request(_ context.Context, req relay.Request) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failing {
		return errors.New("forbidden")
	}
	f.asked = append(f.asked, req)
	return nil
}

func (f *fakeRelay) Result(context.Context) (*relay.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.result, nil
}

func relayRunner(resource string, on bool, store RelayStore) *runner {
	return &runner{
		log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:   Config{ControllerResource: resource, RelayTelemetry: on, Relay: store},
		relay: newRelayState(),
	}
}

func configAsking(rev uint64, traces bool, pct *uint32) *ikhnosv1.Config {
	return &ikhnosv1.Config{TelemetryRequest: &ikhnosv1.TelemetryRequest{Revision: rev, Signals: &ikhnosv1.TelemetrySignals{Traces: traces}, TracesSamplingPercentage: pct}}
}

func TestAConfigWithoutARequestSaysNothingAboutOne(t *testing.T) {
	r := relayRunner("this-cluster", true, &fakeRelay{})
	r.noteRelayRequest(&ikhnosv1.Config{})
	if r.relayStatus() != nil || r.relay.req != nil || r.relay.pending {
		t.Fatalf("a Config with no request started something: %+v", r.relay)
	}
}

// A cluster that did not turn the relay on, or that nothing keeps installed, says so in words at once; the request does not wait for ever.
func TestAClusterThatDoesNotRelaySaysSoInWords(t *testing.T) {
	for name, r := range map[string]*runner{
		"off":     relayRunner("this-cluster", false, nil),
		"no-ctrl": relayRunner("", true, &fakeRelay{}),
	} {
		r.noteRelayRequest(configAsking(5, true, nil))
		st := r.relayStatus()
		if st == nil || st.Revision != 5 || st.State != relay.Refused || len(st.Reason) < 20 {
			t.Fatalf("%s: %+v", name, st)
		}
		if r.relay.pending {
			t.Fatalf("%s: a request that cannot be handed on is waiting to be", name)
		}
	}
	if st := relayRunner("this-cluster", false, nil).relayStatus(); st != nil {
		t.Fatalf("no request, yet %+v", st)
	}
}

func TestARequestIsWrittenOnceThenFollowedUntilTheControllerSaysWhatBecameOfIt(t *testing.T) {
	f := &fakeRelay{}
	r := relayRunner("this-cluster", true, f)
	pct := uint32(20)
	r.noteRelayRequest(configAsking(7, true, &pct))
	if r.relayStatus() != nil {
		t.Fatalf("said something before the request was written: %+v", r.relayStatus())
	}
	r.relayStep(context.Background())
	if len(f.asked) != 1 || f.asked[0].Revision != 7 || f.asked[0].Signals == nil || !f.asked[0].Signals.Traces || *f.asked[0].TracesSamplingPercentage != 20 {
		t.Fatalf("written: %+v", f.asked)
	}
	if st := r.relayStatus(); st == nil || st.State != relay.Written || st.Revision != 7 {
		t.Fatalf("after writing: %+v", st)
	}

	// The same Config again (every Config is applied) writes nothing more.
	r.noteRelayRequest(configAsking(7, true, &pct))
	r.relayStep(context.Background())
	if len(f.asked) != 1 {
		t.Fatalf("the same request was written twice: %+v", f.asked)
	}

	// What the controller said about an older revision is not about this one.
	f.result = &relay.Result{Revision: 6, State: relay.Applied}
	r.relayStep(context.Background())
	if st := r.relayStatus(); st.State != relay.Written {
		t.Fatalf("took an older revision's word: %+v", st)
	}
	f.result = &relay.Result{Revision: 7, State: relay.Refused, Reason: strings.Repeat("x", 900)}
	r.relayStep(context.Background())
	if st := r.relayStatus(); st.State != relay.Refused || len(st.Reason) > 300+len("…") {
		t.Fatalf("after the controller refused: state %q, %d characters of reason", st.State, len(st.Reason))
	}
	f.result = &relay.Result{Revision: 7, State: relay.Reverted, Reason: "the pods did not come up"}
	r.relayStep(context.Background())
	if st := r.relayStatus(); st.State != relay.Reverted || st.Reason != "the pods did not come up" {
		t.Fatalf("after the controller put it back: %+v", st)
	}
}

// A request that cannot be written is tried again, and the server is told why meanwhile.
func TestARequestThatCannotBeWrittenIsTriedAgainAndTheServerIsToldWhy(t *testing.T) {
	f := &fakeRelay{failing: true}
	r := relayRunner("this-cluster", true, f)
	r.noteRelayRequest(configAsking(2, true, nil))
	r.relayStep(context.Background())
	st := r.relayStatus()
	if st == nil || st.State != relay.Refused || !strings.Contains(st.Reason, "forbidden") || !r.relay.pending {
		t.Fatalf("%+v pending=%v", st, r.relay.pending)
	}
	f.mu.Lock()
	f.failing = false
	f.mu.Unlock()
	r.relayStep(context.Background())
	if st := r.relayStatus(); st.State != relay.Written || len(f.asked) != 1 {
		t.Fatalf("after the right came: %+v %+v", st, f.asked)
	}
}

// A newer request while the controller is still on the last one is written in its turn and is the one that is followed.
func TestANewerRequestIsTheOneFollowed(t *testing.T) {
	f := &fakeRelay{result: &relay.Result{Revision: 1, State: relay.Applied}}
	r := relayRunner("this-cluster", true, f)
	r.noteRelayRequest(configAsking(1, true, nil))
	r.relayStep(context.Background())
	r.relayStep(context.Background())
	if st := r.relayStatus(); st.Revision != 1 || st.State != relay.Applied {
		t.Fatalf("%+v", st)
	}
	r.noteRelayRequest(configAsking(2, false, nil))
	if r.relayStatus() != nil {
		t.Fatalf("still saying revision 1 about revision 2: %+v", r.relayStatus())
	}
	r.relayStep(context.Background())
	if st := r.relayStatus(); st.Revision != 2 || st.State != relay.Written || len(f.asked) != 2 {
		t.Fatalf("%+v %+v", st, f.asked)
	}
}

func TestARunnerWithoutTheRelayStateSaysNothing(t *testing.T) {
	r := &runner{}
	r.noteRelayRequest(configAsking(1, true, nil))
	if r.relayStatus() != nil {
		t.Fatal("a runner with no relay state said something")
	}
}
