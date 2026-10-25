package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/relay"
)

func wlRunner() *runner {
	return &runner{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

var t0 = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func find(rep []*ikhnosv1.WorkloadStatus, name string) *ikhnosv1.WorkloadStatus {
	for _, w := range rep {
		if w.Name == name {
			return w
		}
	}
	return nil
}

func TestAWorkloadWithPodsMissingIsCountedFromWhenItWasFirstSeenSoAndForgottenWhenComplete(t *testing.T) {
	r := wlRunner()
	kepler := relay.Workload{Name: "ikhnos-telemetry-kepler", Kind: "DaemonSet", Component: "telemetry-kepler", Wanted: 3, Ready: 2}
	host := relay.Workload{Name: "ikhnos-telemetry-host", Kind: "DaemonSet", Component: "telemetry-host", Wanted: 3, Ready: 3}

	r.noteWorkloads([]relay.Workload{host, kepler}, t0)
	rep := r.workloadReport(t0)
	if len(rep) != 2 || rep[0].Name != "ikhnos-telemetry-host" {
		t.Fatalf("report %v (a stable order, by name)", rep)
	}
	if k := find(rep, "ikhnos-telemetry-kepler"); k.Wanted != 3 || k.Ready != 2 || k.NotReadySeconds != 0 || k.Component != "telemetry-kepler" || k.Kind != "DaemonSet" {
		t.Fatalf("kepler %v", k)
	}

	// Seen again later with the pod still missing: counted from the first time, not from each reading.
	r.noteWorkloads([]relay.Workload{host, kepler}, t0.Add(5*time.Minute))
	if k := find(r.workloadReport(t0.Add(5*time.Minute)), "ikhnos-telemetry-kepler"); k.NotReadySeconds != 300 {
		t.Fatalf("kepler %v", k)
	}
	if h := find(r.workloadReport(t0.Add(5*time.Minute)), "ikhnos-telemetry-host"); h.NotReadySeconds != 0 {
		t.Fatalf("host %v", h)
	}

	// Complete, then short again: a new count.
	kepler.Ready = 3
	r.noteWorkloads([]relay.Workload{host, kepler}, t0.Add(6*time.Minute))
	kepler.Ready = 2
	r.noteWorkloads([]relay.Workload{host, kepler}, t0.Add(7*time.Minute))
	if k := find(r.workloadReport(t0.Add(8*time.Minute)), "ikhnos-telemetry-kepler"); k.NotReadySeconds != 60 {
		t.Fatalf("kepler %v", k)
	}
}

func TestWhatTheStatusSaysIsBoundedAndWhatCannotBeReadIsLetGoOfAfterAFewTries(t *testing.T) {
	r := wlRunner()
	var many []relay.Workload
	for i := 0; i < 40; i++ {
		many = append(many, relay.Workload{Name: string(rune('a'+i%26)) + string(rune('a'+i/26)), Kind: "Deployment", Wanted: 1, Ready: 1})
	}
	r.noteWorkloads(many, t0)
	if n := len(r.workloadReport(t0)); n != maxWorkloadsReported {
		t.Fatalf("%d reported", n)
	}
	r.workloadsFailed(errors.New("forbidden"))
	r.workloadsFailed(errors.New("forbidden"))
	if len(r.workloadReport(t0)) == 0 {
		t.Fatal("two failed readings are not enough to forget what was known")
	}
	r.workloadsFailed(errors.New("forbidden"))
	if rep := r.workloadReport(t0); rep != nil {
		t.Fatalf("still said after three failed readings: %v", rep)
	}
	// A good reading after that starts again.
	r.noteWorkloads(many[:2], t0)
	if n := len(r.workloadReport(t0)); n != 2 {
		t.Fatalf("%d reported", n)
	}
}

func TestSecondsMissingDoNotMakeAMessageButBeingHeldBackDoes(t *testing.T) {
	r := wlRunner()
	sig := func(now time.Time) string {
		d := &ikhnosv1.Diagnostics{Workloads: r.workloadReport(now)}
		return string(diagSignature(d))
	}
	w := relay.Workload{Name: "ikhnos-telemetry-kepler", Kind: "DaemonSet", Wanted: 3, Ready: 2}
	r.noteWorkloads([]relay.Workload{w}, t0)
	a := sig(t0)
	if b := sig(t0.Add(30 * time.Second)); b != a {
		t.Fatal("the seconds a pod has been missing changed the message")
	}
	if c := sig(t0.Add(WorkloadsHeldBackAfter + time.Second)); c == a {
		t.Fatal("a workload held back did not change the message")
	}
	d := sig(t0.Add(WorkloadsHeldBackAfter + time.Second))
	if e := sig(t0.Add(time.Hour)); e != d {
		t.Fatal("the hours it has been held back changed the message")
	}
}

type wlStore struct {
	fakeRelay
	mu2 sync.Mutex
	ws  []relay.Workload
	err error
}

func (s *wlStore) Workloads(context.Context) ([]relay.Workload, error) {
	s.mu2.Lock()
	defer s.mu2.Unlock()
	return s.ws, s.err
}

func TestTheLoopReadsTheStoreAndStopsWithTheAgent(t *testing.T) {
	r := wlRunner()
	s := &wlStore{ws: []relay.Workload{{Name: "ikhnos-agent", Kind: "Deployment", Wanted: 1, Ready: 1}}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.workloadsLoop(ctx, s, 10*time.Millisecond); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for len(r.workloadReport(time.Now())) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("nothing read")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not stop")
	}
}
