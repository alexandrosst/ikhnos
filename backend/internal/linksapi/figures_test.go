package linksapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/kernel"
)

func lr(cluster, peer, dir string, v float64) fusionapi.LinkReading {
	return fusionapi.LinkReading{Cluster: cluster, Peer: peer, Direction: dir, Value: v}
}

func TestPairsGetTheirFiguresFromWhicheverClusterMeasuredThem(t *testing.T) {
	links := []kernel.LinkSetting{{A: "cl-a", B: "cl-b"}, {A: "cl-a", B: "cl-c"}, {A: "cl-d", B: "cl-e"}}
	rd := fusionapi.LinkReadings{
		// a measured towards b; b measured towards a and says something else: a is believed
		Loss:       []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0.02), lr("cl-a", "cl-b", "reverse", 0), lr("cl-b", "cl-a", "forward", 0.5), lr("cl-b", "cl-a", "reverse", 0.5)},
		Jitter:     []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0.0004), lr("cl-b", "cl-a", "forward", 0.006)},
		RTT:        []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 0.012)},
		MTU:        []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 1500), lr("cl-b", "cl-a", "", 1420)},
		Throughput: []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 9.2e8), lr("cl-a", "cl-b", "reverse", 8.7e8)},
		Probed:     []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 1791200000), lr("cl-b", "cl-a", "reverse", 1791200100)},
		Tested:     []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 1791100000)},
		// the best of the week, whether the latest tests were all held to their cap, and the port the tests leave through
		ThroughputBest:  []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 9.5e8), lr("cl-a", "cl-b", "reverse", 9.1e8)},
		ThroughputFloor: []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0)},
		PortSpeed:       []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 1e9)},
		// c only reports from its side, where "forward" is c to a
	}
	rd.Loss = append(rd.Loss, lr("cl-c", "cl-a", "forward", 0.1), lr("cl-c", "cl-a", "reverse", 0.2))
	got := latestPairs(links, rd, time.Unix(1791200200, 0))
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	ab := got[0]
	f := func(p *float64) float64 {
		if p == nil {
			return -1
		}
		return *p
	}
	if f(ab.LossAToB) != 0.02 || f(ab.LossBToA) != 0 || f(ab.RTTSeconds) != 0.012 || f(ab.JitterSeconds) != 0.006 || f(ab.ThroughputAToB) != 9.2e8 || f(ab.ThroughputBToA) != 8.7e8 {
		t.Errorf("a-b = %+v", ab)
	}
	if f(ab.ThroughputBestAToB) != 9.5e8 || f(ab.ThroughputBestBToA) != 9.1e8 || f(ab.PortBitsPerSecond) != 1e9 || ab.ThroughputFloor {
		t.Errorf("best, port or floor of a-b: %+v", ab)
	}
	if ab.MTUBytes == nil || *ab.MTUBytes != 1420 {
		t.Errorf("the smaller MTU of the two clusters is the pair's: %v", ab.MTUBytes)
	}
	if ab.ProbedAt == nil || ab.ProbedAt.Unix() != 1791200100 || ab.TestedAt == nil || ab.TestedAt.Unix() != 1791100000 {
		t.Errorf("times: %v %v", ab.ProbedAt, ab.TestedAt)
	}
	ac := got[1]
	if f(ac.LossAToB) != 0.2 || f(ac.LossBToA) != 0.1 { // a to c is c's reverse; c to a is c's forward
		t.Errorf("a-c measured from c's side: %+v / %+v", f(ac.LossAToB), f(ac.LossBToA))
	}
	if got[1].ThroughputBestAToB != nil || got[1].PortBitsPerSecond != nil || got[1].ThroughputFloor {
		t.Errorf("a pair with no tests has no capacity: %+v", got[1])
	}
	de := got[2]
	if de.LossAToB != nil || de.RTTSeconds != nil || de.MTUBytes != nil || de.ProbedAt != nil || de.TestedAt != nil || de.A != "cl-d" {
		t.Errorf("a pair nobody reported on has no figures: %+v", de)
	}
}

func lrr(cluster, peer, dir, reason string, v float64) fusionapi.LinkReading {
	return fusionapi.LinkReading{Cluster: cluster, Peer: peer, Direction: dir, Reason: reason, Value: v}
}

// The store answers in no defined order, and a rolled pod can leave two series for one key. The figure shown must not depend on
// which comes last: the worse loss, the larger delay variation and the smaller MTU win.
func TestTwoSeriesOfOneKeyResolveTheSameWhicheverComesLast(t *testing.T) {
	links := []kernel.LinkSetting{{A: "cl-a", B: "cl-b"}}
	now := time.Unix(1791200200, 0)
	loss := []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 1), lr("cl-a", "cl-b", "forward", 0.1)}
	jit := []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0.02), lr("cl-a", "cl-b", "forward", 0.001)}
	mtu := []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 1500), lr("cl-a", "cl-b", "", 1420)}
	for _, rev := range []bool{false, true} {
		l, j, m := append([]fusionapi.LinkReading(nil), loss...), append([]fusionapi.LinkReading(nil), jit...), append([]fusionapi.LinkReading(nil), mtu...)
		if rev {
			l[0], l[1], j[0], j[1], m[0], m[1] = l[1], l[0], j[1], j[0], m[1], m[0]
		}
		got := latestPairs(links, fusionapi.LinkReadings{Loss: l, Jitter: j, MTU: m}, now)[0]
		if got.LossAToB == nil || *got.LossAToB != 1 || got.JitterSeconds == nil || *got.JitterSeconds != 0.02 || got.MTUBytes == nil || *got.MTUBytes != 1420 {
			t.Errorf("reversed=%v: loss %v jitter %v mtu %v", rev, got.LossAToB, got.JitterSeconds, got.MTUBytes)
		}
	}
}

func probeStateOf(p LinkPairLatest) (string, string, string, string) {
	f := func(s *LinkProbeLatest) string {
		if s == nil {
			return "-"
		}
		return string(s.State) + "/" + string(s.Reason)
	}
	d := func(s *LinkProbeLatest) string {
		if s == nil {
			return ""
		}
		return string(s.Diagnosis)
	}
	return f(p.ProbeAToB), f(p.ProbeBToA), d(p.ProbeAToB), d(p.ProbeBToA)
}

func TestProbeStateAndDiagnosisRules(t *testing.T) {
	now := time.Unix(1791200000, 0)
	fresh, old := float64(now.Unix()-60), float64(now.Unix()-3600)
	ab, ba := func(v float64) fusionapi.LinkReading { return lr("cl-a", "cl-b", "forward", v) }, func(v float64) fusionapi.LinkReading { return lr("cl-a", "cl-b", "reverse", v) }
	rttOK := []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 0.012)}
	connFail := func(why string) []fusionapi.LinkReading {
		return []fusionapi.LinkReading{lrr("cl-a", "cl-b", "", why, 4)}
	}
	for _, c := range []struct {
		name         string
		rd           fusionapi.LinkReadings
		noProbe      bool
		wantA, wantB string // state/reason of A to B and B to A
		diagA, diagB string
	}{
		{name: "nothing reported", wantA: "-", wantB: "-"},
		{name: "up in both directions, packets arrive", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(1)}, Loss: []fusionapi.LinkReading{ab(0.01), ba(0)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(fresh)}},
			wantA: "ok/", wantB: "ok/"},
		{name: "every packet lost although the run completed", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(1)}, Loss: []fusionapi.LinkReading{ab(1), ba(0)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(fresh)}},
			wantA: "ok/", wantB: "ok/", diagA: "udp-lost"},
		{name: "99 percent counts as lost, 98 does not", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(1)}, Loss: []fusionapi.LinkReading{ab(0.99), ba(0.98)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(fresh)}},
			wantA: "ok/", wantB: "ok/", diagA: "udp-lost"},
		{name: "up but no run completed for long", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(1)}, Loss: []fusionapi.LinkReading{ab(0), ba(0)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(old)}},
			wantA: "ok/", wantB: "ok/", diagB: "stale"},
		{name: "the field case: forward lossy, reverse refused by the responder",
			rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(0)}, Loss: []fusionapi.LinkReading{ab(1)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(old)}, RTT: rttOK,
				ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "reverse", "other", 9)}},
			wantA: "ok/", wantB: "failing/other", diagA: "udp-lost", diagB: "denied"},
		{name: "authentication refused, connections fine", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(0), ba(0)}, Probed: []fusionapi.LinkReading{ab(old), ba(old)}, RTT: rttOK,
			ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "auth", 2), lrr("cl-a", "cl-b", "reverse", "auth", 2)}},
			wantA: "failing/auth", wantB: "failing/auth", diagA: "denied", diagB: "denied"},
		{name: "nothing listens on the control port", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(0), ba(0)}, ConnectFailure: connFail("refused"),
			ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "refused", 5), lrr("cl-a", "cl-b", "reverse", "refused", 5)}},
			wantA: "never/refused", wantB: "never/refused", diagA: "no-responder", diagB: "no-responder"},
		{name: "the control port does not answer", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(0), ba(0)}, ConnectFailure: connFail("timeout"),
			ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "timeout", 5)}},
			wantA: "never/timeout", wantB: "never/", diagA: "filtered", diagB: "filtered"},
		{name: "connections failed lately but were also timed: the evidence is mixed", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(0), ba(0)}, RTT: rttOK, ConnectFailure: connFail("refused"),
			ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "other", 5)}},
			wantA: "never/other", wantB: "never/"},
		{name: "one direction down with nothing else known", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(0)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(fresh)}},
			wantA: "ok/", wantB: "failing/", diagB: "one-way"},
		{name: "pinned to a kind of address the peer lacks says nothing more", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(0), ba(0)}, ConnectFailure: connFail("timeout"),
			ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "no_such_kind", 5)}},
			wantA: "never/no_such_kind", wantB: "never/", diagB: "filtered"},
		{name: "reported from the second cluster's side", rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{lr("cl-b", "cl-a", "forward", 0), lr("cl-b", "cl-a", "reverse", 1)}, Probed: []fusionapi.LinkReading{lr("cl-b", "cl-a", "reverse", fresh)}},
			wantA: "ok/", wantB: "never/", diagB: "one-way"},
		{name: "probing off for the pair", noProbe: true, rd: fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(0), ba(0)}}, wantA: "-", wantB: "-"},
	} {
		got := latestPairs([]kernel.LinkSetting{{A: "cl-a", B: "cl-b", NoProbe: c.noProbe}}, c.rd, now)[0]
		a, b, da, db := probeStateOf(got)
		if a != c.wantA || b != c.wantB || da != c.diagA || db != c.diagB {
			t.Errorf("%s: states %s %s, diagnoses %q %q; want %s %s, %q %q", c.name, a, b, da, db, c.wantA, c.wantB, c.diagA, c.diagB)
		}
	}
	// the time of the last completed run is kept per direction
	got := latestPairs([]kernel.LinkSetting{{A: "cl-a", B: "cl-b"}}, fusionapi.LinkReadings{Up: []fusionapi.LinkReading{ab(1), ba(0)}, Probed: []fusionapi.LinkReading{ab(fresh), ba(old)}}, now)[0]
	if got.ProbeAToB.At == nil || got.ProbeAToB.At.Unix() != int64(fresh) || got.ProbeBToA.At == nil || got.ProbeBToA.At.Unix() != int64(old) || got.ProbeBToA.State != "failing" {
		t.Errorf("per-direction times: %+v %+v", got.ProbeAToB, got.ProbeBToA)
	}
}

// A reason label is whatever the cluster's metrics said, and an address in it is not for a viewer: only the known words go out.
func TestDiagnosisNeverCarriesAnAddressOrWhatTheStoreSaid(t *testing.T) {
	now := time.Unix(1791200000, 0)
	rd := fusionapi.LinkReadings{
		Up:           []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0), lr("cl-a", "cl-b", "reverse", 0)},
		RTT:          []fusionapi.LinkReading{lr("cl-a", "cl-b", "", 0.01)},
		ProbeFailure: []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "dial tcp 10.2.3.4:8861: i/o timeout", 3), lrr("cl-a", "cl-b", "reverse", "auth", 1)},
	}
	got := latestPairs([]kernel.LinkSetting{{A: "cl-a", B: "cl-b"}}, rd, now)[0]
	j, _ := json.Marshal(got)
	if strings.Contains(string(j), "10.2.3.4") || strings.Contains(string(j), "dial") {
		t.Fatalf("the store's text reached the answer: %s", j)
	}
	if got.ProbeAToB.Reason != "other" || got.ProbeAToB.Diagnosis != "denied" || got.ProbeBToA.Reason != "auth" {
		t.Errorf("%+v %+v", got.ProbeAToB, got.ProbeBToA)
	}
	known := map[string]bool{"": true}
	for _, w := range ProbeDiagnosis("").WireWords() {
		known[w] = true
	}
	for _, s := range []*LinkProbeLatest{got.ProbeAToB, got.ProbeBToA} {
		if !known[string(s.Diagnosis)] {
			t.Errorf("a diagnosis outside the bounded words: %q", s.Diagnosis)
		}
	}
	// on two reasons of equal count the same one wins in either order
	two := []fusionapi.LinkReading{lrr("cl-a", "cl-b", "forward", "timeout", 2), lrr("cl-a", "cl-b", "forward", "refused", 2)}
	one := linkReasons(two)[[3]string{"cl-a", "cl-b", "forward"}]
	if rev := linkReasons([]fusionapi.LinkReading{two[1], two[0]})[[3]string{"cl-a", "cl-b", "forward"}]; one != rev || one != "refused" {
		t.Errorf("tie: %q / %q", one, rev)
	}
}

func readOf(r fusionapi.LinkReadings, err error, calls *atomic.Int32) func(context.Context) (fusionapi.LinkReadings, error) {
	return func(ctx context.Context) (fusionapi.LinkReadings, error) {
		calls.Add(1)
		return r, err
	}
}

func TestACancelledFirstCallerStillCachesTheSharedRead(t *testing.T) {
	var c readingsCache
	t0 := time.Unix(1791200000, 0)
	now := func() time.Time { return t0 }
	var reads atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	good := fusionapi.LinkReadings{Loss: []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0.5)}}
	read := func(rc context.Context) (fusionapi.LinkReadings, error) {
		reads.Add(1)
		cancel() // the first caller's request ends while FUSION is being read
		if rc.Err() != nil {
			return fusionapi.LinkReadings{}, rc.Err()
		}
		if _, ok := rc.Deadline(); !ok {
			t.Error("the shared read has no deadline of its own")
		}
		return good, nil
	}
	if _, _, err := c.get(ctx, now, read); err != nil {
		t.Fatalf("the read took the caller's cancellation: %v", err)
	}
	r, s, err := c.get(context.Background(), now, readOf(fusionapi.LinkReadings{}, errors.New("must not be asked"), &reads))
	if err != nil || len(r.Loss) != 1 || reads.Load() != 1 || s.Stale {
		t.Fatalf("the second caller did not get the cached read: %v %v reads=%d", r, err, reads.Load())
	}
}

func TestACallerWaitingForTheSharedReadStopsWaitingWithItsRequest(t *testing.T) {
	var c readingsCache
	now := func() time.Time { return time.Unix(1791200000, 0) }
	release, started := make(chan struct{}), make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.get(context.Background(), now, func(context.Context) (fusionapi.LinkReadings, error) {
			close(started)
			<-release
			return fusionapi.LinkReadings{}, nil
		})
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	t0 := time.Now()
	if _, _, err := c.get(ctx, now, nil); !errors.Is(err, context.DeadlineExceeded) || time.Since(t0) > 2*time.Second {
		t.Fatalf("a waiter was held past its own deadline: %v after %v", err, time.Since(t0))
	}
	close(release)
	<-done
}

func TestAFailedReadServesTheLastGoodOneMarkedStaleAndThenGivesUp(t *testing.T) {
	var c readingsCache
	t0 := time.Unix(1791200000, 0)
	now := t0
	clock := func() time.Time { return now }
	var reads atomic.Int32
	good := fusionapi.LinkReadings{Loss: []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 0.5)}}
	down := errors.New("store down")
	if _, s, err := c.get(context.Background(), clock, readOf(good, nil, &reads)); err != nil || s.Stale || !s.At.Equal(t0) {
		t.Fatalf("first read: %v %+v", err, s)
	}
	// the next read fails: the last good one is served, with the time it was read and the stale flag
	now = t0.Add(LatestEvery + time.Second)
	r, s, err := c.get(context.Background(), clock, readOf(fusionapi.LinkReadings{}, down, &reads))
	if err != nil || len(r.Loss) != 1 || !s.Stale || !s.At.Equal(t0) || reads.Load() != 2 {
		t.Fatalf("after a failed read: %v %+v reads=%d", err, s, reads.Load())
	}
	// the failure is kept for the window, like a success: nobody asks the store again
	now = now.Add(5 * time.Second)
	if _, s, err := c.get(context.Background(), clock, readOf(good, nil, &reads)); err != nil || !s.Stale || reads.Load() != 2 {
		t.Fatalf("inside the window: %v %+v reads=%d", err, s, reads.Load())
	}
	// FUSION is back: fresh again
	now = now.Add(LatestEvery)
	if _, s, err := c.get(context.Background(), clock, readOf(good, nil, &reads)); err != nil || s.Stale || !s.At.Equal(now) {
		t.Fatalf("recovered: %v %+v", err, s)
	}
	// a store that stays down is given up on after the bound: the figures would pass for current ones
	good2 := now
	now = good2.Add(LatestEvery + time.Second)
	if _, s, err := c.get(context.Background(), clock, readOf(fusionapi.LinkReadings{}, down, &reads)); err != nil || !s.Stale {
		t.Fatalf("still within the bound: %v %+v", err, s)
	}
	now = good2.Add(LatestStaleFor + time.Second)
	if r, s, err := c.get(context.Background(), clock, readOf(fusionapi.LinkReadings{}, down, &reads)); !errors.Is(err, down) || s.Stale || len(r.Loss) != 0 {
		t.Fatalf("past the bound: %v %+v %v", err, s, r)
	}
	// and with no good read ever, the failure is what is served
	var fresh readingsCache
	if _, _, err := fresh.get(context.Background(), clock, readOf(fusionapi.LinkReadings{}, down, &reads)); !errors.Is(err, down) {
		t.Fatalf("no good read: %v", err)
	}
}

// A pair is a floor when the latest tests of any of its directions were all held to their cap: the figure then says "at least".
func TestAPairWhoseLatestTestsAreHeldToTheirCapIsAFloor(t *testing.T) {
	links := []kernel.LinkSetting{{A: "cl-a", B: "cl-b"}}
	rd := fusionapi.LinkReadings{
		ThroughputBest:  []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 2.4e7)},
		ThroughputFloor: []fusionapi.LinkReading{lr("cl-a", "cl-b", "forward", 1), lr("cl-a", "cl-b", "reverse", 0)},
	}
	got := latestPairs(links, rd, time.Unix(1791200200, 0))
	if len(got) != 1 || !got[0].ThroughputFloor {
		t.Errorf("%+v", got)
	}
}

func kindUp(cluster, peer, kind string, v float64) fusionapi.LinkReading {
	return fusionapi.LinkReading{Cluster: cluster, Peer: peer, Kind: kind, Value: v}
}

// A pair reached over one kind of address and not over another is told apart from one that is not reached at all.
func TestTheKindsOfAddressThatDidNotAnswerAreNamed(t *testing.T) {
	now := time.Unix(1791200000, 0)
	pairs := []kernel.LinkSetting{{A: "cl-a", B: "cl-b"}}
	silent := func(in ...fusionapi.LinkReading) string {
		var out []string
		for _, k := range latestPairs(pairs, fusionapi.LinkReadings{KindUp: in}, now)[0].Silent {
			out = append(out, string(k))
		}
		return strings.Join(out, ",")
	}
	if got := silent(kindUp("cl-a", "cl-b", "mesh", 1), kindUp("cl-a", "cl-b", "public", 0)); got != "public" {
		t.Errorf("mesh answers, public does not: %q", got)
	}
	if got := silent(kindUp("cl-a", "cl-b", "public", 0), kindUp("cl-a", "cl-b", "private", 0), kindUp("cl-a", "cl-b", "mesh", 1)); got != "private,public" {
		t.Errorf("two kinds are silent, in the order of the words: %q", got)
	}
	if got := silent(kindUp("cl-a", "cl-b", "mesh", 1), kindUp("cl-a", "cl-b", "public", 1)); got != "" {
		t.Errorf("every kind answers: %q", got)
	}
	if got := silent(kindUp("cl-a", "cl-b", "mesh", 0), kindUp("cl-a", "cl-b", "public", 0)); got != "" {
		t.Errorf("none answers, which says nothing of one path against another: %q", got)
	}
	if got := silent(kindUp("cl-a", "cl-b", "public", 0)); got != "" {
		t.Errorf("a single kind that was tried and failed: %q", got)
	}
	// either cluster may have measured it, and one that reached a kind makes it answer.
	if got := silent(kindUp("cl-b", "cl-a", "mesh", 1), kindUp("cl-b", "cl-a", "public", 0)); got != "public" {
		t.Errorf("from the second cluster's side: %q", got)
	}
	if got := silent(kindUp("cl-a", "cl-b", "mesh", 1), kindUp("cl-a", "cl-b", "public", 0), kindUp("cl-b", "cl-a", "public", 1)); got != "" {
		t.Errorf("reached from the other side: %q", got)
	}
	// another pair's readings, and words that are not kinds (the label is whatever a cluster's metrics said), are not used.
	if got := silent(kindUp("cl-a", "cl-c", "mesh", 1), kindUp("cl-a", "cl-c", "public", 0), kindUp("cl-a", "cl-b", "mesh", 1), kindUp("cl-a", "cl-b", "10.2.3.4", 0)); got != "" {
		t.Errorf("%q", got)
	}
	j, _ := json.Marshal(latestPairs(pairs, fusionapi.LinkReadings{KindUp: []fusionapi.LinkReading{kindUp("cl-a", "cl-b", "mesh", 1), kindUp("cl-a", "cl-b", "10.2.3.4", 0)}}, now)[0])
	if strings.Contains(string(j), "10.2.3.4") {
		t.Errorf("an address reached the answer: %s", j)
	}
	// a pair that is not probed carries none.
	if got := latestPairs([]kernel.LinkSetting{{A: "cl-a", B: "cl-b", NoProbe: true}}, fusionapi.LinkReadings{KindUp: []fusionapi.LinkReading{kindUp("cl-a", "cl-b", "mesh", 1), kindUp("cl-a", "cl-b", "public", 0)}}, now)[0]; len(got.Silent) != 0 {
		t.Errorf("a pair with probing off: %v", got.Silent)
	}
}

func tr(cluster, peer, reason string, n float64) fusionapi.LinkReading {
	return fusionapi.LinkReading{Cluster: cluster, Peer: peer, Reason: reason, Value: n}
}

// A pair with throughput tests and no recent result is told why: what held its tests back or made them fail, or that none has been tried.
func TestWhyAPairsTestsHaveNotRunIsSaid(t *testing.T) {
	now := time.Unix(1791200000, 0)
	recent, old := float64(now.Unix()-3600), float64(now.Unix()-8*3600)
	with := []kernel.LinkSetting{{A: "cl-a", B: "cl-b", Throughput: &kernel.LinkThroughput{}}}
	tested := func(at float64) []fusionapi.LinkReading { return []fusionapi.LinkReading{lr("cl-a", "cl-b", "", at)} }
	for _, c := range []struct {
		name   string
		links  []kernel.LinkSetting
		rd     fusionapi.LinkReadings
		held   TestsHeld
		reason ProbeFailure
	}{
		{name: "nothing tried yet", links: with, held: heldWaiting},
		{name: "a result within the window says nothing", links: with, rd: fusionapi.LinkReadings{Tested: tested(recent), TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-b", "budget", 3)}}},
		{name: "held back by real traffic", links: with, rd: fusionapi.LinkReadings{TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-b", "link_busy", 5)}}, held: heldLinkBusy},
		{name: "held back by the daily amount, with an old result", links: with, rd: fusionapi.LinkReadings{Tested: tested(old), TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-b", "budget", 2)}}, held: heldBudget},
		{name: "the other cluster's server is busy, reported from the second cluster", links: with, rd: fusionapi.LinkReadings{TestsSkipped: []fusionapi.LinkReading{tr("cl-b", "cl-a", "server_busy", 1)}}, held: heldServerBusy},
		{name: "failures come before holds", links: with, rd: fusionapi.LinkReadings{TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-b", "budget", 9)}, TestsFailed: []fusionapi.LinkReading{tr("cl-a", "cl-b", "auth", 1)}}, held: heldFailing, reason: failAuth},
		{name: "a reason that is not a known word reads as other", links: with, rd: fusionapi.LinkReadings{TestsFailed: []fusionapi.LinkReading{tr("cl-a", "cl-b", "dial tcp 10.2.3.4:5201: i/o timeout", 2)}}, held: heldFailing, reason: failOther},
		{name: "a hold that is not a known word says nothing", links: with, rd: fusionapi.LinkReadings{Tested: tested(old), TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-b", "10.2.3.4", 2)}}},
		{name: "an old result and nothing counted: a plan that runs once a day is not late", links: with, rd: fusionapi.LinkReadings{Tested: tested(old)}},
		{name: "another pair's counts are not used", links: with, rd: fusionapi.LinkReadings{TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-c", "budget", 9)}}, held: heldWaiting},
		{name: "a pair with no tests says nothing", links: []kernel.LinkSetting{{A: "cl-a", B: "cl-b"}}, rd: fusionapi.LinkReadings{TestsSkipped: []fusionapi.LinkReading{tr("cl-a", "cl-b", "budget", 9)}}},
	} {
		got := latestPairs(c.links, c.rd, now)[0]
		if got.TestsHeld != c.held || got.TestsReason != c.reason {
			t.Errorf("%s: held %q reason %q, want %q %q", c.name, got.TestsHeld, got.TestsReason, c.held, c.reason)
		}
	}
	j, _ := json.Marshal(latestPairs(with, fusionapi.LinkReadings{TestsFailed: []fusionapi.LinkReading{tr("cl-a", "cl-b", "dial tcp 10.2.3.4:5201: i/o timeout", 2)}}, now)[0])
	if strings.Contains(string(j), "10.2.3.4") {
		t.Errorf("the store's text reached the answer: %s", j)
	}
}
