package link

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// connectBuckets are the upper bounds, in seconds, of the connect-time histogram.
var connectBuckets = rttBuckets

// DialFunc opens a connection. It exists so tests can stand in for the network.
type DialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// TCPConnect is the baseline that needs nothing from the network but an open port: every Every it opens a TCP connection to each
// peer's responder (the port every cluster's link responder already listens on), times the handshake and closes it again. One SYN
// and its answer per peer, no data. The time is a round trip as the TCP stack sees it, and a failure is counted by its reason.
//
// It does not depend on ICMP, which many networks filter, and not on the one-way probe's gate, because it sends a handful of
// packets. It does keep out of the way of a throughput test (Testing): a handshake that queues behind a saturated link measures
// the load, not the link, and this series is the baseline the loaded round trip is compared with. It does depend on the responder being reachable, so it cannot tell a dead responder from a dead link by itself: a
// refusal means the host is up and the responder is not, a timeout means the host or the path is down, and the one-way probe says
// the rest.
type TCPConnect struct {
	Meter *Meter
	Log   *slog.Logger

	// Port is the responder's control port.
	Port int
	// Every is how often each peer is tried and Timeout how long one connection attempt may take (default 15s and 3s).
	Every, Timeout time.Duration
	// Stagger is how long a round waits for an address before it starts on the next one (default 250ms): the addresses of a peer
	// are tried together, in the order of their likelihood, and the first to answer is the answer.
	Stagger time.Duration
	Dial    DialFunc
	// Testing, if set, is on while a throughput test loads a link (Capacity.Testing). A round trip taken then, or overlapping
	// one, is not recorded at all: it counts neither as a sample nor as a failure.
	Testing *Flag

	mu     sync.Mutex
	good   map[string]string // peer -> the address that last answered
	failed map[string]string // peer -> the reason its last round failed, for the log to say only a change
	absent absence           // peer -> since when it has not been in the peer list (see peerGrace)
	slow   bool              // the last round took longer than Every
}

// Defaults fills what was left unset.
func (t *TCPConnect) Defaults() {
	if t.Port == 0 {
		t.Port = 861
	}
	if t.Every == 0 {
		t.Every = 15 * time.Second
	}
	if t.Timeout == 0 {
		t.Timeout = 3 * time.Second
	}
	if t.Stagger == 0 {
		t.Stagger = 250 * time.Millisecond
	}
	if t.Dial == nil {
		t.Dial = (&net.Dialer{}).DialContext
	}
	if t.Log == nil {
		t.Log = slog.Default()
	}
}

// Validate checks the settings so a bad value fails at start-up.
func (t *TCPConnect) Validate() error {
	if t.Port < 1 || t.Port > 65535 {
		return fmt.Errorf("invalid responder port %d", t.Port)
	}
	if t.Every < 5*time.Second {
		return fmt.Errorf("timing connections more often than every 5s is not supported, got %s", t.Every)
	}
	if t.Timeout < 500*time.Millisecond || t.Timeout > 30*time.Second {
		return fmt.Errorf("the connection timeout must be between 500ms and 30s, got %s", t.Timeout)
	}
	if t.Timeout >= t.Every {
		return fmt.Errorf("the connection timeout %s must be shorter than the interval %s", t.Timeout, t.Every)
	}
	if t.Stagger < 0 || t.Stagger > t.Timeout {
		return fmt.Errorf("the delay between addresses must be between 0 and the connection timeout %s, got %s", t.Timeout, t.Stagger)
	}
	return nil
}

// Run times connections until ctx ends.
func (t *TCPConnect) Run(ctx context.Context) {
	t.Defaults()
	if err := t.Validate(); err != nil {
		t.Log.Error("link connect timing not started", "err", err)
		return
	}
	if !sleep(ctx, time.Duration(rand.Int64N(int64(t.Every)+1))) {
		return
	}
	for {
		started := time.Now()
		t.Round(ctx)
		t.noteRound(ctx, time.Since(started))
		if !sleep(ctx, max(t.Every-time.Since(started), 0)) {
			return
		}
	}
}

// noteRound says when rounds start to take longer than the interval (many peers that do not answer, each costing its timeouts),
// and when they stop: the samples then come less often than the interval says, and nothing exported shows it.
func (t *TCPConnect) noteRound(ctx context.Context, took time.Duration) {
	if ctx.Err() != nil {
		return
	}
	over := took > t.Every
	t.mu.Lock()
	changed := over != t.slow
	t.slow = over
	t.mu.Unlock()
	switch {
	case over && changed:
		t.Log.Warn("link connect timing round took longer than the interval, so peers are timed less often than that", "took", took.Round(time.Millisecond), "interval", t.Every)
	case !over && changed:
		t.Log.Info("link connect timing rounds fit the interval again", "took", took.Round(time.Millisecond), "interval", t.Every)
	}
}

// Round times one connection to every peer, a few at a time so that unreachable peers do not delay the others.
func (t *TCPConnect) Round(ctx context.Context) {
	t.Defaults()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	peers := t.Meter.Peers.List()
	t.forget(peers, time.Now())
	for _, p := range peers {
		if !p.Probed() {
			continue
		}
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			t.measure(ctx, p)
		}()
	}
	wg.Wait()
}

// forget drops what is remembered about the peers that have been gone from the list for peerGrace: the address that answered and
// the state the log compares with. Both are small, but they would otherwise outlive every peer that was ever listed.
func (t *TCPConnect) forget(peers []Peer, now time.Time) {
	live := make(map[string]bool, len(peers))
	for _, p := range peers {
		if p.Probed() {
			live[p.ID] = true
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	seen := map[string]bool{}
	for id := range t.good {
		seen[id] = true
	}
	for id := range t.failed {
		seen[id] = true
	}
	for id := range t.absent {
		seen[id] = true
	}
	for id := range seen {
		if t.absent.gone(id, live[id], now) {
			delete(t.good, id)
			delete(t.failed, id)
		}
	}
}

// noteResult logs what became of a round for a peer, at Warn when the way it fails changes (a new reason, or the first failure
// after it worked) and at Info when it works again, so a peer that stays unreachable is said once and not every few seconds.
func (t *TCPConnect) noteResult(peer, reason string, kv ...any) {
	t.mu.Lock()
	prev := t.failed[peer]
	if reason == "" {
		delete(t.failed, peer)
	} else {
		if t.failed == nil {
			t.failed = map[string]string{}
		}
		t.failed[peer] = reason
	}
	t.mu.Unlock()
	switch {
	case reason == prev:
		if reason != "" {
			t.Log.Debug("link connect timing failed", append([]any{"peer", peer, "reason", reason}, kv...)...)
		}
	case reason == "":
		t.Log.Info("link connect timing works again", "peer", peer)
	default:
		t.Log.Warn("link connect timing failed", append([]any{"peer", peer, "reason", reason}, kv...)...)
	}
}

// maxRaced is how many of a peer's addresses one round tries: a cluster has many node addresses, and a few of each kind and
// family are enough to find the one that answers.
const maxRaced = 6

// attempt is one connection to one address of a peer, and how it ended.
type attempt struct {
	addr string
	d    time.Duration
	why  string // "" if it opened
}

// measure tries the peer's addresses together and keeps the first that answers. They are started in the order of their
// likelihood (the ones the traffic uses, the one that answered last, then one of each kind and family, then the others), the next
// one when the one before it has failed or Stagger has passed, so an address that is up is not made to wait for the others and
// one that is silent does not hold the rest back. The first to open is the sample. Once it has, the addresses not yet tried are
// started at once and every one is waited for (at most Timeout), because what did not answer is as much a finding as what did: the
// kinds of address that did not are exported (see ObserveAddresses), so a peer that is reached over one path but not another can
// be told from one that is not reached at all. A round fails only when no address opened, and then by the reason of the first.
func (t *TCPConnect) measure(ctx context.Context, p Peer) {
	stamp := t.Testing.Stamp()
	if stamp.on {
		return // a throughput test is loading the link: no baseline can be taken now
	}
	t.mu.Lock()
	last := t.good[p.ID]
	t.mu.Unlock()
	if noAddress(p) {
		t.Meter.ConnectFailed(p.ID, ReasonNoKind)
		t.noteResult(p.ID, ReasonNoKind, "via", p.Via)
		return
	}
	addrs := raceAddresses(p, last, maxRaced)
	if len(addrs) == 0 {
		return
	}
	results := make(chan attempt, len(addrs))
	started := 0
	start := func() {
		a := addrs[started]
		started++
		go func() {
			d, why := t.once(ctx, a)
			results <- attempt{a, d, why}
		}()
	}
	var (
		done  []attempt
		won   *attempt
		first = true // the first answer is the one that decides which of the others are still worth a connection
	)
	timer := time.NewTimer(t.Stagger)
	defer timer.Stop()
	next := func() {
		start()
		timer.Reset(t.Stagger)
	}
	next()
	for len(done) < len(addrs) {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if started < len(addrs) {
				next()
			}
		case r := <-results:
			done = append(done, r)
			if r.why == "" && won == nil {
				if !t.Testing.Unchanged(stamp) {
					return // a test started, or ended, while this one was going: the figure is the loaded link's
				}
				won = &r
				t.mu.Lock()
				if t.good == nil {
					t.good = map[string]string{}
				}
				t.good[p.ID] = r.addr
				t.mu.Unlock()
				t.Meter.ObserveConnect(p.ID, r.d)
				t.Meter.NoteAnswered(p.ID, r.addr)
				t.noteResult(p.ID, "")
			}
			// Nothing is gained by waiting once an address has failed, or once one has answered.
			switch {
			case won != nil:
				if first {
					addrs = append(addrs[:started], unsurveyed(addrs[:started], addrs[started:], done)...)
					first = false
				}
				for started < len(addrs) {
					start()
				}
			case r.why != "" && started < len(addrs):
				next()
			}
		}
	}
	if !t.Testing.Unchanged(stamp) {
		return
	}
	t.Meter.ObserveAddresses(p.ID, kindsAnswered(done))
	if won == nil {
		reason := done[0].why
		for _, r := range done {
			if r.addr == addrs[0] {
				reason = r.why
			}
		}
		t.Meter.ConnectFailed(p.ID, reason)
		t.noteResult(p.ID, reason, "tried", len(addrs))
	}
}

// once times one connection and returns how long the handshake took, or why it failed.
func (t *TCPConnect) once(ctx context.Context, addr string) (time.Duration, string) {
	ip, err := netip.ParseAddr(addr)
	if err != nil || ip.IsUnspecified() || ip.IsMulticast() {
		return 0, "other"
	}
	cctx, cancel := context.WithTimeout(ctx, t.Timeout)
	defer cancel()
	start := time.Now()
	c, err := t.Dial(cctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(t.Port)))
	d := time.Since(start)
	if err != nil {
		return 0, connectReason(err)
	}
	// Closed at once, with a reset rather than a goodbye: nothing is left in TIME_WAIT on either side, and the responder is
	// not made to wait for a greeting that is never coming.
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetLinger(0)
	}
	_ = c.Close()
	return d, ""
}

// connectReason names why a connection did not open, with the same words the passive series use.
func connectReason(err error) string {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "reset"
	case errors.Is(err, syscall.ENETUNREACH), errors.Is(err, syscall.EHOSTUNREACH), errors.Is(err, syscall.ENETDOWN), errors.Is(err, syscall.EHOSTDOWN):
		return "unreachable"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, syscall.ETIMEDOUT):
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	return "other"
}

type connectStats struct {
	h        *hist
	failed   map[string]uint64
	lastOK   time.Time
	answered string          // the address that opened last, which the one-way probe tries first
	kinds    map[string]bool // kind of address -> whether one of that kind opened in the latest round
}

// unsurveyed picks, of the addresses not yet tried, those that can still teach something once one has answered: of each kind and
// family that has not answered yet, two addresses in all (a single silent one may be a node that is down and not a path that is
// closed), and none of the kinds that have, whose other addresses are the same path again.
func unsurveyed(tried, rest []string, done []attempt) []string {
	answered := map[string]bool{}
	for _, r := range done {
		if a, err := netip.ParseAddr(r.addr); err == nil && r.why == "" {
			answered[Class(a)] = true
		}
	}
	type form struct {
		class string
		v6    bool
	}
	seen := map[form]int{}
	for _, s := range tried {
		a, _ := netip.ParseAddr(s)
		seen[form{Class(a), a.Unmap().Is6()}]++
	}
	var out []string
	for _, s := range rest {
		a, err := netip.ParseAddr(s)
		if err != nil {
			continue
		}
		f := form{Class(a), a.Unmap().Is6()}
		if answered[f.class] || seen[f] >= 2 {
			continue
		}
		seen[f]++
		out = append(out, s)
	}
	return out
}

// kindsAnswered says, for each kind of address the round tried, whether one of that kind opened. An address that has no kind
// (see Class) says nothing.
func kindsAnswered(done []attempt) map[string]bool {
	out := map[string]bool{}
	for _, r := range done {
		a, err := netip.ParseAddr(r.addr)
		if err != nil || Class(a) == "" {
			continue
		}
		out[Class(a)] = out[Class(a)] || r.why == ""
	}
	return out
}

// NoteAnswered records the address of peer that last opened a connection, so the one-way probe, which tries addresses one after
// the other and gives up after a few that stay silent, begins with an address that is known to answer on the control port.
func (m *Meter) NoteAnswered(peer, addr string) {
	if m == nil || !m.knows(peer) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connect(peer).answered = addr
}

// Answered is the address of peer that last opened a connection, or "".
func (m *Meter) Answered(peer string) string {
	if m == nil {
		return ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if s := m.connects[peer]; s != nil {
		return s.answered
	}
	return ""
}

// ObserveAddresses records which kinds of address of peer opened a connection in the latest round and which did not. It
// replaces what an earlier round said: a kind the round did not try is no longer reported.
func (m *Meter) ObserveAddresses(peer string, kinds map[string]bool) {
	if m == nil || !m.knows(peer) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connect(peer).kinds = kinds
}

func (m *Meter) connect(peer string) *connectStats {
	if m.connects == nil {
		m.connects = map[string]*connectStats{}
	}
	s := m.connects[peer]
	if s == nil {
		s = &connectStats{h: newHist(connectBuckets), failed: map[string]uint64{}}
		m.connects[peer] = s
	}
	return s
}

// ObserveConnect records the time a connection to peer took to open.
func (m *Meter) ObserveConnect(peer string, d time.Duration) {
	if m == nil || !m.knows(peer) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.connect(peer)
	s.h.observe(d.Seconds())
	s.lastOK = m.now()
}

// connectFailReasons are the reasons a connect timing round can fail for: the kernel's, and a pin to a kind of address the peer
// does not have.
var connectFailReasons = append(append([]string(nil), failReasons...), ReasonNoKind)

// ConnectFailed counts a connection to peer that did not open, by reason.
func (m *Meter) ConnectFailed(peer, reason string) {
	if m == nil || !m.knows(peer) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connect(peer).failed[reason]++
}

// writeConnects renders the connect-timing series for the peers that are probed. Called with m.mu held.
func (m *Meter) writeConnects(w io.Writer, own string, peerIDs []string) {
	if !m.timingConnects {
		return
	}
	lbl := func(peer string, extra ...string) string {
		l := fmt.Sprintf(`cluster="%s",peer="%s"`, esc(own), esc(peer))
		for i := 0; i+1 < len(extra); i += 2 {
			l += fmt.Sprintf(`,%s="%s"`, extra[i], esc(extra[i+1]))
		}
		return l
	}
	head := func(name, typ, help string) { fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ) }
	const name = "ikhnos_link_tcp_connect_seconds"
	head(name, "histogram", "Time to open a TCP connection to the peer's link responder, from SYN to established: a round trip as the TCP stack sees it, sampled every few seconds without sending data.")
	for _, id := range peerIDs {
		h := newHist(connectBuckets)
		if s := m.connects[id]; s != nil {
			h = s.h
		}
		cum := uint64(0)
		for i, b := range h.bounds {
			cum += h.counts[i]
			fmt.Fprintf(w, "%s_bucket{%s} %d\n", name, lbl(id, "le", trimFloat(b)), cum)
		}
		cum += h.counts[len(h.bounds)]
		fmt.Fprintf(w, "%s_bucket{%s} %d\n", name, lbl(id, "le", "+Inf"), cum)
		fmt.Fprintf(w, "%s_sum{%s} %g\n%s_count{%s} %d\n", name, lbl(id), h.sum, name, lbl(id), h.n)
	}
	head("ikhnos_link_tcp_connect_failures_total", "counter", "Rounds in which no address of the peer's link responder opened a TCP connection, by the reason of the last attempt; a round where a later address answered counts nothing here. The loss ratio, per round, is this over this plus the histogram's count (whose samples may come from different nodes of the peer).")
	for _, id := range peerIDs {
		for _, r := range connectFailReasons {
			v := uint64(0)
			if s := m.connects[id]; s != nil {
				v = s.failed[r]
			}
			fmt.Fprintf(w, "ikhnos_link_tcp_connect_failures_total{%s} %d\n", lbl(id, "reason", r), v)
		}
	}
	head("ikhnos_link_tcp_connect_kind_up", "gauge", "Whether a connection to an address of this kind (mesh, private or public) of the peer's link responder opened in the latest round: 1 if one did, 0 if every address of the kind that was tried stayed silent or refused. Absent for a kind that was not tried.")
	for _, id := range peerIDs {
		s := m.connects[id]
		if s == nil {
			continue
		}
		for _, k := range []string{ClassMesh, ClassPrivate, ClassPublic} {
			if up, ok := s.kinds[k]; ok {
				v := 0
				if up {
					v = 1
				}
				fmt.Fprintf(w, "ikhnos_link_tcp_connect_kind_up{%s} %d\n", lbl(id, "kind", k), v)
			}
		}
	}
	head("ikhnos_link_tcp_connect_last_success_timestamp_seconds", "gauge", "When a connection to the peer's link responder last opened. Absent until the first time.")
	for _, id := range peerIDs {
		if s := m.connects[id]; s != nil && !s.lastOK.IsZero() {
			fmt.Fprintf(w, "ikhnos_link_tcp_connect_last_success_timestamp_seconds{%s} %d\n", lbl(id), s.lastOK.Unix())
		}
	}
}

// EnableConnects makes the meter render the connect-timing series. Without it none are written, so a cluster that does not
// time connections never reports a failure counter of zero that nothing measured.
func (m *Meter) EnableConnects() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.timingConnects = true
	m.mu.Unlock()
}
