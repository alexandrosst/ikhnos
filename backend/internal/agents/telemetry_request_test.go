package agents

import (
	"testing"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

func pct(n int) *int { return &n }

func TestTurningSomethingOnOrUpWidensAndTurningItOffOrDownDoesNot(t *testing.T) {
	traces := &TelemetrySignals{Traces: true}
	both := &TelemetrySignals{Traces: true, ResourceUsage: true}
	for _, tc := range []struct {
		name      string
		cur, next TelemetryRequest
		widens    bool
	}{
		{"the first request to turn a signal on", TelemetryRequest{}, TelemetryRequest{Signals: traces}, true},
		{"one more signal", TelemetryRequest{Signals: traces}, TelemetryRequest{Signals: both}, true},
		{"one signal fewer", TelemetryRequest{Signals: both}, TelemetryRequest{Signals: traces}, false},
		{"every signal off", TelemetryRequest{Signals: both}, TelemetryRequest{Signals: &TelemetrySignals{}}, false},
		{"the same signals", TelemetryRequest{Signals: both}, TelemetryRequest{Signals: both}, false},
		{"keeping less of the traces than the all there is by default", TelemetryRequest{}, TelemetryRequest{TracesSamplingPercent: pct(10)}, false},
		{"keeping more of the traces", TelemetryRequest{TracesSamplingPercent: pct(10)}, TelemetryRequest{TracesSamplingPercent: pct(50)}, true},
		{"keeping less of the traces", TelemetryRequest{TracesSamplingPercent: pct(50)}, TelemetryRequest{TracesSamplingPercent: pct(10)}, false},
		{"taking the request back", TelemetryRequest{Signals: both, TracesSamplingPercent: pct(50)}, TelemetryRequest{}, false},
	} {
		if got := TelemetryWidens(tc.cur, tc.next); got != tc.widens {
			t.Errorf("%s: widens %v, want %v", tc.name, got, tc.widens)
		}
	}
}

func TestACleanedRequestKeepsTheShareAndTheSignalsAndRefusesWhatCannotBe(t *testing.T) {
	if _, err := CleanTelemetryRequest(TelemetryRequest{TracesSamplingPercent: pct(0)}); err == nil {
		t.Error("a share of 0 was accepted")
	}
	if _, err := CleanTelemetryRequest(TelemetryRequest{TracesSamplingPercent: pct(101)}); err == nil {
		t.Error("a share of 101 was accepted")
	}
	if _, err := CleanTelemetryRequest(TelemetryRequest{Signals: &TelemetrySignals{ResourceUsage: true}, TracesSamplingPercent: pct(5)}); err == nil {
		t.Error("a share of traces with the traces off was accepted")
	}
	got, err := CleanTelemetryRequest(TelemetryRequest{Signals: &TelemetrySignals{Traces: true}, TracesSamplingPercent: pct(5)})
	if err != nil || !got.Signals.Traces || *got.TracesSamplingPercent != 5 {
		t.Errorf("%+v %v", got, err)
	}
	// The result does not alias what was given: a caller changing its copy cannot change what is kept.
	in := TelemetryRequest{Signals: &TelemetrySignals{Traces: true}}
	out, _ := CleanTelemetryRequest(in)
	in.Signals.Traces = false
	if !out.Signals.Traces {
		t.Error("the cleaned request shares memory with the one it came from")
	}
}

func TestAWordTheServerDoesNotKnowIsNeverShownAsSuccess(t *testing.T) {
	for state, want := range map[string]string{"written": "written", "applied": "applied", "refused": "refused", "reverted": "reverted", "": "waiting", "thriving": "waiting", "APPLIED": "waiting"} {
		got := CleanRelayState(&ikhnosv1.TelemetryRelayStatus{Revision: 1, State: state})
		if got.State != want {
			t.Errorf("%q shown as %q, want %q", state, got.State, want)
		}
	}
	if CleanRelayState(nil) != nil {
		t.Error("nothing said became something")
	}
}

func TestTheDocumentOnlyCallsARevisionAppliedWhenTheClusterSaidSoAboutThatRevision(t *testing.T) {
	req := TelemetryRequest{Revision: 4, TracesSamplingPercent: pct(10)}
	for _, tc := range []struct {
		name  string
		relay *TelemetryRelay
		want  string
	}{
		{"nothing said", nil, TelemetryWaiting},
		{"an older revision applied", &TelemetryRelay{Revision: 3, State: TelemetryApplied}, TelemetryWaiting},
		{"this revision refused", &TelemetryRelay{Revision: 4, State: TelemetryRefused, Reason: "no"}, TelemetryRefused},
		{"this revision applied", &TelemetryRelay{Revision: 4, State: TelemetryApplied}, TelemetryApplied},
	} {
		if got := TelemetryRequestDocOf(req, tc.relay, "2026-10-25T00:00:00Z"); got.State != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got.State, tc.want)
		}
	}
}
