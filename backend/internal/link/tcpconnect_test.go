package link

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

func connectFor(t *testing.T, dial DialFunc, peers ...*ikhnosv1.LinkPeer) *TCPConnect {
	t.Helper()
	p := NewPeers()
	p.Set("cl-me", peers)
	m := NewMeter(p)
	m.OnlyProbes()
	m.EnableConnects()
	c := &TCPConnect{Meter: m, Dial: dial, Port: 8861}
	c.Defaults()
	return c
}

// listener accepts and closes connections on a loopback port; dialing any address returns a connection to it.
func listener(t *testing.T) DialFunc {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listener:", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, ln.Addr().String())
	}
}

func TestAConnectionThatOpensIsTimedAndCounted(t *testing.T) {
	c := connectFor(t, listener(t), peer("cl-a", "A", "10.0.0.1"))
	c.Round(context.Background())
	c.Round(context.Background())
	got := render(c.Meter)
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "2" {
		t.Fatalf("two connections should be counted: %v", got)
	}
	if got[`ikhnos_link_tcp_connect_seconds_bucket{cluster="cl-me",peer="cl-a",le="+Inf"}`] != "2" {
		t.Fatalf("buckets: %v", got)
	}
	for _, r := range failReasons {
		if got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="`+r+`"}`] != "0" {
			t.Fatalf("no failure expected for %s: %v", r, got)
		}
	}
	if _, ok := got[`ikhnos_link_tcp_connect_last_success_timestamp_seconds{cluster="cl-me",peer="cl-a"}`]; !ok {
		t.Fatal("last success missing")
	}
}

func TestFailuresAreCountedByTheirReason(t *testing.T) {
	cases := map[string]error{
		"refused":     &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED},
		"unreachable": &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH},
		"reset":       &net.OpError{Op: "dial", Err: syscall.ECONNRESET},
		"timeout":     context.DeadlineExceeded,
		"other":       errors.New("boom"),
	}
	for reason, err := range cases {
		c := connectFor(t, func(context.Context, string, string) (net.Conn, error) { return nil, err }, peer("cl-a", "A", "10.0.0.1"))
		c.Round(context.Background())
		got := render(c.Meter)
		key := `ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="` + reason + `"}`
		if got[key] != "1" {
			t.Errorf("%s: want one failure, got %v", reason, got)
		}
		if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "0" {
			t.Errorf("%s: a failure must not be timed", reason)
		}
		if _, ok := got[`ikhnos_link_tcp_connect_last_success_timestamp_seconds{cluster="cl-me",peer="cl-a"}`]; ok {
			t.Errorf("%s: no success to report", reason)
		}
	}
}

func TestADownNodeIsSkippedNotCountedAsALinkFailure(t *testing.T) {
	good := listener(t)
	var mu sync.Mutex
	tried := map[string]int{}
	dial := func(ctx context.Context, n, addr string) (net.Conn, error) {
		mu.Lock()
		tried[addr]++
		mu.Unlock()
		if strings.HasPrefix(addr, "10.0.0.1:") {
			return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
		}
		return good(ctx, n, addr)
	}
	c := connectFor(t, dial, peer("cl-a", "A", "10.0.0.1", "10.0.0.2"))
	for i := 0; i < 4; i++ {
		c.Round(context.Background())
	}
	got := render(c.Meter)
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "4" {
		t.Fatalf("the second address answers every time: %v", got)
	}
	if got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="refused"}`] != "0" {
		t.Fatalf("a down node is not a link failure: %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if tried["10.0.0.2:8861"] != 4 || tried["10.0.0.1:8861"] > 1 {
		t.Fatalf("the address that answered should be tried first from then on: %v", tried)
	}
}

func TestAPeerThatIsNotProbedIsNotTimed(t *testing.T) {
	called := false
	lp := peer("cl-a", "A", "10.0.0.1")
	lp.NoProbe = true
	c := connectFor(t, func(context.Context, string, string) (net.Conn, error) { called = true; return nil, errors.New("x") }, lp)
	c.Round(context.Background())
	if called {
		t.Fatal("no connection should be opened for a pair with probing off")
	}
	for k := range render(c.Meter) {
		if strings.Contains(k, "tcp_connect") {
			t.Fatalf("a pair with probing off has no connect series: %s", k)
		}
	}
}

func TestConnectSeriesAreAbsentUntilTurnedOn(t *testing.T) {
	p := NewPeers()
	p.Set("cl-me", []*ikhnosv1.LinkPeer{peer("cl-a", "A", "10.0.0.1")})
	m := NewMeter(p)
	m.EnableProbes()
	for k := range render(m) {
		if strings.Contains(k, "tcp_connect") {
			t.Fatalf("a cluster that does not time connections must not report them: %s", k)
		}
	}
}

func TestConnectSettingsAreChecked(t *testing.T) {
	for name, c := range map[string]*TCPConnect{
		"port":    {Port: 70000, Every: 15 * time.Second, Timeout: time.Second},
		"every":   {Port: 861, Every: time.Second, Timeout: 500 * time.Millisecond},
		"timeout": {Port: 861, Every: 15 * time.Second, Timeout: time.Millisecond},
		"order":   {Port: 861, Every: 6 * time.Second, Timeout: 6 * time.Second},
	} {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	c := &TCPConnect{}
	c.Defaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAPeerTheOtherClusterTimesIsNotTimedHere(t *testing.T) {
	var mu sync.Mutex
	dialed := map[string]bool{}
	served := peer("cl-a", "A", "10.0.0.1")
	served.SkipProbe = true
	c := connectFor(t, func(_ context.Context, _, addr string) (net.Conn, error) {
		mu.Lock()
		dialed[addr] = true
		mu.Unlock()
		return nil, errors.New("x")
	}, served, peer("cl-b", "B", "10.0.1.1"))
	c.Round(context.Background())
	mu.Lock()
	defer mu.Unlock()
	if dialed["10.0.0.1:8861"] || !dialed["10.0.1.1:8861"] {
		t.Fatalf("dialed %v: the pair the other cluster times must be left alone and the other one not", dialed)
	}
	for k := range render(c.Meter) {
		if strings.Contains(k, `peer="cl-a"`) && strings.Contains(k, "tcp_connect") {
			t.Fatalf("a connect series for a pair timed by the other cluster: %s", k)
		}
	}
}

// A handshake that queues behind a saturated link is the load, not the baseline: while a throughput test runs nothing is
// recorded, neither a sample nor a failure, and the series are otherwise unchanged.
func TestNoSampleIsTakenWhileAThroughputTestRuns(t *testing.T) {
	dialed := 0
	fail := false
	c := connectFor(t, func(ctx context.Context, n, a string) (net.Conn, error) {
		dialed++
		if fail {
			return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
		}
		return listener(t)(ctx, n, a)
	}, peer("cl-a", "A", "10.0.0.1"))
	c.Testing = &Flag{}

	exit := c.Testing.Enter()
	c.Round(context.Background())
	fail = true
	c.Round(context.Background())
	exit()
	if dialed != 0 {
		t.Errorf("%d connections were opened during a test", dialed)
	}
	got := render(c.Meter)
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "0" {
		t.Errorf("a sample was taken during a test: %v", got)
	}
	for _, r := range connectFailReasons {
		if got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="`+r+`"}`] != "0" {
			t.Errorf("a failure (%s) was counted during a test", r)
		}
	}

	// Once the test is over, rounds count again, successes and failures alike.
	fail = false
	c.Round(context.Background())
	fail = true
	c.Round(context.Background())
	got = render(c.Meter)
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "1" ||
		got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="refused"}`] != "1" {
		t.Errorf("after the test: %v", got)
	}
}

// A test that starts or ends while a handshake is in flight taints that sample too.
func TestASampleThatOverlapsAThroughputTestStartIsDropped(t *testing.T) {
	var c *TCPConnect
	c = connectFor(t, func(ctx context.Context, n, a string) (net.Conn, error) {
		exit := c.Testing.Enter() // the test starts, and is over again, before this handshake returns
		exit()
		return listener(t)(ctx, n, a)
	}, peer("cl-a", "A", "10.0.0.1"))
	c.Testing = &Flag{}
	c.Round(context.Background())
	if got := render(c.Meter); got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "0" {
		t.Errorf("a sample that overlapped a test was kept: %v", got)
	}
	c.Dial = listener(t)
	c.Round(context.Background())
	if got := render(c.Meter); got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "1" {
		t.Errorf("the next, clean sample was not taken: %v", got)
	}
}

func TestWhatIsRememberedForAGonePeerGoesAfterTheGrace(t *testing.T) {
	c := connectFor(t, listener(t), peer("cl-a", "A", "10.0.0.1"))
	c.Round(context.Background())
	if c.good["cl-a"] == "" {
		t.Fatal("the answering address was not remembered")
	}
	t0 := time.Unix(1_700_000_000, 0)
	c.forget(nil, t0)
	c.forget(nil, t0.Add(peerGrace-time.Minute))
	if c.good["cl-a"] == "" {
		t.Error("forgotten inside the grace")
	}
	c.forget([]Peer{{ID: "cl-a"}}, t0.Add(peerGrace-time.Second)) // back in time
	c.forget(nil, t0.Add(peerGrace))
	c.forget(nil, t0.Add(peerGrace+time.Minute))
	if c.good["cl-a"] == "" {
		t.Error("a return inside the grace did not restart it")
	}
	c.forget(nil, t0.Add(2*peerGrace+2*time.Minute))
	if len(c.good) != 0 || len(c.failed) != 0 || len(c.absent) != 0 {
		t.Errorf("left: %v %v %v", c.good, c.failed, c.absent)
	}
}

func TestFailuresAreLoggedAsWarningsOnlyWhenTheyChangeAndRoundsThatOverrunAreSaid(t *testing.T) {
	var buf bytes.Buffer
	var err error
	c := connectFor(t, func(context.Context, string, string) (net.Conn, error) { return nil, err }, peer("cl-a", "A", "10.0.0.1"))
	c.Log = slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	count := func(s string) int { return strings.Count(buf.String(), s) }

	err = &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
	c.Round(context.Background())
	c.Round(context.Background())
	if count("level=WARN") != 1 || count("level=DEBUG") != 1 {
		t.Fatalf("a steady failure should warn once and then be quiet:\n%s", buf.String())
	}
	err = context.DeadlineExceeded
	c.Round(context.Background())
	if count("level=WARN") != 2 {
		t.Errorf("a new reason should warn:\n%s", buf.String())
	}
	err = nil
	c.Dial = listener(t)
	c.Round(context.Background())
	if count("works again") != 1 {
		t.Errorf("recovery was not said:\n%s", buf.String())
	}

	buf.Reset()
	c.noteRound(context.Background(), 2*c.Every)
	c.noteRound(context.Background(), 3*c.Every)
	if count("longer than the interval") != 1 || count("level=WARN") != 1 {
		t.Errorf("an overrun should be warned once:\n%s", buf.String())
	}
	c.noteRound(context.Background(), c.Every/2)
	if count("fit the interval again") != 1 {
		t.Errorf("the end of the overrun was not said:\n%s", buf.String())
	}
}

// stagedDial answers the addresses in answers (a loopback connection), refuses those in refuse, and leaves every other one
// silent until its context ends, as a firewall that drops the packets does.
func stagedDial(t *testing.T, answers, refuse []string, tried *sync.Map) DialFunc {
	t.Helper()
	good := listener(t)
	return func(ctx context.Context, n, addr string) (net.Conn, error) {
		tried.Store(addr, true)
		host, _, _ := net.SplitHostPort(addr)
		switch {
		case slices.Contains(answers, host):
			return good(ctx, n, addr)
		case slices.Contains(refuse, host):
			return nil, &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

func TestAnAddressThatIsUpIsNotMadeToWaitForOnesThatAreSilent(t *testing.T) {
	// A peer reached over its mesh address only: the public ones are dropped, as by a firewall that lets the mesh in.
	var tried sync.Map
	dial := stagedDial(t, []string{"100.64.0.7"}, nil, &tried)
	c := connectFor(t, dial, peer("cl-a", "A", "203.0.113.2", "2001:db8::2", "100.64.0.7"))
	c.Timeout, c.Stagger = 600*time.Millisecond, 20*time.Millisecond
	started := time.Now()
	c.Round(context.Background())
	if took := time.Since(started); took < c.Timeout {
		t.Errorf("the round did not wait for the silent addresses (%s): the kinds that did not answer would not be known", took)
	}
	got := render(c.Meter)
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "1" {
		t.Fatalf("the mesh address answers, so the round is one sample: %v", got)
	}
	// The sample is the handshake of the address that answered, not the time the round took.
	sum, err := strconv.ParseFloat(got[`ikhnos_link_tcp_connect_seconds_sum{cluster="cl-me",peer="cl-a"}`], 64)
	if err != nil || sum > 0.3 {
		t.Errorf("the sample (%v, %v) includes the wait for the silent addresses", sum, err)
	}
	for _, r := range failReasons {
		if got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="`+r+`"}`] != "0" {
			t.Errorf("a path that answers is no failure, but %s was counted: %v", r, got)
		}
	}
	for kind, want := range map[string]string{"mesh": "1", "public": "0"} {
		if v := got[`ikhnos_link_tcp_connect_kind_up{cluster="cl-me",peer="cl-a",kind="`+kind+`"}`]; v != want {
			t.Errorf("kind %s: got %q, want %s: %v", kind, v, want, got)
		}
	}
	if _, ok := got[`ikhnos_link_tcp_connect_kind_up{cluster="cl-me",peer="cl-a",kind="private"}`]; ok {
		t.Error("a kind the peer has no address of is reported")
	}
	if c.good["cl-a"] != "100.64.0.7" || c.Meter.Answered("cl-a") != "100.64.0.7" {
		t.Errorf("the address that answered is not the one remembered: %q %q", c.good["cl-a"], c.Meter.Answered("cl-a"))
	}

	// The next round starts at the address that answered, and the kinds are reported again.
	c.Round(context.Background())
	got = render(c.Meter)
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "2" || got[`ikhnos_link_tcp_connect_kind_up{cluster="cl-me",peer="cl-a",kind="public"}`] != "0" {
		t.Errorf("second round: %v", got)
	}
}

func TestEveryKindIsReportedAsAnsweringWhenOneAddressOfItDoes(t *testing.T) {
	var tried sync.Map
	dial := stagedDial(t, []string{"203.0.113.2", "203.0.113.3", "203.0.113.4", "100.64.0.7"}, nil, &tried)
	c := connectFor(t, dial, peer("cl-a", "A", "203.0.113.2", "203.0.113.3", "203.0.113.4", "100.64.0.7"))
	c.Timeout, c.Stagger = 600*time.Millisecond, 20*time.Millisecond
	c.Round(context.Background())
	got := render(c.Meter)
	for _, kind := range []string{"mesh", "public"} {
		if got[`ikhnos_link_tcp_connect_kind_up{cluster="cl-me",peer="cl-a",kind="`+kind+`"}`] != "1" {
			t.Errorf("kind %s answered: %v", kind, got)
		}
	}
	// The public addresses are the same path as one another: once one has answered the others are not dialed, and while none has,
	// no more than two are.
	n := 0
	tried.Range(func(k, _ any) bool { n++; return true })
	if n > 3 {
		var all []string
		tried.Range(func(k, _ any) bool { all = append(all, k.(string)); return true })
		t.Errorf("dialed %v, more than the mesh address and two public ones", all)
	}
}

func TestARoundFailsByTheReasonOfTheAddressItTriedFirst(t *testing.T) {
	var tried sync.Map
	dial := stagedDial(t, nil, []string{"10.0.0.1"}, &tried) // the others are silent
	lp := peer("cl-a", "A", "10.0.0.1", "203.0.113.2", "100.64.0.7")
	c := connectFor(t, dial, lp)
	c.Meter.Peers.set("cl-me", []*ikhnosv1.LinkPeer{lp}, map[string][]string{"cl-a": {"10.0.0.1"}})
	c.Timeout, c.Stagger = 500*time.Millisecond, 10*time.Millisecond
	c.Round(context.Background())
	got := render(c.Meter)
	if got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="refused"}`] != "1" || got[`ikhnos_link_tcp_connect_failures_total{cluster="cl-me",peer="cl-a",reason="timeout"}`] != "0" {
		t.Errorf("the first address the round tried was refused, and that is the reason: %v", got)
	}
	if got[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "0" {
		t.Errorf("nothing opened: %v", got)
	}
	for _, kind := range []string{"mesh", "private", "public"} {
		if got[`ikhnos_link_tcp_connect_kind_up{cluster="cl-me",peer="cl-a",kind="`+kind+`"}`] != "0" {
			t.Errorf("kind %s did not answer: %v", kind, got)
		}
	}
	if c.Meter.Answered("cl-a") != "" {
		t.Error("an address is remembered as answering when none did")
	}
}

func TestAnAddressIsTriedAtOnceWhenTheOneBeforeItFails(t *testing.T) {
	// The first address is refused straight away and the second is up: a long stagger must not make the round wait for it.
	var tried sync.Map
	dial := stagedDial(t, []string{"203.0.113.2"}, []string{"10.0.0.1"}, &tried)
	lp := peer("cl-a", "A", "10.0.0.1", "203.0.113.2")
	c := connectFor(t, dial, lp)
	c.Meter.Peers.set("cl-me", []*ikhnosv1.LinkPeer{lp}, map[string][]string{"cl-a": {"10.0.0.1"}})
	c.Timeout, c.Stagger = 2*time.Second, 2*time.Second
	started := time.Now()
	c.Round(context.Background())
	if took := time.Since(started); took > time.Second {
		t.Errorf("the round waited %s for the stagger after a refusal", took)
	}
	if render(c.Meter)[`ikhnos_link_tcp_connect_seconds_count{cluster="cl-me",peer="cl-a"}`] != "1" {
		t.Error("the second address answered and was not timed")
	}
}

func TestTheStaggerIsHeldBetweenZeroAndTheTimeout(t *testing.T) {
	for _, bad := range []time.Duration{-time.Millisecond, 4 * time.Second} {
		c := &TCPConnect{Stagger: bad}
		c.Defaults()
		if c.Validate() == nil {
			t.Errorf("a stagger of %s is accepted", bad)
		}
	}
	c := &TCPConnect{}
	c.Defaults()
	if err := c.Validate(); err != nil || c.Stagger != 250*time.Millisecond {
		t.Errorf("the default: %v %s", err, c.Stagger)
	}
}

func TestTheAddressesOfARoundCoverEveryKindAndFamilyBeforeRepeatingOne(t *testing.T) {
	lp := peer("cl-a", "A", "10.0.0.1", "10.0.0.2", "10.0.0.3", "203.0.113.2", "2001:db8::1", "2001:db8::2", "2001:db8::3", "100.64.0.7")
	ps := NewPeers()
	ps.Set("cl-me", []*ikhnosv1.LinkPeer{lp})
	p := ps.List()[0]
	for i := 0; i < 50; i++ {
		got := raceAddresses(p, "10.0.0.2", 6)
		if len(got) != 6 || got[0] != "10.0.0.2" {
			t.Fatalf("the address that answered is not first, or the round is not six wide: %v", got)
		}
		forms := map[string]bool{}
		for _, s := range got[:4] {
			a := netip.MustParseAddr(s)
			forms[Class(a)+strconv.FormatBool(a.Is6())] = true
		}
		if len(forms) != 4 {
			t.Fatalf("the first four of %v are not one of each of the four kinds and families", got)
		}
	}
	// The one that answered stays in even when the traffic names more addresses than there is room for.
	lp.Addresses = []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	ps.Set("cl-me", []*ikhnosv1.LinkPeer{lp})
	p = ps.List()[0]
	ps.set("cl-me", []*ikhnosv1.LinkPeer{lp}, map[string][]string{"cl-a": {"10.0.0.1", "10.0.0.3"}})
	p = ps.List()[0]
	if got := raceAddresses(p, "10.0.0.2", 2); !slices.Contains(got, "10.0.0.2") || len(got) != 2 {
		t.Errorf("with room for two: %v", got)
	}
}

func TestAKindWithOneSilentAddressAndOneThatAnswersIsUp(t *testing.T) {
	// One node of the peer is down; its other node answers. That is not a path that is closed.
	var tried sync.Map
	dial := stagedDial(t, []string{"203.0.113.3", "100.64.0.7"}, nil, &tried)
	c := connectFor(t, dial, peer("cl-a", "A", "203.0.113.2", "203.0.113.3", "100.64.0.7"))
	c.Timeout, c.Stagger = 400*time.Millisecond, 10*time.Millisecond
	for i := 0; i < 5; i++ {
		c.Round(context.Background())
		got := render(c.Meter)
		for _, kind := range []string{"mesh", "public"} {
			if got[`ikhnos_link_tcp_connect_kind_up{cluster="cl-me",peer="cl-a",kind="`+kind+`"}`] != "1" {
				t.Fatalf("round %d: kind %s has an address that answers: %v", i, kind, got)
			}
		}
	}
}
