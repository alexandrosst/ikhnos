package agent

import (
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// Export health changes a message when a route changes state (or the collectors become unreadable), not on
// every reading: the counters and times behind a state move all the time.
func TestDiagSignatureIgnoresExportCountersButNotStates(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	mk := func(sent uint64, state ikhnosv1.ExportRouteHealth_State, reached uint32, when time.Time) *ikhnosv1.Diagnostics {
		return &ikhnosv1.Diagnostics{ExportHealth: &ikhnosv1.ExportHealth{
			ScrapedAt: timestamppb.New(when), PodsReached: reached,
			Routes: []*ikhnosv1.ExportRouteHealth{{Exporter: "otlp", Signal: "logs", State: state, Sent: sent, LastSentAt: timestamppb.New(when)}},
		}}
	}
	base := string(diagSignature(mk(10, ikhnosv1.ExportRouteHealth_EXPORTING, 2, at)))
	if got := string(diagSignature(mk(99, ikhnosv1.ExportRouteHealth_EXPORTING, 2, at.Add(time.Minute)))); got != base {
		t.Error("counters and times moving alone must not make a new message")
	}
	if got := string(diagSignature(mk(10, ikhnosv1.ExportRouteHealth_FAILING, 2, at))); got == base {
		t.Error("a route changing state must make a new message")
	}
	if got := string(diagSignature(mk(10, ikhnosv1.ExportRouteHealth_EXPORTING, 0, at))); got == base {
		t.Error("the collectors becoming unreadable must make a new message")
	}
	d := mk(10, ikhnosv1.ExportRouteHealth_EXPORTING, 2, at)
	_ = diagSignature(d)
	if d.ExportHealth.Routes[0].Sent != 10 || d.ExportHealth.ScrapedAt == nil {
		t.Error("diagSignature must not change the message it describes")
	}
}

// The receivers' and sources' counters move with every reading that has data, as the routes' do, and are no more a reason for a message. A
// queue that moves between readings is not one either; a queue that is empty one time and not the next, a first dropped item, and a collector
// restart are.
func TestDiagSignatureIgnoresEveryCounterButNotWhatTheyMean(t *testing.T) {
	at := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	mk := func(mod func(h *ikhnosv1.ExportHealth)) string {
		h := &ikhnosv1.ExportHealth{
			ScrapedAt: timestamppb.New(at), PodsReached: 2,
			Routes:    []*ikhnosv1.ExportRouteHealth{{Exporter: "otlp", Signal: "logs", State: ikhnosv1.ExportRouteHealth_EXPORTING, Sent: 10, Queued: 5, QueueCapacity: 1000, LastSentAt: timestamppb.New(at)}},
			Receivers: []*ikhnosv1.ReceiverHealth{{Signal: "logs", State: ikhnosv1.ReceiverHealth_RECEIVING, Accepted: 100, LastAcceptedAt: timestamppb.New(at)}},
			Sources:   []*ikhnosv1.SourceHealth{{Receiver: "hostmetrics", State: ikhnosv1.SourceHealth_RECEIVING, Accepted: 50, LastAcceptedAt: timestamppb.New(at)}},
		}
		if mod != nil {
			mod(h)
		}
		return string(diagSignature(&ikhnosv1.Diagnostics{ExportHealth: h}))
	}
	base := mk(nil)
	same := map[string]func(h *ikhnosv1.ExportHealth){
		"a receiver's counters and time": func(h *ikhnosv1.ExportHealth) {
			h.Receivers[0].Accepted, h.Receivers[0].LastAcceptedAt = 9999, timestamppb.New(at.Add(time.Minute))
		},
		"a source's counters and time": func(h *ikhnosv1.ExportHealth) {
			h.Sources[0].Accepted, h.Sources[0].Failed, h.Sources[0].LastFailedAt = 9999, 3, timestamppb.New(at.Add(time.Minute))
		},
		"a queue that moved":           func(h *ikhnosv1.ExportHealth) { h.Routes[0].Queued, h.Routes[0].QueueCapacity = 700, 2000 },
		"the time of the last restart": func(h *ikhnosv1.ExportHealth) { h.LastCollectorRestartAt = timestamppb.New(at) },
	}
	for name, mod := range same {
		if mk(mod) != base {
			t.Errorf("%s alone must not make a new message", name)
		}
	}
	differs := map[string]func(h *ikhnosv1.ExportHealth){
		"a queue that emptied":      func(h *ikhnosv1.ExportHealth) { h.Routes[0].Queued = 0 },
		"a first dropped item":      func(h *ikhnosv1.ExportHealth) { h.Routes[0].Dropped = 1 },
		"a collector restart":       func(h *ikhnosv1.ExportHealth) { h.CollectorRestarts = 1 },
		"a receiver changing state": func(h *ikhnosv1.ExportHealth) { h.Receivers[0].State = ikhnosv1.ReceiverHealth_REFUSING },
		"a source changing state":   func(h *ikhnosv1.ExportHealth) { h.Sources[0].State = ikhnosv1.SourceHealth_FAILING },
	}
	for name, mod := range differs {
		if mk(mod) == base {
			t.Errorf("%s must make a new message", name)
		}
	}
}
