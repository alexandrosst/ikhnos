package link

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ExecEnvFunc is ExecFunc with extra environment, for the one thing iperf3 reads from it (the password).
type ExecEnvFunc func(ctx context.Context, env []string, name string, args ...string) (stdout, stderr []byte, err error)

func osExecEnv(ctx context.Context, env []string, name string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.Env = append([]string{"PATH=/usr/bin:/bin", "LC_ALL=C"}, env...)
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	return capBytes(out.Bytes()), capBytes(errb.Bytes()), err
}

// UDPRamp is the loss ramp: UDP at Start, doubled each step until more than Loss of it is lost or Max is reached. The
// highest rate that stayed within Loss is what the link carries without dropping.
type UDPRamp struct {
	Enabled    bool
	Start, Max uint64 // bits per second
	Step       time.Duration
	Loss       float64 // fraction
}

// Capacity runs iperf3 against the responders of the paired clusters, now and then, within limits. Where the one-way probe
// answers "is the link healthy", this answers "how much can it carry, and where does it start to drop": TCP with one
// stream and with several, in each direction, and a UDP ramp that finds the rate at which loss begins.
//
// The limits are what make it fit to leave running: one test at a time anywhere in the process (shared with the one-way
// probe through Gate), a budget of bytes per day, a wait while real traffic is using the link, and an optional time of day.
type Capacity struct {
	Meter *Meter
	Log   *slog.Logger
	// Gate is shared with the Runner so a throughput test never overlaps a one-way probe. Load, if set, says how busy the
	// links are.
	Gate *Gate
	Load *Load
	// Testing is on while an iperf3 client is running. The connect timing reads it (see TCPConnect.Testing): a handshake
	// that queues behind a saturated link is not the idle round trip that series is for.
	Testing Flag

	Bin  string
	Port int

	// Every is how often each peer's whole plan is run. Duration is how long each TCP test sends, Omit how much of its
	// start is left out of the figures (TCP slow start).
	Every, Duration, Omit time.Duration
	// Parallel is the number of streams of the multi-stream test (below 2: not run).
	Parallel int
	// Reverse also tests from the peer to here; Bidir adds a test of both at once.
	Reverse, Bidir bool
	UDP            UDPRamp
	// DSCP lists the classes the single-stream TCP test and the UDP ramp are repeated for. Nil: unmarked only.
	DSCP []int
	// TCPMaxRate caps each TCP stream (bits per second; 0: as fast as it goes).
	TCPMaxRate uint64

	// Budget is the bytes the tests may carry per BudgetWindow, all peers together (0: unlimited).
	Budget       uint64
	BudgetWindow time.Duration
	// BusyFraction: a test waits while the observed traffic in its direction is above this fraction of the rate the last TCP
	// test reached there. SkipAbove is the same with an absolute rate, used until a first test has set the former.
	BusyFraction float64
	SkipAbove    uint64
	Window       TimeWindow

	// User, KeyFile (the server's RSA public key) and PasswordFile select iperf3's authenticated mode, in which the server
	// refuses anyone without the credentials.
	User, KeyFile, PasswordFile string
	// AutoAuth says the install lets the Ikhnos server issue the credentials (Peer.Iperf): a peer without them is not tested,
	// and counts as an `auth` failure, rather than being tried without authentication. The credentials of User, KeyFile and
	// PasswordFile, when set, take precedence for every peer.
	AutoAuth bool
	// KeyDir is where the peers' public keys are written for iperf3 to read (the prober's private, in-memory scratch space; empty:
	// a directory made under the system's temporary one). Public keys only: a password never touches a file.
	KeyDir string

	// Pause is left between two tests so that the one-way probe gets its turn. StartAfter bounds the random delay before a
	// peer is first tested (negative: none).
	Pause      time.Duration
	StartAfter time.Duration

	Exec ExecEnvFunc
	Now  func() time.Time
	// PortSpeed tells the speed of the port the tests towards an address leave through (nil: read from the system).
	PortSpeed PortSpeedFunc

	password string
	ledger   *Ledger
	keyFiles map[string]string // sha256 of a public key -> the file it is in

	mu        sync.Mutex
	pairs     map[string]*Ledger // peer -> its own daily budget, for the peers that have one
	peers     map[string]*capPeer
	good      map[string]string
	lastBytes map[string]uint64
	lastBits  map[string]float64
	// lastBounded says which of the readings in lastBits came up to a rate cap, so they are floors and say nothing about the size of a test without one.
	lastBounded map[string]bool
	warned      map[string]bool
	// absent is since when a peer has not been among the peers to test (see peerGrace).
	absent absence
}

type capTest struct {
	kind string // tcp, tcp-parallel, tcp-bidir, udp-ramp
	dir  Direction
	dscp int
}

func (t capTest) key(peer string) string {
	return fmt.Sprintf("%s|%s|%s|%d", peer, t.kind, t.dir, t.dscp)
}

type capPeer struct {
	next    time.Time // when the next cycle starts
	retryAt time.Time // the unfinished cycle waits until then
	queue   []capTest // what is left of this cycle
	// ran is true once a test of this cycle has finished: the peer answers, so a failure after that belongs to the test.
	ran bool
	// failures counts the cycles in a row that failed at their first test, for the back-off.
	failures int

	// The rest is for a peer whose tests are automatic (Peer.Automatic). size is how each test of the running cycle was sized,
	// cursor the place in the plan the next cycle starts at (what a cycle's share of the allowance does not pay for waits for
	// a later one, so every test comes round), cycleBytes what the running cycle has carried.
	size       map[capTest]testSize
	cursor     int
	cycleBytes uint64
}

// testSize is how an automatic peer's test differs from the plan's: a shorter run (dur, when the allowance is smaller than a
// whole test), a UDP ramp that stops lower (top), or, when not even the shortest run fits, a capped rate (capped). A test whose
// size is not known is sized only when it is about to run (learn).
type testSize struct {
	dur    time.Duration
	top    uint64
	capped bool
	// learn says what a whole test carries is not known yet: a one-second run finds out just before the test, which is then sized.
	learn bool
}

// minAutoDuration is the shortest TCP run an automatic peer is given: shorter than this is mostly slow start.
const minAutoDuration = 3 * time.Second

// learnBytes is what the one-second run that finds out the size of a test carries on a link of 1 Gbit/s. An allowance below it
// does not pay for a first run on a link of any size, so the test is capped to the allowance instead.
const learnBytes = 125_000_000

// Defaults fills what was left unset.
func (c *Capacity) Defaults() {
	if c.Bin == "" {
		c.Bin = "iperf3"
	}
	if c.Port == 0 {
		c.Port = 8862
	}
	if c.Every == 0 {
		c.Every = 12 * time.Hour
	}
	if c.Duration == 0 {
		c.Duration = 10 * time.Second
	}
	if c.UDP.Enabled {
		if c.UDP.Start == 0 {
			c.UDP.Start = 5e6
		}
		if c.UDP.Max == 0 {
			c.UDP.Max = 200e6
		}
		if c.UDP.Step == 0 {
			c.UDP.Step = 5 * time.Second
		}
		if c.UDP.Loss == 0 {
			c.UDP.Loss = 0.01
		}
	}
	if len(c.DSCP) == 0 {
		c.DSCP = []int{0}
	}
	if c.BudgetWindow == 0 {
		c.BudgetWindow = 24 * time.Hour
	}
	if c.Pause == 0 {
		c.Pause = 3 * time.Second
	}
	if c.Exec == nil {
		c.Exec = osExecEnv
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.PortSpeed == nil {
		c.PortSpeed = portSpeed
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.ledger == nil {
		c.ledger = &Ledger{Window: c.BudgetWindow, Limit: c.Budget}
	}
	if c.lastBytes == nil {
		c.lastBytes = map[string]uint64{}
	}
}

// Validate checks the settings so a bad value fails at start-up.
func (c *Capacity) Validate() error {
	whole := func(name string, d, lo, hi time.Duration) error {
		if d < lo || d > hi || d%time.Second != 0 {
			return fmt.Errorf("%s must be a whole number of seconds between %s and %s, got %s", name, lo, hi, d)
		}
		return nil
	}
	if c.Every < 10*time.Minute {
		return fmt.Errorf("running the throughput tests more often than every 10 minutes is not supported, got %s", c.Every)
	}
	if err := whole("test duration", c.Duration, 3*time.Second, 60*time.Second); err != nil {
		return err
	}
	if c.Omit < 0 || c.Omit > 10*time.Second || c.Omit >= c.Duration || c.Omit%time.Second != 0 {
		return fmt.Errorf("omitted start must be whole seconds, at most 10s and shorter than the test, got %s", c.Omit)
	}
	if c.Parallel < 0 || c.Parallel > 32 || c.Parallel == 1 {
		return fmt.Errorf("parallel streams must be 0 (off) or between 2 and 32, got %d", c.Parallel)
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("invalid iperf3 port %d", c.Port)
	}
	if c.TCPMaxRate > 400e9 {
		// The same bound as the top of the UDP ramp; it also keeps the byte estimate (rate x streams x seconds) far from
		// overflowing.
		return fmt.Errorf("the TCP rate cap must be at most 400G bit/s, got %d", c.TCPMaxRate)
	}
	if c.UDP.Enabled {
		if c.UDP.Start < 100e3 || c.UDP.Max < c.UDP.Start || c.UDP.Max > 400e9 || c.UDP.Max/c.UDP.Start > 1024 {
			return fmt.Errorf("UDP ramp from %d to %d bit/s is not valid: start at least 100k, top not below start, at most 400G and 1024 times the start", c.UDP.Start, c.UDP.Max)
		}
		if err := whole("UDP step", c.UDP.Step, 2*time.Second, 30*time.Second); err != nil {
			return err
		}
		if c.UDP.Loss <= 0 || c.UDP.Loss > 0.5 {
			return fmt.Errorf("UDP loss threshold must be above 0 and at most 50%%, got %g", c.UDP.Loss*100)
		}
	}
	if len(c.DSCP) > 4 {
		return errors.New("at most 4 DSCP classes")
	}
	seen := map[int]bool{}
	for _, d := range c.DSCP {
		if d < 0 || d > 63 || seen[d] {
			return fmt.Errorf("DSCP classes must be different values between 0 and 63, got %v", c.DSCP)
		}
		seen[d] = true
	}
	if c.BusyFraction < 0 || c.BusyFraction > 1 {
		return fmt.Errorf("busy fraction must be between 0 and 1, got %g", c.BusyFraction)
	}
	if n := countSet(c.User, c.KeyFile, c.PasswordFile); n != 0 && n != 3 {
		return errors.New("authenticated mode needs a user, the server's public key file and a password file")
	}
	return nil
}

func countSet(ss ...string) int {
	n := 0
	for _, s := range ss {
		if s != "" {
			n++
		}
	}
	return n
}

// Start reads the credentials and wires the budget into the meter. It is called once before Run.
func (c *Capacity) Start() error {
	c.Defaults()
	if err := c.Validate(); err != nil {
		return err
	}
	if c.PasswordFile != "" {
		b, err := os.ReadFile(c.PasswordFile)
		if err != nil {
			return fmt.Errorf("password file: %w", err)
		}
		c.password = strings.TrimSpace(string(b))
		if c.password == "" {
			return errors.New("password file is empty")
		}
	}
	c.Meter.SetBudget(func() (uint64, uint64) { return c.ledger.Used(c.Now()), c.ledger.Limit })
	c.Meter.SetPairBudgets(func(peer string) (used, limit uint64, ok bool) {
		c.mu.Lock()
		l := c.pairs[peer]
		c.mu.Unlock()
		if l == nil {
			return 0, 0, false
		}
		l.mu.Lock()
		limit = l.Limit
		l.mu.Unlock()
		return l.Used(c.Now()), limit, true
	})
	return nil
}

// Run tests until ctx ends.
func (c *Capacity) Run(ctx context.Context) {
	if err := c.Start(); err != nil {
		c.Log.Error("throughput tests not started", "err", err)
		return
	}
	for {
		if !sleep(ctx, c.step(ctx)) {
			return
		}
	}
}

// plan is the tests of one cycle for a peer, in order.
func (c *Capacity) plan() []capTest {
	var out []capTest
	dirs := []Direction{Forward}
	if c.Reverse {
		dirs = append(dirs, Reverse)
	}
	for _, ds := range c.DSCP {
		for _, d := range dirs {
			out = append(out, capTest{"tcp", d, ds})
		}
	}
	if c.Parallel >= 2 {
		for _, d := range dirs {
			out = append(out, capTest{"tcp-parallel", d, c.DSCP[0]})
		}
	}
	if c.Bidir {
		out = append(out, capTest{"tcp-bidir", Forward, c.DSCP[0]})
	}
	if c.UDP.Enabled {
		for _, ds := range c.DSCP {
			for _, d := range dirs {
				out = append(out, capTest{"udp-ramp", d, ds})
			}
		}
	}
	return out
}

// peerGrace is how long what is kept for a peer (its place in the schedule, the bytes it has been charged, the last figures,
// and in the meter its probe and connect state) survives the peer's absence from the peer list. The list can be empty or short
// for a moment without the pair having gone: the agent answers an empty one around a restart or while its flow collector is
// paused, and forgetting a peer then would hand it a new random start and a fresh budget, so a whole cycle would run again at
// once. A pair that really is gone is forgotten after this; its series stop at once, as they are only written for listed peers.
const peerGrace = 30 * time.Minute

// absence remembers since when ids have been missing from the peer list. The zero value is ready.
type absence map[string]time.Time

// gone is called for an id on every pass: live says it is listed now. It is true once the id has been unlisted for peerGrace,
// and then forgets the id, so the caller deletes what it keeps for it.
func (a *absence) gone(id string, live bool, now time.Time) bool {
	if live {
		delete(*a, id)
		return false
	}
	if *a == nil {
		*a = absence{}
	}
	since, ok := (*a)[id]
	if !ok {
		(*a)[id] = now
		return false
	}
	if now.Sub(since) < peerGrace {
		return false
	}
	delete(*a, id)
	return true
}

// sync adds new peers, with a random first test time, and forgets the ones that have been gone for peerGrace.
func (c *Capacity) sync(peers []Peer, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.peers == nil {
		c.peers = map[string]*capPeer{}
	}
	live := map[string]bool{}
	for _, p := range peers {
		if !p.Throughput {
			continue // the server did not ask for throughput tests towards this one (or the other side runs them)
		}
		live[p.ID] = true
		if c.peers[p.ID] == nil {
			spread := c.StartAfter
			if spread == 0 {
				spread = min(c.Every/4, 10*time.Minute)
			}
			var d time.Duration
			if spread > 0 {
				d = time.Duration(rand.Int64N(int64(spread) + 1))
			}
			c.peers[p.ID] = &capPeer{next: now.Add(d)}
		}
	}
	for id := range c.peers {
		if c.absent.gone(id, live[id], now) {
			delete(c.peers, id)
			delete(c.good, id)
			delete(c.pairs, id)
			prefix := id + "|"
			dropPrefix(c.lastBytes, prefix)
			dropPrefix(c.lastBits, prefix)
			dropPrefix(c.lastBounded, prefix)
			dropPrefix(c.warned, prefix)
		}
	}
}

// dropPrefix removes the keys of a per-peer map that belong to the peer whose prefix ("id|") is given.
func dropPrefix[V any](m map[string]V, prefix string) {
	for k := range m {
		if strings.HasPrefix(k, prefix) {
			delete(m, k)
		}
	}
}

// step runs at most one test and returns how long to wait before the next look.
func (c *Capacity) step(ctx context.Context) time.Duration {
	now := c.Now()
	var peers []Peer
	for _, p := range c.Meter.Peers.List() {
		if p.Throughput {
			peers = append(peers, p)
		}
	}
	c.sync(peers, now)
	if !c.Window.Contains(now) {
		return min(c.Window.Opens(now), time.Minute)
	}
	// Choose the peer whose turn came first.
	var pick *Peer
	var pickAt time.Time
	c.mu.Lock()
	for i := range peers {
		st := c.peers[peers[i].ID]
		due := st.next
		if len(st.queue) > 0 {
			due = st.retryAt
		}
		if due.After(now) {
			continue
		}
		if pick == nil || due.Before(pickAt) {
			pick, pickAt = &peers[i], due
		}
	}
	var st *capPeer
	var t capTest
	var fresh bool
	if pick != nil {
		st = c.peers[pick.ID]
		fresh = len(st.queue) == 0
	}
	c.mu.Unlock()
	if pick == nil {
		return 5 * time.Second
	}
	if fresh {
		c.notePort(*pick)
		// A cycle is planned outside c.mu: sizing the tests of an automatic peer reads what the last ones carried.
		var queue []capTest
		var size map[capTest]testSize
		cursor := 0
		if pick.Automatic() {
			c.mu.Lock()
			cursor = st.cursor
			c.mu.Unlock()
			queue, size, cursor = c.autoCycle(*pick, cursor)
		} else {
			queue = c.plan()
		}
		c.mu.Lock()
		st.queue, st.size, st.cursor, st.ran, st.cycleBytes = queue, size, cursor, false, 0
		if len(queue) == 0 {
			// Not one test fits the allowance: look again later, when the figures it is worked out from may have changed.
			st.next = now.Add(time.Hour)
			c.mu.Unlock()
			return c.Pause
		}
		c.mu.Unlock()
	}
	c.mu.Lock()
	t = st.queue[0]
	c.mu.Unlock()

	out := c.runTest(ctx, *pick, t)
	now = c.Now()
	// What is counted is recorded after c.mu is released: the meter takes its own lock while it writes (and then asks for
	// the pair budgets, which take c.mu), so calling it with c.mu held is a lock-order inversion.
	var skipped, failed string
	c.mu.Lock()
	switch {
	case ctx.Err() != nil:
	case out.skip != "" && out.drop:
		skipped = out.skip
		st.queue = st.queue[1:]
		c.advance(st, *pick, now)
	case out.skip != "":
		st.retryAt = now.Add(out.retry)
		skipped = out.skip
	case out.fail != "":
		failed = out.fail
		if st.ran || out.fail == "other" {
			// The peer answers (or the test, not the peer, is what is wrong): leave this test out and go on with the
			// rest, so the tests before it are not run again and the ones after it are not starved.
			st.failures = 0
			st.queue = st.queue[1:]
			c.advance(st, *pick, now)
		} else {
			st.queue = nil
			st.failures++
			st.next = now.Add(min(c.Every, 30*time.Minute<<min(st.failures-1, 6)))
		}
	default:
		st.ran, st.failures = true, 0
		st.queue = st.queue[1:]
		c.advance(st, *pick, now)
	}
	c.mu.Unlock()
	if skipped != "" {
		c.Meter.ThroughputSkipped(pick.ID, skipped)
	}
	if failed != "" {
		c.Meter.ThroughputFailed(pick.ID, failed)
	}
	return c.Pause
}

// notePort records the speed of the port the peer's tests leave through, once per cycle: a cable or a switch can change.
func (c *Capacity) notePort(p Peer) {
	var bits uint64
	if addrs := c.candidates(p); len(addrs) > 0 {
		if a, err := netip.ParseAddr(addrs[0]); err == nil {
			bits = c.PortSpeed(a)
		}
	}
	c.Meter.SetPortSpeed(p.ID, bits)
}

// advance moves a peer on after one test of its plan was dealt with: the next test follows at once, or, when the plan is
// done, the next cycle is scheduled. Called with c.mu held.
func (c *Capacity) advance(st *capPeer, p Peer, now time.Time) {
	if len(st.queue) > 0 {
		st.retryAt = now
		return
	}
	if p.Automatic() {
		// The next cycle follows when the allowance has paid back what this one carried, never sooner than an hour or later
		// than a day, and never early: a cycle that starts before its bytes are out of the window waits, and counts as held back.
		every := time.Duration(float64(c.BudgetWindow) * float64(st.cycleBytes) / float64(c.allowanceOf(p)))
		every = min(max(every, time.Hour), 24*time.Hour)
		st.next = now.Add(time.Duration(float64(every) * (1 + 0.1*rand.Float64())))
		return
	}
	every := c.Every
	if p.ThroughputEvery > 0 {
		every = p.ThroughputEvery
	}
	st.next = now.Add(time.Duration(float64(every) * (0.9 + 0.2*rand.Float64())))
}

type outcome struct {
	skip  string // held back, for the metric
	retry time.Duration
	fail  string
	// drop says a held-back test is not tried again this cycle: it can never fit the byte budget, so waiting for it would
	// hold back every test queued behind it (the UDP ramp among them) for good.
	drop bool
}

func (c *Capacity) runTest(ctx context.Context, p Peer, t capTest) outcome {
	now := c.Now()
	est := c.estimate(p, t)
	// A peer with only a daily allowance (Peer.Automatic) is tested at full speed: its cycle was sized when it was planned
	// (autoCycle), so a test here is unthrottled, shorter only when the allowance is smaller than a whole test.
	//
	// Any other peer's tests are sized to the byte budget, so that a cycle always yields a reading whatever the budget is: a
	// cycle's tests share it (and a window holds several cycles when they are more often than the window is long). A test keeps
	// its length (a shorter one would be mostly TCP's slow start) and its rate is capped instead, to what its share pays for
	// over that length; a UDP ramp stops at the highest rate whose whole ramp fits its share. A test that reaches its cap says
	// "at least this" (bounded); one that stays clearly under it has found what the link carries. A share that cannot pay for
	// a rate worth reporting leaves the test out of the cycle.
	var capStream, rampMax uint64
	dur := c.Duration
	whole := uint64((c.Duration + c.Omit).Seconds())
	streams, dirs := c.shape(t)
	var sz testSize
	// size applies how an automatic peer's test was sized to the test: est is what it is expected to carry.
	size := func() {
		dur, capStream, rampMax = c.Duration, 0, 0
		est = c.estimate(p, t)
		switch {
		case sz.top > 0:
			rampMax = sz.top
			est = rampBytes(c.UDP.Start, sz.top, c.UDP.Step)
		case sz.dur > 0:
			dur = sz.dur
			est = est * uint64((dur + c.Omit).Seconds()) / whole
		case sz.capped:
			capStream = c.allowanceOf(p) * 8 / whole / uint64(streams*dirs)
			est = capStream * uint64(streams*dirs) * whole / 8
		case sz.learn:
			est = min(est, c.allowanceOf(p))
		}
	}
	if p.Automatic() {
		c.mu.Lock()
		if st := c.peers[p.ID]; st != nil {
			sz = st.size[t]
		}
		c.mu.Unlock()
		size()
	} else if share := c.share(p); share > 0 && est > share {
		if t.kind == "udp-ramp" {
			rampMax = c.rampTop(share)
			if rampMax == 0 {
				c.warnShare(p, t, share)
				return outcome{skip: "budget", drop: true}
			}
			est = rampBytes(c.UDP.Start, rampMax, c.UDP.Step)
		} else {
			capStream = share * 8 / whole / uint64(streams*dirs)
			if capStream < minCapBits {
				c.warnShare(p, t, share)
				return outcome{skip: "budget", drop: true}
			}
			est = capStream * uint64(streams*dirs) * whole / 8
		}
	}
	if !c.allows(p, now, est) {
		if limit := c.budgetLimit(p); limit > 0 && est > limit {
			c.warnOversized(p, t, est)
			return outcome{skip: "budget", drop: true}
		}
		if p.Automatic() {
			// Wait for exactly as long as the window needs to have room, so the test is held back once, not every half hour.
			return outcome{skip: "budget", retry: max(c.waitFor(p, now, est), time.Minute)}
		}
		return outcome{skip: "budget", retry: 30 * time.Minute} // it fits when the window is empty: wait for it
	}
	if capStream > 0 || rampMax > 0 {
		c.warnBounded(p, t, est)
	}
	if c.busy(p, t, now) {
		return outcome{skip: "link_busy", retry: 10 * time.Minute}
	}
	if sz.learn {
		// A first run, short enough to cost little on any link, tells what a whole test carries; the test is sized with that.
		carried, out := c.learn(ctx, p, t)
		if out != (outcome{}) {
			return out
		}
		if !sleep(ctx, c.Pause) { // the server needs a moment to be ready for the next client
			return outcome{skip: "server_busy", retry: time.Minute} // stopping; nothing is reported
		}
		// Sized to what the allowance has left after the first run.
		allowance := c.allowanceOf(p)
		var ok bool
		if sz, _, ok = c.fitTest(p, t, allowance-min(carried, allowance)); !ok {
			c.warnAllowance(p, t, c.allowanceOf(p))
			return outcome{skip: "budget", drop: true}
		}
		c.mu.Lock()
		if st := c.peers[p.ID]; st != nil {
			st.size[t] = sz // a retry of this test is sized already
		}
		c.mu.Unlock()
		size()
		if !c.allows(p, c.Now(), est) {
			return outcome{skip: "budget", retry: max(c.waitFor(p, c.Now(), est), time.Minute)}
		}
	}
	if t.kind == "udp-ramp" {
		return c.runRamp(ctx, p, t, rampMax)
	}
	extra := []string{"-t", secs(dur)}
	if c.Omit > 0 {
		extra = append(extra, "-O", secs(c.Omit))
	}
	if streams > 1 {
		extra = append(extra, "-P", strconv.Itoa(streams))
	}
	// The rate cap of each stream: the one the user set, or what the byte budget allows, whichever is lower.
	effCap := c.TCPMaxRate
	if capStream > 0 && (effCap == 0 || capStream < effCap) {
		effCap = capStream
	}
	if effCap > 0 {
		extra = append(extra, "-b", strconv.FormatUint(effCap, 10))
	}
	if t.kind == "tcp-bidir" {
		extra = append(extra, "--bidir")
	}
	res, out := c.invoke(ctx, p, t, extra)
	if out != (outcome{}) {
		return out
	}
	var total uint64
	proto := "tcp"
	if t.kind == "tcp-bidir" {
		proto = "tcp_bidir"
	}
	for _, f := range res.Flows {
		total += f.Bytes
		// A test that came up to its cap says what the link carries at least, not what it carries.
		bounded := effCap > 0 && f.Bits >= boundedAt*float64(effCap)*float64(streams)
		c.Meter.ObserveTCP(p.ID, f, proto, res.Streams, t.dscp, bounded)
		if proto == "tcp" { // a test loading both ways reaches less in each, which says nothing about the link's capacity
			c.mu.Lock()
			c.setBits(p.ID, f.Direction, res.Streams, t.dscp, f.Bits, bounded)
			c.mu.Unlock()
		}
	}
	// The omitted start was sent too, at about the same rate.
	total += uint64(float64(total) * c.Omit.Seconds() / dur.Seconds())
	c.charge(p, c.Now(), total)
	if capStream == 0 { // a test the budget capped says nothing about what an uncapped one would carry
		c.mu.Lock()
		// What a test of the whole length would carry, also after a shorter one.
		c.lastBytes[t.key(p.ID)] = total * whole / uint64((dur + c.Omit).Seconds())
		c.mu.Unlock()
	}
	return outcome{}
}

// learn runs a one-second test of the shape of t and keeps what it carried as the size of a whole one. Its reading is not reported:
// one second is mostly TCP's slow start. It is counted in the bytes the tests carry and charged to the budget like any test.
func (c *Capacity) learn(ctx context.Context, p Peer, t capTest) (carried uint64, _ outcome) {
	streams, _ := c.shape(t)
	extra := []string{"-t", "1", "-O", "0"}
	if streams > 1 {
		extra = append(extra, "-P", strconv.Itoa(streams))
	}
	if t.kind == "tcp-bidir" {
		extra = append(extra, "--bidir")
	}
	res, out := c.invoke(ctx, p, t, extra)
	if out != (outcome{}) {
		return 0, out
	}
	var total uint64
	for _, f := range res.Flows {
		total += f.Bytes
		c.Meter.AddTestBytes(p.ID, f.Direction, f.Bytes)
	}
	c.charge(p, c.Now(), total)
	c.mu.Lock()
	c.lastBytes[t.key(p.ID)] = max(total, 1) * uint64((c.Duration + c.Omit).Seconds())
	if t.kind == "tcp" {
		for _, f := range res.Flows {
			c.setBits(p.ID, f.Direction, res.Streams, t.dscp, f.Bits, false)
		}
	}
	c.mu.Unlock()
	return total, outcome{}
}

func (c *Capacity) setBits(peer string, d Direction, streams, dscp int, bits float64, bounded bool) {
	if c.lastBits == nil {
		c.lastBits = map[string]float64{}
	}
	if c.lastBounded == nil {
		c.lastBounded = map[string]bool{}
	}
	k := fmt.Sprintf("%s|%s|%d|%d", peer, d, streams, dscp)
	c.lastBits[k] = bits
	c.lastBounded[k] = bounded
}

// shape is how many streams a test of this kind opens and how many directions it loads.
func (c *Capacity) shape(t capTest) (streams, dirs int) {
	streams, dirs = 1, 1
	if t.kind == "tcp-parallel" {
		streams = c.Parallel
	}
	if t.kind == "tcp-bidir" {
		dirs = 2
	}
	return streams, dirs
}

// reached is the highest TCP rate the latest tests reached towards (Forward) or from (Reverse) the peer, over the stream
// counts and DSCP classes; 0 before the first. A class that is policed does not lower it.
func (c *Capacity) reached(peer string, d Direction) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	var best float64
	for _, s := range []int{1, c.Parallel} {
		for _, ds := range c.DSCP {
			if v := c.lastBits[fmt.Sprintf("%s|%s|%d|%d", peer, d, s, ds)]; v > best {
				best = v
			}
		}
	}
	return best
}

func (c *Capacity) runRamp(ctx context.Context, p Peer, t capTest, top uint64) outcome {
	if top == 0 {
		top = c.UDP.Max
	}
	var steps []UDPStep
	var clean uint64
	var hasClean, saturated bool
	for rate := c.UDP.Start; ; {
		if len(steps) > 0 {
			if !sleep(ctx, c.Pause) {
				return outcome{skip: "server_busy", retry: time.Minute} // stopping; nothing is reported
			}
			if !c.allows(p, c.Now(), rate*uint64(c.UDP.Step.Seconds())/8) {
				return outcome{skip: "budget", retry: 30 * time.Minute}
			}
		}
		res, out := c.invoke(ctx, p, t, []string{"-u", "-b", strconv.FormatUint(rate, 10), "-t", secs(c.UDP.Step)})
		if out != (outcome{}) {
			return out
		}
		f, ok := res.Flow(t.dir)
		if !ok {
			return outcome{fail: "other"}
		}
		c.charge(p, c.Now(), f.Bytes)
		c.Meter.AddTestBytes(p.ID, t.dir, f.Bytes)
		steps = append(steps, UDPStep{Rate: rate, Loss: f.LossRatio(), Jitter: f.Jitter})
		if f.LossRatio() > c.UDP.Loss {
			saturated = true
			break
		}
		if offeredBits(f) < 0.9*float64(rate) {
			// The sender could not send what was asked, so nothing was lost because nothing was offered: this rate, and
			// any above it, say nothing about the link. The ramp ends at the last rate that was really delivered.
			break
		}
		clean, hasClean = rate, true
		if rate >= top {
			break
		}
		rate = min(rate*2, top)
	}
	c.Meter.ObserveUDPRamp(p.ID, t.dir, t.dscp, steps, clean, hasClean, saturated)
	return outcome{}
}

// offeredBits is the rate the sender really put on the link in a UDP step. The flow's own rate is the receiver's, which already
// leaves out what was lost, so with a loss threshold above 10% it can be below 90% of the asked rate only because the link
// dropped that much, and the step would be mistaken for one the sender could not keep up with.
func offeredBits(f IperfFlow) float64 {
	loss := f.LossRatio()
	if loss >= 1 {
		return 0
	}
	return f.Bits / (1 - loss)
}

// invoke runs one iperf3 client invocation towards the peer, trying its addresses (the one that answered last first), and
// turns what happened into a result or an outcome. extra are the arguments that make it the wanted test.
func (c *Capacity) invoke(ctx context.Context, p Peer, t capTest, extra []string) (IperfResult, outcome) {
	addrs := c.candidates(p)
	if len(addrs) == 0 {
		if noAddress(p) {
			return IperfResult{}, outcome{fail: ReasonNoKind}
		}
		return IperfResult{}, outcome{fail: "unreachable"}
	}
	au, err := c.authFor(p)
	if err != nil {
		if c.warnOnce(p.ID + "|auth") {
			c.Log.Warn("throughput tests towards the peer wait for iperf3 credentials: no test is run without them", "peer", p.ID, "reason", err.Error())
		}
		return IperfResult{}, outcome{fail: "auth"}
	}
	c.mu.Lock()
	delete(c.warned, p.ID+"|auth")
	c.mu.Unlock()
	var reason string
	sawBusy := false
	for _, a := range addrs {
		res, why := c.once(ctx, a, t, extra, au)
		switch why {
		case "":
			c.mu.Lock()
			if c.good == nil {
				c.good = map[string]string{}
			}
			c.good[p.ID] = a
			c.mu.Unlock()
			return res, outcome{}
		case "busy":
			sawBusy = true // another node may be free: every node runs its own server
			continue
		}
		reason = why
		if why == "auth" || ctx.Err() != nil {
			break
		}
	}
	if sawBusy && (reason == "" || reason == "timeout" || reason == "refused" || reason == "unreachable") {
		return IperfResult{}, outcome{skip: "server_busy", retry: time.Duration(60+rand.IntN(120)) * time.Second}
	}
	c.Log.Debug("throughput test failed", "peer", p.ID, "test", t.kind, "direction", t.dir, "reason", reason)
	return IperfResult{}, outcome{fail: reason}
}

func (c *Capacity) candidates(p Peer) []string {
	c.mu.Lock()
	last := c.good[p.ID]
	c.mu.Unlock()
	return orderAddresses(p, last, 5)
}

// once runs the client once towards addr.
func (c *Capacity) once(ctx context.Context, addr string, t capTest, extra []string, au iperfAuth) (IperfResult, string) {
	ip, err := netip.ParseAddr(addr)
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() {
		return IperfResult{}, "other"
	}
	args := []string{"-c", ip.String(), "-p", strconv.Itoa(c.Port), "-J", "-i", "0", "--connect-timeout", "5000"}
	if t.dir == Reverse {
		args = append(args, "-R")
	}
	if t.dscp > 0 {
		args = append(args, "--dscp", strconv.Itoa(t.dscp))
	}
	var env []string
	if au.user != "" {
		args = append(args, "--username", au.user, "--rsa-public-key-path", au.keyFile)
		env = append(env, "IPERF3_PASSWORD="+au.password.Reveal())
	}
	args = append(args, extra...)
	release, err := c.Gate.Acquire(ctx)
	if err != nil {
		return IperfResult{}, "timeout"
	}
	defer release()
	defer c.Testing.Enter()()
	cctx, cancel := context.WithTimeout(ctx, c.Duration+c.Omit+c.UDP.Step+30*time.Second)
	defer cancel()
	out, errOut, err := c.Exec(cctx, env, c.Bin, args...)
	// iperf3 prints its JSON, error included, even when it exits 1.
	res, perr := ParseIperf(bytes.NewReader(out), t.dir)
	if perr == nil {
		return res, ""
	}
	var ie *IperfError
	switch {
	case errors.As(perr, &ie):
		return IperfResult{}, classifyIperf(ie.Msg, false)
	case err != nil:
		return IperfResult{}, classifyIperf(string(errOut)+string(out), cctx.Err() != nil)
	}
	return IperfResult{}, "other"
}

func classifyIperf(msg string, timedOut bool) string {
	if strings.Contains(strings.ToLower(msg), "server is busy") {
		return "busy"
	}
	return classify(msg, timedOut)
}

func secs(d time.Duration) string { return strconv.Itoa(int(d.Seconds())) }

// estimate is what a test is expected to carry, for the budget: what the same test carried last time, or, before there is
// any, a guess from the rate cap (or 1 Gbit/s).
func (c *Capacity) estimate(p Peer, t capTest) uint64 {
	if t.kind == "udp-ramp" {
		return rampBytes(c.UDP.Start, c.UDP.Max, c.UDP.Step)
	}
	c.mu.Lock()
	last := c.lastBytes[t.key(p.ID)]
	// A test the link limited measured the rate a timed test would carry for its whole length.
	key := fmt.Sprintf("%s|%s|%d|%d", p.ID, t.dir, c.streamsOf(t), t.dscp)
	bits := c.lastBits[key]
	if c.lastBounded[key] {
		bits = 0 // it came up to its cap: the link carries more, by an amount nobody knows
	}
	c.mu.Unlock()
	if last > 0 {
		return last
	}
	if bits == 0 && t.kind != "tcp" {
		// A test of streams or both ways that has not run yet: the single-stream tests say what the link reached.
		bits = c.reached(p.ID, t.dir)
	}
	rate := uint64(1e9)
	if bits > 0 {
		rate = uint64(bits)
	}
	if c.TCPMaxRate > 0 {
		rate = min(rate, c.TCPMaxRate)
		if bits == 0 {
			rate = c.TCPMaxRate
		}
		if t.kind == "tcp-parallel" {
			rate *= uint64(c.Parallel)
		}
	}
	est := rate * uint64((c.Duration + c.Omit).Seconds()) / 8
	if t.kind == "tcp-bidir" {
		est *= 2
	}
	return est
}

func (c *Capacity) streamsOf(t capTest) int {
	if t.kind == "tcp-parallel" {
		return c.Parallel
	}
	return 1
}

// rampBytes is what a UDP ramp carries when every rung is held for step: start, doubling, up to top.
func rampBytes(start, top uint64, step time.Duration) uint64 {
	var sum uint64
	for r := start; ; r = min(r*2, top) {
		sum += r * uint64(step.Seconds()) / 8
		if r >= top {
			return sum
		}
	}
}

// rampTop is the highest top rate whose whole ramp fits share bytes, at most the configured top; 0 when even the first rung does not.
func (c *Capacity) rampTop(share uint64) uint64 {
	if rampBytes(c.UDP.Start, c.UDP.Start, c.UDP.Step) > share {
		return 0
	}
	top := c.UDP.Max
	for top > c.UDP.Start && rampBytes(c.UDP.Start, top, c.UDP.Step) > share {
		top = max(top/2, c.UDP.Start)
	}
	return top
}

// allowanceOf is the most the tests towards a peer may carry in a window: its own daily allowance, or the cluster's budget when
// that is smaller; 0 for none.
func (c *Capacity) allowanceOf(p Peer) uint64 {
	limit := c.ledger.Limit
	if p.ThroughputBudget > 0 && (limit == 0 || p.ThroughputBudget < limit) {
		limit = p.ThroughputBudget
	}
	return limit
}

// known says whether what a test carries is known, from a test that ran before: otherwise its size is a guess.
func (c *Capacity) known(p Peer, t capTest) bool {
	if t.kind == "udp-ramp" {
		return true // its rungs are fixed
	}
	c.mu.Lock()
	last := c.lastBytes[t.key(p.ID)]
	bits := c.lastBits[fmt.Sprintf("%s|%s|%d|%d", p.ID, t.dir, c.streamsOf(t), t.dscp)]
	c.mu.Unlock()
	return last > 0 || bits > 0 || c.reached(p.ID, t.dir) > 0
}

// fitTest sizes one test of an automatic peer to the whole allowance: as it is when that pays for it, a shorter run when it does
// not, and a capped one when not even the shortest does. A test whose size is not yet known is left to be sized by a first,
// one-second run (learn). cost is what it is expected to carry; ok is false for a test no allowance this small can pay for.
func (c *Capacity) fitTest(p Peer, t capTest, allowance uint64) (sz testSize, cost uint64, ok bool) {
	if t.kind == "udp-ramp" {
		cost = rampBytes(c.UDP.Start, c.UDP.Max, c.UDP.Step)
		if cost <= allowance {
			return sz, cost, true
		}
		top := c.rampTop(allowance)
		if top == 0 {
			return sz, 0, false
		}
		return testSize{top: top}, rampBytes(c.UDP.Start, top, c.UDP.Step), true
	}
	full := max(c.estimate(p, t), 1)
	whole := uint64((c.Duration + c.Omit).Seconds())
	streams, dirs := c.shape(t)
	if allowance*8/whole/uint64(streams*dirs) < minCapBits {
		return sz, 0, false // not enough for even the slowest reading worth having
	}
	at := func(d time.Duration) uint64 { return full * uint64((d + c.Omit).Seconds()) / whole }
	known := c.known(p, t)
	if !known && allowance >= learnBytes {
		return testSize{learn: true}, min(full, allowance), true
	}
	d := c.Duration
	switch {
	case !known:
		d = 0 // an allowance that cannot pay for a first run either: the test is capped to it
	case full > allowance:
		d = time.Duration(int64(allowance*whole/full)-int64(c.Omit.Seconds())) * time.Second
	}
	if d >= minAutoDuration {
		if cost = at(d); cost <= allowance {
			if d == c.Duration {
				return sz, cost, true
			}
			return testSize{dur: d}, cost, true
		}
	}
	return testSize{capped: true}, allowance, true
}

// autoCycle plans the next cycle of an automatic peer: the tests of the plan from cursor on, as many as the allowance pays for
// (the first one always, shortened if need be), each sized by fitTest. The ones that did not fit come first the next time, so
// when the allowance is smaller than the whole plan every test still comes round, and it is the allowance, not a schedule,
// that says how often. It returns the place the next cycle starts at.
func (c *Capacity) autoCycle(p Peer, cursor int) (queue []capTest, size map[capTest]testSize, next int) {
	allowance := c.allowanceOf(p)
	plan := c.plan()
	size = map[capTest]testSize{}
	var sum uint64
	taken := 0
	for ; taken < len(plan); taken++ {
		t := plan[(cursor+taken)%len(plan)]
		sz, cost, ok := c.fitTest(p, t, allowance)
		if !ok {
			c.warnAllowance(p, t, allowance)
			continue
		}
		if len(queue) > 0 && sum+cost > allowance {
			break
		}
		queue = append(queue, t)
		size[t] = sz
		sum += cost
	}
	return queue, size, (cursor + taken) % len(plan)
}

// warnAllowance says once that a test is left out because the daily allowance is too small for any run of it.
func (c *Capacity) warnAllowance(p Peer, t capTest, allowance uint64) {
	if c.warnOnce("allowance|" + t.key(p.ID)) {
		c.Log.Warn("a throughput test is left out: the daily allowance is too small to pay for even the shortest run of it; raise the allowance",
			"peer", p.ID, "test", t.kind, "direction", t.dir, "allowance", allowance)
	}
}

// share is what one test of the plan may carry: the byte budget divided over the tests of a cycle and the cycles a window
// holds. 0 when nothing limits it.
func (c *Capacity) share(p Peer) uint64 {
	limit := c.budgetLimit(p)
	if limit == 0 {
		return 0
	}
	return limit / c.testsPerWindow(p)
}

// testsPerWindow is how many tests the byte budget of a window has to pay for: the tests of a cycle times the cycles a window holds.
func (c *Capacity) testsPerWindow(p Peer) uint64 {
	every := c.Every
	if p.ThroughputEvery > 0 {
		every = p.ThroughputEvery
	}
	cycles := uint64(1)
	if every > 0 && c.BudgetWindow > every {
		cycles = uint64((c.BudgetWindow + every - 1) / every)
	}
	return max(uint64(len(c.plan()))*cycles, 1)
}

// warnShare says once that a test is left out because its share of the byte budget is too small for a reading: the tests are run
// so often that the budget, divided over them, pays for almost nothing each. It is not that one test is larger than the budget.
func (c *Capacity) warnShare(p Peer, t capTest, share uint64) {
	k := "share|" + t.key(p.ID)
	if !c.warnOnce(k) {
		return
	}
	c.Log.Warn("a throughput test is left out: the byte budget is shared by too many tests, so each may carry too little for a reading; run them less often or raise the budget",
		"peer", p.ID, "test", t.kind, "direction", t.dir, "tests_per_window", c.testsPerWindow(p), "budget", c.budgetLimit(p), "bytes_per_test", share)
}

// warnBounded says once that a test is sized down to its share of the byte budget.
func (c *Capacity) warnBounded(p Peer, t capTest, bytes uint64) {
	k := "bounded|" + t.key(p.ID)
	c.mu.Lock()
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	done := c.warned[k]
	c.warned[k] = true
	c.mu.Unlock()
	if !done {
		c.Log.Info("a throughput test is capped to its share of the byte budget; if it reaches the cap its reading is a floor (bounded), not what the link carries", "peer", p.ID, "test", t.kind, "direction", t.dir, "bytes", bytes)
	}
}

const (
	// minCapBits is the lowest rate cap worth a reading: a share that pays for less leaves the test out of the cycle.
	minCapBits = 1e6
	// boundedAt is the fraction of its cap a test must reach to count as having come up to it.
	boundedAt = 0.9
)

// budgetLimit is the smallest byte budget that applies to a peer (the cluster's, or its own), 0 for none.
func (c *Capacity) budgetLimit(p Peer) uint64 {
	limit := c.ledger.Limit
	if pl := c.pairLedger(p); pl != nil && (limit == 0 || pl.Limit < limit) {
		limit = pl.Limit
	}
	return limit
}

// warnOversized says once that a test can never run because it would not fit the budget even when the window is empty.
func (c *Capacity) warnOversized(p Peer, t capTest, est uint64) {
	limit := c.budgetLimit(p)
	if limit == 0 || est <= limit {
		return
	}
	k := t.key(p.ID)
	c.mu.Lock()
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	done := c.warned[k]
	c.warned[k] = true
	c.mu.Unlock()
	if !done {
		c.Log.Warn("a throughput test is larger than the whole byte budget and will never run; raise the budget or cap the rate", "peer", p.ID, "test", t.kind, "direction", t.dir, "expected_bytes", est, "budget", limit)
	}
}

// pairLedger is the daily budget of one peer, or nil when the server set none for it. It is kept between calls so that
// what was carried stays counted; a changed limit takes effect at once.
func (c *Capacity) pairLedger(p Peer) *Ledger {
	c.mu.Lock()
	defer c.mu.Unlock()
	if p.ThroughputBudget == 0 {
		delete(c.pairs, p.ID)
		return nil
	}
	if c.pairs == nil {
		c.pairs = map[string]*Ledger{}
	}
	l := c.pairs[p.ID]
	if l == nil {
		l = &Ledger{Window: c.BudgetWindow}
		c.pairs[p.ID] = l
	}
	l.mu.Lock()
	l.Limit = p.ThroughputBudget
	l.mu.Unlock()
	return l
}

// allows says whether a test expected to carry est bytes fits both the cluster's budget and the peer's own.
func (c *Capacity) allows(p Peer, now time.Time, est uint64) bool {
	if !c.ledger.Allows(now, est) {
		return false
	}
	if pl := c.pairLedger(p); pl != nil {
		return pl.Allows(now, est)
	}
	return true
}

// waitFor is how long until a test expected to carry est bytes fits both the cluster's budget and the peer's own (0: no telling).
func (c *Capacity) waitFor(p Peer, now time.Time, est uint64) time.Duration {
	w := c.ledger.Wait(now, est)
	if pl := c.pairLedger(p); pl != nil {
		w = max(w, pl.Wait(now, est))
	}
	return w
}

func (c *Capacity) charge(p Peer, now time.Time, n uint64) {
	c.ledger.Charge(now, n)
	if pl := c.pairLedger(p); pl != nil {
		pl.Charge(now, n)
	}
	c.mu.Lock()
	if st := c.peers[p.ID]; st != nil {
		st.cycleBytes += n
	}
	c.mu.Unlock()
}

// busy says whether the link is carrying real traffic in a direction this test would load.
func (c *Capacity) busy(p Peer, t capTest, now time.Time) bool {
	if c.Load == nil || (c.BusyFraction == 0 && c.SkipAbove == 0) {
		return false
	}
	lr, ok := c.Load.Rate(p.ID, now, 3*time.Minute)
	if !ok {
		return false
	}
	dirs := []Direction{t.dir}
	if t.kind == "tcp-bidir" {
		dirs = []Direction{Forward, Reverse}
	}
	for _, d := range dirs {
		rate := lr.Sent
		if d == Reverse {
			rate = lr.Received
		}
		if c.SkipAbove > 0 && rate > float64(c.SkipAbove) {
			return true
		}
		if r := c.reached(p.ID, d); c.BusyFraction > 0 && r > 0 && rate > c.BusyFraction*r {
			return true
		}
	}
	return false
}

// ReasonNoKind is the failure reason of a measurement that is pinned to one kind of address (Peer.Via) towards a peer that
// has none of that kind. It is counted wherever the other failure reasons are, and there is no fallback to another kind: the
// pin says which path is to be measured.
const ReasonNoKind = "no_such_kind"

// usableAddresses are the addresses of p an active measurement may use: all of them, or, when the pair is pinned, those of the
// kind it is pinned to.
func usableAddresses(p Peer) []netip.Addr {
	if p.Via == "" {
		return p.Addresses
	}
	var out []netip.Addr
	for _, a := range p.Addresses {
		if Class(a) == p.Via {
			out = append(out, a)
		}
	}
	return out
}

// noAddress says p is pinned to a kind of address it does not have, so no measurement towards it can be made.
func noAddress(p Peer) bool { return p.Via != "" && len(usableAddresses(p)) == 0 }

// rankAddresses splits the usable addresses of p into the order every active measurement follows: first the ones the real
// traffic is seen using (p.Preferred, busiest first), then the one that answered last, then the others in the order of the
// peer's list. A peer without a preference has only the last two.
func rankAddresses(p Peer, last string) (preferred []string, answered string, rest []string) {
	pool := usableAddresses(p)
	taken := map[string]bool{}
	for _, a := range p.Preferred {
		if s := a.String(); slices.Contains(pool, a) && !taken[s] {
			taken[s] = true
			preferred = append(preferred, s)
		}
	}
	for _, a := range pool {
		switch s := a.String(); {
		case taken[s]:
		case s == last:
			answered = s
		default:
			rest = append(rest, s)
		}
	}
	return preferred, answered, rest
}

// orderAddresses are the addresses an active measurement tries towards a peer, in order: those the traffic uses, the one that
// answered last, then the others in random order, at most max of the preferred and the others together (and the one that
// answered last besides). A pair pinned to a kind of address (p.Via) is offered only addresses of that kind, none if the peer
// has none: the caller says so (noAddress) instead of measuring another path. A peer with no preference gets what it always
// did: the one that answered last, then up to max others at random.
func orderAddresses(p Peer, last string, max int) []string {
	preferred, answered, rest := rankAddresses(p, last)
	if len(preferred) > max {
		preferred = preferred[:max]
	}
	rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	if room := max - len(preferred); len(rest) > room {
		rest = rest[:room]
	}
	out := preferred
	if answered != "" {
		out = append(out, answered)
	}
	return append(out, rest...)
}

// traceTarget is the address the route to p is traced towards: the first of the order above, but with the others in a fixed
// order, so that the target does not change from one trace to the next and a route change is a real one. "" if there is none.
func traceTarget(p Peer, last string) string {
	preferred, answered, rest := rankAddresses(p, last)
	for _, s := range append(append(preferred, answered), rest...) {
		if s != "" {
			return s
		}
	}
	return ""
}

// iperfAuth is what one iperf3 client invocation authenticates with; zero is no authentication.
type iperfAuth struct {
	user, keyFile string
	password      Secret
}

// authFor works out the credentials for the tests towards a peer: the install's own Secret when it has one, else what the server
// issued for this peer, else (only when the install does not let the server issue them) none. An install that lets the server
// issue them never tests a peer it holds none for.
func (c *Capacity) authFor(p Peer) (iperfAuth, error) {
	if c.User != "" {
		return iperfAuth{user: c.User, keyFile: c.KeyFile, password: Secret(c.password)}, nil
	}
	if p.Iperf == nil {
		if c.AutoAuth {
			return iperfAuth{}, errors.New("the server has not issued any yet")
		}
		return iperfAuth{}, nil
	}
	f, err := c.keyFile(p.Iperf.PublicKeyPEM)
	if err != nil {
		return iperfAuth{}, errors.New("the public key could not be prepared")
	}
	return iperfAuth{user: p.Iperf.Username, keyFile: f, password: p.Iperf.Password}, nil
}

// keyFile writes a peer's public key where iperf3 can read it (once per distinct key) and returns the path.
func (c *Capacity) keyFile(pemKey string) (string, error) {
	sum := sha256.Sum256([]byte(pemKey))
	id := hex.EncodeToString(sum[:8])
	c.mu.Lock()
	defer c.mu.Unlock()
	if f, ok := c.keyFiles[id]; ok {
		if _, err := os.Stat(f); err == nil {
			return f, nil
		}
	}
	if c.KeyDir == "" {
		d, err := os.MkdirTemp("", "iperf-keys-")
		if err != nil {
			return "", err
		}
		c.KeyDir = d
	}
	f := filepath.Join(c.KeyDir, id+".pem")
	if err := os.WriteFile(f, []byte(pemKey), 0o600); err != nil {
		return "", err
	}
	if c.keyFiles == nil {
		c.keyFiles = map[string]string{}
	}
	c.keyFiles[id] = f
	return f, nil
}

// warnOnce is true the first time it is asked about key (until the key is cleared).
func (c *Capacity) warnOnce(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.warned == nil {
		c.warned = map[string]bool{}
	}
	done := c.warned[key]
	c.warned[key] = true
	return !done
}
