package landscape

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"

	"ikhnos/internal/store"
	"ikhnos/internal/testkit"
)

// The Run goroutine's ticker calls rec.tick (-> scan) while POST /history/record runs Hub.ScanNow (-> scan)
// on an HTTP goroutine. Both write the recorder's bookkeeping, so they must not overlap; run with -race.
func TestRecorderTickAndScanNowDoNotRace(t *testing.T) {
	r := newHubRig(t)
	id, _, _ := r.approvedAgent(t, testkit.FP)
	r.hub.Mu.Lock()
	r.hub.Views[id] = liveView(*r.now, "n1", "n2")
	r.hub.Mu.Unlock()

	var clock atomic.Int64 // race-free test clock so only the code under test races
	base := r.now.UnixNano()
	r.hub.C.Now = func() time.Time { return time.Unix(0, base+clock.Add(int64(11*time.Second))) }

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { // the HTTP handler goroutine(s)
		defer wg.Done()
		for i := 0; i < 60; i++ {
			r.hub.Mu.Lock()
			st := r.hub.Views[id].State
			if i%2 == 0 {
				delete(st.Nodes, "n2")
			} else {
				st.Nodes["n2"] = &ikhnosv1.NodeFacts{Key: "n2", Name: "n2", Ready: true}
			}
			r.hub.Mu.Unlock()
			r.hub.ScanNow(r.ctx)
		}
	}()
	go func() { // the Run ticker goroutine
		defer wg.Done()
		for i := 0; i < 60; i++ {
			r.hub.rec.changed.Store(true)
			r.hub.rec.tick(r.ctx)
		}
	}()
	wg.Wait()
}

// With every link silent nothing marks the picture changed, so the recorder would look only at its next periodic scan, up to SnapshotMinutes
// after a link went quiet. It looks once, just after the quiet window has passed since the last traffic, and not again until there is more.
func TestRecorderLooksOnceWhenTrafficHasBeenQuietForTheWholeWindow(t *testing.T) {
	r := newHubRig(t)
	rec := r.hub.rec
	quiet := r.hub.StaleFlows()
	t0 := *r.now

	if rec.settleDue(t0.Add(10 * quiet)) {
		t.Fatal("no traffic was ever reported: there is nothing to settle")
	}
	rec.noteFlow(t0)
	if rec.settleDue(t0.Add(quiet)) {
		t.Fatal("the last link is not quiet yet at the moment the window ends: it is quiet after it")
	}
	if !rec.settleDue(t0.Add(quiet + 2*time.Second)) {
		t.Fatal("a scan must follow the window, or the quiet link waits for the periodic one")
	}
	rec.settled = rec.lastFlow.Load() // what tick does when it scans
	if rec.settleDue(t0.Add(3 * quiet)) {
		t.Fatal("one look per stretch of silence")
	}
	rec.noteFlow(t0.Add(4 * quiet))
	if !rec.settleDue(t0.Add(5*quiet + 2*time.Second)) {
		t.Fatal("traffic again, then quiet again: another look")
	}
}

// Traffic stops, and nothing else changes: the history says the link went quiet once the quiet window has passed, not at some later periodic scan.
func TestQuietLinkReachesTheHistoryWhenItsWindowEndsEvenWithNothingElseChanging(t *testing.T) {
	r := newHubRig(t)
	id, _, _ := r.approvedAgent(t, testkit.FP)
	if err := r.st.SetAccessTier(r.ctx, "org-1", id, 2); err != nil { // traffic is drawn from tier 2
		t.Fatal(err)
	}
	r.hub.FlowStaleAfter = 60 * time.Second
	v := liveView(*r.now, "n1")
	v.State.Workloads["ns/Deployment/a"] = testkit.Workload("ns", "Deployment", "a")
	v.State.Workloads["ns/Deployment/b"] = testkit.Workload("ns", "Deployment", "b")
	r.hub.Mu.Lock()
	r.hub.Views[id] = v
	r.hub.Mu.Unlock()
	r.hub.ScanNow(r.ctx)                // the first look only sets the baseline
	*r.now = r.now.Add(5 * time.Second) // past the picture's own short cache
	r.hub.Mu.Lock()
	v.Flows.Apply(&ikhnosv1.FlowBatch{WindowSeconds: 10, Flows: []*ikhnosv1.Flow{testkit.FlowOf(testkit.WorkloadEndpoint("ns/Deployment/a"), testkit.WorkloadEndpoint("ns/Deployment/b"), 80, 3)}}, *r.now)
	r.hub.Mu.Unlock()
	r.hub.rec.noteFlow(*r.now)
	r.hub.ScanNow(r.ctx)

	kinds := func() string {
		evs, _ := r.st.ListEvents(r.ctx, "org-1", store.EventQuery{Since: r.now.Add(-time.Hour)})
		var out []string
		for _, e := range evs {
			out = append(out, e.Kind)
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	if got := kinds(); !strings.Contains(got, "dependency-seen") {
		t.Fatalf("the link was not recorded as seen at all, so this test would prove nothing: %q", got)
	}
	r.hub.rec.changed.Store(false)

	beat := func() {
		r.hub.Mu.Lock()
		r.hub.Views[id].LastBeat, r.hub.Views[id].LastSync = *r.now, *r.now // the agent is alive; only the traffic stopped
		r.hub.Mu.Unlock()
	}
	*r.now = r.now.Add(30 * time.Second)
	beat()
	r.hub.rec.tick(r.ctx)
	if strings.Contains(kinds(), "dependency-quiet") {
		t.Fatal("30 s into a 60 s window the link is not quiet yet")
	}
	*r.now = r.now.Add(35 * time.Second)
	beat()
	r.hub.rec.tick(r.ctx)
	if !strings.Contains(kinds(), "dependency-quiet") {
		t.Fatalf("65 s after the last traffic, the window over, nothing says it went quiet: %q", kinds())
	}
}
