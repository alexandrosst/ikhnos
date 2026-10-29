package link

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

// An automatic peer is one that is given only a daily allowance: the tests run at full speed, as often as the allowance pays
// for.

func automatic(c *Capacity, allowance uint64) *ikhnosv1.LinkPeer {
	lp := peer("cl-a", "A", "10.0.0.1")
	lp.Throughput, lp.ThroughputBudgetBytes = true, allowance
	c.Meter.Peers.Set("cl-me", []*ikhnosv1.LinkPeer{lp})
	return lp
}

func pairUsed(t *testing.T, c *Capacity) uint64 {
	t.Helper()
	v, err := strconv.ParseUint(render(c.Meter)[`ikhnos_link_throughput_pair_budget_used_bytes{cluster="cl-me",peer="cl-a",method="iperf3"}`], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func skipped(c *Capacity, reason string) string {
	return render(c.Meter)[fmt.Sprintf(`ikhnos_link_throughput_tests_skipped_total{cluster="cl-me",peer="cl-a",method="iperf3",reason="%s"}`, reason)]
}

// durationOf is the -t of a call.
func durationOf(call string) int {
	f := strings.Fields(call)
	for i, x := range f {
		if x == "-t" && i+1 < len(f) {
			n, _ := strconv.Atoi(f[i+1])
			return n
		}
	}
	return 0
}

// The runs of f other than the one-second ones that find out what a test carries.
func realRuns(f *fakeIperf) []string {
	var out []string
	for i := 0; i < f.count(); i++ {
		if a := f.argsOf(i); !isLearning(a) {
			out = append(out, a)
		}
	}
	return out
}

func isLearning(call string) bool {
	return strings.HasSuffix(call, " -t 1 -O 0") || strings.Contains(call, " -t 1 -O 0 ")
}

func TestOnlyABudgetMakesAPeerAutomatic(t *testing.T) {
	for _, tc := range []struct {
		p    Peer
		want bool
	}{
		{Peer{Throughput: true, ThroughputBudget: 1 << 30}, true},
		{Peer{Throughput: true, ThroughputBudget: 1 << 30, ThroughputEvery: time.Hour}, false}, // a schedule of its own: the budget only caps it
		{Peer{Throughput: true}, false},                                                        // the cluster's own schedule and budget
		{Peer{ThroughputBudget: 1 << 30}, false},                                               // not asked to run tests
	} {
		if got := tc.p.Automatic(); got != tc.want {
			t.Errorf("%+v: Automatic = %v", tc.p, got)
		}
	}
}

// The first run towards a peer is a one-second one, to learn what a test carries; it is how a test is sized without anyone
// saying how fast the link is, and it costs little on any link. From then on the tests are as long as configured and carry no
// rate cap.
func TestAnAutomaticPeerLearnsTheRateWithAShortRunAndThenTestsAtFullSpeed(t *testing.T) {
	f := &fakeIperf{link: 400e6}
	c, ck := capFor(t, f, func(c *Capacity) { c.Parallel = 0 })
	automatic(c, 100<<30)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	settle(c)
	if f.count() != 4 || len(realRuns(f)) != 2 {
		t.Fatalf("%d runs in the first cycle, %d of them whole tests; want a one-second run before each of the two", f.count(), len(realRuns(f)))
	}
	for _, a := range realRuns(f) {
		if durationOf(a) != 10 || strings.Contains(a, " -b ") {
			t.Errorf("the whole test should run at full length and unthrottled: %s", a)
		}
	}
	n := f.count()
	ck.add(25 * time.Hour)
	settle(c)
	if f.count() != n+2 {
		t.Fatalf("the second cycle ran %d tests, want the two without a first run", f.count()-n)
	}
	for i := n; i < f.count(); i++ {
		if a := f.argsOf(i); isLearning(a) || durationOf(a) != 10 || strings.Contains(a, " -b ") {
			t.Errorf("the next cycle should run the whole test, unthrottled: %s", a)
		}
	}
	if v := boundedOf(render(c.Meter), "forward"); v != "0" {
		t.Errorf("a test that was never throttled is marked as a floor: bounded = %q", v)
	}
}

// What the allowance cannot pay for in whole is run shorter, not slower: the rate is still the link's.
func TestAnAllowanceSmallerThanAWholeTestShortensTheTestInsteadOfThrottlingIt(t *testing.T) {
	f := &fakeIperf{link: 400e6} // a whole test is about 650 MB
	c, _ := capFor(t, f, func(c *Capacity) { c.Reverse, c.Parallel = false, 0 })
	const allowance = 400 << 20
	automatic(c, allowance)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	settle(c)
	runs := realRuns(f)
	if len(runs) != 1 {
		t.Fatalf("%d whole tests", len(runs))
	}
	if d := durationOf(runs[0]); d < 3 || d >= 10 || strings.Contains(runs[0], " -b ") {
		t.Errorf("want a shorter run with no rate cap, got %s", runs[0])
	}
	if used := pairUsed(t, c); used > allowance {
		t.Errorf("the allowance was exceeded: %d", used)
	}
	if v := boundedOf(render(c.Meter), "forward"); v != "0" {
		t.Errorf("a shorter run is not a floor: bounded = %q", v)
	}
}

// When the allowance pays for only some of the tests of the plan, the others are not starved: the next cycle goes on where this
// one stopped, so every test comes round.
func TestAnAllowanceSmallerThanThePlanLetsEveryTestComeRound(t *testing.T) {
	f := &fakeIperf{link: 400e6}
	c, ck := capFor(t, f, func(c *Capacity) {}) // tcp and parallel, both ways: four tests of about 650 MB
	const allowance = 1500 << 20
	automatic(c, allowance)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for cycle := 0; cycle < 8; cycle++ {
		n := f.count()
		settle(c)
		for i := n; i < f.count(); i++ {
			a := f.argsOf(i)
			seen[fmt.Sprintf("P=%v R=%v", strings.Contains(a, " -P "), strings.Contains(a, " -R"))] = true
		}
		if used := pairUsed(t, c); used > allowance {
			t.Fatalf("cycle %d: the allowance of %d was exceeded: %d", cycle, allowance, used)
		}
		ck.add(25 * time.Hour)
	}
	if len(seen) != 4 {
		t.Errorf("only %v ran in eight days", seen)
	}
}

// The next cycle comes when the allowance has paid back what this one carried: a small cycle on a large allowance is soon again,
// never sooner than an hour.
func TestTheNextCycleFollowsWhenTheAllowanceHasPaidBackTheLastOne(t *testing.T) {
	t.Run("never before an hour", func(t *testing.T) {
		f := &fakeIperf{link: 400e6}
		c, ck := capFor(t, f, func(c *Capacity) { c.Parallel = 0 })
		automatic(c, 100<<30)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		settle(c)
		ck.add(55 * time.Minute)
		n := f.count()
		settle(c)
		if f.count() != n {
			t.Fatal("the cycle came back before an hour")
		}
		ck.add(15 * time.Minute) // past an hour and 10%
		settle(c)
		if f.count() == n {
			t.Fatal("the cycle did not come back after an hour and 10%")
		}
	})
	t.Run("in proportion to what it carried", func(t *testing.T) {
		f := &fakeIperf{link: 400e6}
		c, ck := capFor(t, f, func(c *Capacity) { c.Parallel = 0 })
		const allowance = 4 << 30
		automatic(c, allowance)
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		settle(c)
		carried := pairUsed(t, c)
		every := time.Duration(float64(24*time.Hour) * float64(carried) / float64(allowance))
		if every < 2*time.Hour || every > 20*time.Hour {
			t.Fatalf("test setup: a cycle that carries %d should be paid back in hours, not %s", carried, every)
		}
		n := f.count()
		ck.add(every * 95 / 100)
		settle(c)
		if f.count() != n {
			t.Errorf("the cycle came back at 95%% of %s", every)
		}
		ck.add(every * 16 / 100)
		settle(c)
		if f.count() == n {
			t.Errorf("the cycle did not come back after %s and 10%%", every)
		}
	})
}

// A cycle that does not fit the window yet is held back once, for as long as the window needs, rather than every half hour, and
// nothing is run, not even the first run that sizes it, until the window has room.
func TestAHeldBackAutomaticCycleWaitsOnceForTheWindowToHaveRoom(t *testing.T) {
	f := &fakeIperf{link: 400e6}
	c, ck := capFor(t, f, func(c *Capacity) { c.Parallel = 0 })
	automatic(c, 1000<<20) // room for one whole test (about 650 MB) and its first run, not two
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	settle(c) // the forward test
	n := f.count()
	for i := 0; i < 4; i++ {
		ck.add(5 * time.Hour)
		settle(c) // the reverse test is due after about 16 hours, and then waits for the forward one to leave the window
	}
	if got := skipped(c, "budget"); got != "1" {
		t.Errorf("held back %s times, want once", got)
	}
	if f.count() != n {
		t.Errorf("%d runs while it waited for the window", f.count()-n)
	}
	ck.add(10 * time.Hour)
	settle(c)
	if got := len(realRuns(f)); got != 2 {
		t.Errorf("the second test did not run once the window had room: %d whole tests", got)
	}
	if learned := f.count() - len(realRuns(f)); learned != 2 {
		t.Errorf("%d first runs for two directions", learned)
	}
	if got := skipped(c, "budget"); got != "1" {
		t.Errorf("held back %s times in all, want once", got)
	}
}

// An allowance too small for any run of a test says so once and tests nothing; it does not spin.
func TestAnAllowanceTooSmallForAnyRunSaysSoAndTestsNothing(t *testing.T) {
	f := &fakeIperf{}
	var logs bytes.Buffer
	c, ck := capFor(t, f, func(c *Capacity) { c.Log = newTestLog(&logs) })
	automatic(c, 1000)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		settle(c)
		ck.add(2 * time.Hour)
	}
	if f.count() != 0 {
		t.Errorf("%d tests ran on an allowance of 1000 bytes", f.count())
	}
	if n := strings.Count(logs.String(), "daily allowance is too small"); n == 0 || n > 4 {
		t.Errorf("the log should say once per test kind that the allowance is too small, got %d times:\n%s", n, logs.String())
	}
}

// Not even the shortest run fits, but a slow enough one does: it runs capped, and says so (bounded). With so little to spend there
// is no first run either.
func TestAnAllowanceBelowTheShortestRunFallsBackToACappedTest(t *testing.T) {
	f := &fakeIperf{}
	c, _ := capFor(t, f, func(c *Capacity) { c.Reverse, c.Parallel = false, 0 })
	automatic(c, 2<<20)
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	settle(c)
	if f.count() != 1 {
		t.Fatalf("%d runs", f.count())
	}
	if a := f.argsOf(0); !strings.Contains(a, " -b ") || isLearning(a) {
		t.Errorf("want one capped test, got %s", a)
	}
	if v := boundedOf(render(c.Meter), "forward"); v != "1" {
		t.Errorf("a capped reading should be marked as a floor, bounded = %q", v)
	}
}

// Whatever the allowance, the tests of the plan are all there to be run: nothing is left out for good, and the allowance is not
// exceeded. The three named amounts of the settings page (Light, Standard, Heavy: src/lib/clusterLinks.ts PRESETS) must hold for
// the chart's defaults.
func TestEveryAllowanceOfTheSettingsPageTestsEveryKindOfTestWithoutExceedingItself(t *testing.T) {
	for _, pr := range []struct {
		name string
		mb   uint64
		kind int // the kinds of test (plain, several streams, UDP ramp) that should have run in two weeks, each way
	}{{"light", 1024, 2}, {"standard", 5120, 6}, {"heavy", 20480, 6}} {
		t.Run(pr.name, func(t *testing.T) {
			f := &fakeIperf{capacity: 100e6, link: 400e6}
			var logs bytes.Buffer
			c, ck := capFor(t, f, func(c *Capacity) {
				c.Duration, c.Omit = 10*time.Second, 2*time.Second
				c.UDP = UDPRamp{Enabled: true, Start: 5e6, Max: 200e6, Step: 5 * time.Second, Loss: 0.01}
				c.Every = 12 * time.Hour
				c.Budget = 10 << 30
				c.Log = newTestLog(&logs)
			})
			automatic(c, pr.mb<<20)
			if err := c.Start(); err != nil {
				t.Fatal(err)
			}
			seen := map[string]bool{}
			for step := 0; step < 14*4; step++ { // two weeks, looked at every six hours
				n := f.count()
				settle(c)
				for j := n; j < f.count(); j++ {
					a := f.argsOf(j)
					kind := "tcp"
					switch {
					case strings.Contains(a, " -u "):
						kind = "udp"
					case strings.Contains(a, " -P "):
						kind = "parallel"
					}
					seen[kind+fmt.Sprint(strings.Contains(a, " -R"))] = true
				}
				if used := pairUsed(t, c); used > pr.mb<<20 {
					t.Fatalf("step %d: %d bytes carried in a window, allowance %d", step, used, pr.mb<<20)
				}
				ck.add(6 * time.Hour)
			}
			if len(seen) < pr.kind {
				t.Errorf("%s: only %v ran in two weeks\n%s", pr.name, seen, logs.String())
			}
			if !seen["tcpfalse"] || !seen["tcptrue"] {
				t.Errorf("%s: both directions of the plain test should have run: %v", pr.name, seen)
			}
			if strings.Contains(logs.String(), "left out") {
				t.Errorf("%s: a test was left out:\n%s", pr.name, logs.String())
			}
		})
	}
}

// The window says exactly how long until a test fits: as long as it takes for enough of the oldest entries to leave it.
func TestALedgerSaysHowLongUntilATestFits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	l := &Ledger{Window: 24 * time.Hour, Limit: 100}
	l.Charge(now, 40)
	l.Charge(now.Add(2*time.Hour), 40)
	at := now.Add(3 * time.Hour)
	for _, tc := range []struct {
		est  uint64
		want time.Duration
	}{
		{20, 0},              // fits beside both
		{30, 21 * time.Hour}, // needs the first to leave: it leaves 24 h after it was charged, and it is 3 h in
		{70, 23 * time.Hour}, // needs both to leave: the second leaves 26 h in
		{101, 0},             // would not fit an empty window: waiting does not help
	} {
		if got := l.Wait(at, tc.est); got != tc.want {
			t.Errorf("Wait(%d) = %s, want %s", tc.est, got, tc.want)
		}
	}
	if got := (&Ledger{Window: time.Hour}).Wait(at, 1<<40); got != 0 {
		t.Errorf("a ledger with no limit made a test wait %s", got)
	}
}
