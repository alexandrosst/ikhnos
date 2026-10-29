package linkhub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/link"
	"ikhnos/internal/probe"

	"google.golang.org/protobuf/encoding/protojson"
)

const (
	// flushEvery is how often the rows that arrived are sent: a node collector reports every ten seconds, so a cluster of a hundred nodes
	// would otherwise make ten requests a second.
	flushEvery = time.Second
	// beatEvery is how often an empty message goes out when there is nothing to send, so that a hub that was restarted is noticed (it answers
	// with no configuration) and the prober's last request is learned.
	beatEvery = 15 * time.Second
	// MaxQueuedRows bounds what is held for a hub that cannot be reached; beyond it the oldest rows are dropped and counted.
	MaxQueuedRows = 20000
	maxBackoff    = time.Minute
)

// Forwarder is the agent's side: it takes the rows the flow pipeline admitted, cuts them down and sends them to the hub, and keeps the hub told
// what the server's configuration says about the links. It is a link.RowSink. It never blocks the flow pipeline, never holds more than
// MaxQueuedRows, and a hub that is down costs the series of that time and nothing else.
type Forwarder struct {
	url     string
	started time.Time
	secret  []byte
	hc      *http.Client
	filter  *link.RowFilter
	log     *slog.Logger
	now     func() time.Time

	wake chan struct{}

	mu sync.Mutex
	// pending are the rows not yet sent, apart by whether the report that carried them could count bytes.
	known, unknown []*ikhnosv1.RawFlow
	dropped        uint64
	sent           uint64
	cfg            *ikhnosv1.Config
	cfgDigest      string
	status         Status
	lastErr        string
	lastOK         time.Time

	reached atomic.Bool
	seq     atomic.Uint64
}

// NewForwarder sends to the hub at url (scheme, host and port) signed with secret. filter, if set, leaves out the rows that are not the links'
// (see link.RowFilter).
func NewForwarder(url string, secret []byte, filter *link.RowFilter, log *slog.Logger) *Forwarder {
	if log == nil {
		log = slog.Default()
	}
	return &Forwarder{url: url, started: time.Now(), secret: append([]byte(nil), secret...), hc: &http.Client{Timeout: 10 * time.Second}, filter: filter, log: log, now: time.Now, wake: make(chan struct{}, 1)}
}

func (f *Forwarder) poke() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// ObserveRows is link.RowSink: the rows are filtered, narrowed and queued, and the loop sends them within a second.
func (f *Forwarder) ObserveRows(rows []*ikhnosv1.RawFlow, bytesKnown bool) {
	if f == nil || len(rows) == 0 {
		return
	}
	f.mu.Lock()
	none := f.cfg != nil && len(f.cfg.LinkPeers) == 0
	f.mu.Unlock()
	if none {
		return // no link is measured: a row would be looked up among no peers
	}
	kept := make([]*ikhnosv1.RawFlow, 0, len(rows))
	for _, r := range rows {
		if f.filter.Keep(r) {
			kept = append(kept, Narrow(r))
		}
	}
	if len(kept) == 0 {
		return
	}
	f.mu.Lock()
	if bytesKnown {
		f.known = append(f.known, kept...)
	} else {
		f.unknown = append(f.unknown, kept...)
	}
	f.trim()
	f.mu.Unlock()
}

// trim drops the oldest rows beyond the bound. Called with f.mu held.
func (f *Forwarder) trim() {
	for over := len(f.known) + len(f.unknown) - MaxQueuedRows; over > 0; {
		q := &f.known
		if len(f.unknown) > len(f.known) {
			q = &f.unknown
		}
		n := min(over, len(*q))
		*q = append([]*ikhnosv1.RawFlow(nil), (*q)[n:]...)
		f.dropped += uint64(n)
		over -= n
	}
}

// SetConfig gives the forwarder the configuration the server sent; only the slice the hub is meant to have is kept. It is sent at once and again
// whenever the hub says it holds another.
func (f *Forwarder) SetConfig(c *ikhnosv1.Config) {
	if f == nil {
		return
	}
	s := Slice(c)
	d := Digest(f.secret, s)
	f.mu.Lock()
	changed := f.cfgDigest != d
	f.cfg, f.cfgDigest = s, d
	f.mu.Unlock()
	if changed {
		f.poke()
	}
}

// Stats says how the forwarding is going, for the agent's diagnostics.
type Stats struct {
	// Reached: the last message was answered.
	Reached bool
	// Queued, Sent and Dropped are rows.
	Queued, Sent, Dropped uint64
	LastError             string
	LastOK                time.Time
	// Started is when the forwarder was made, so that "never reached" can be told from "not reached yet".
	Started time.Time
	Hub     Status
	// InSync: the hub says it holds the configuration the agent holds.
	InSync bool
}

// Stats is a snapshot.
func (f *Forwarder) Stats() Stats {
	if f == nil {
		return Stats{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return Stats{Reached: f.reached.Load(), Queued: uint64(len(f.known) + len(f.unknown)), Sent: f.sent, Dropped: f.dropped, LastError: f.lastErr, LastOK: f.lastOK, Started: f.started, Hub: f.status, InSync: f.cfg == nil || f.status.ConfigDigest == f.cfgDigest}
}

// ProberSeen is when the hub last saw the prober ask for the peers, as of the hub's last answer; the zero time if it never has, or the
// hub has not been reached.
func (f *Forwarder) ProberSeen() time.Time {
	if f == nil {
		return time.Time{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status.ProberSeen()
}

// Run sends until ctx ends.
func (f *Forwarder) Run(ctx context.Context) {
	tick := time.NewTicker(flushEvery)
	defer tick.Stop()
	var beat time.Time
	wait := time.Duration(0)
	var retryAt time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-f.wake:
		}
		now := f.now()
		if now.Before(retryAt) {
			continue
		}
		err := f.cycle(ctx, now.Sub(beat) >= beatEvery)
		if err != nil {
			f.fail(err)
			wait = min(max(2*wait, 2*time.Second), maxBackoff)
			retryAt = now.Add(wait)
			continue
		}
		beat, wait, retryAt = now, 0, time.Time{}
	}
}

func (f *Forwarder) fail(err error) {
	f.reached.Store(false)
	f.mu.Lock()
	first := f.lastErr == ""
	f.lastErr = err.Error()
	f.mu.Unlock()
	if first {
		f.log.Warn("the link hub cannot be reached; the rows wait for it", "err", err)
	}
}

// errNoConfig is the hub's answer to rows when it holds no configuration (it was just started): the rows are not lost, the configuration
// goes first and they follow.
var errNoConfig = errors.New("the link hub holds no configuration yet")

// cycle sends what there is: the configuration if the hub does not hold the one the agent holds, then the queued rows, and an empty message
// when beat says it is time to ask how the hub is. A hub that says it holds no configuration is told it first, at once.
func (f *Forwarder) cycle(ctx context.Context, beat bool) error {
	var err error
	for range 3 {
		if err = f.once(ctx, beat); !errors.Is(err, errNoConfig) {
			return err
		}
	}
	return err
}

func (f *Forwarder) once(ctx context.Context, beat bool) error {
	sent := false
	f.mu.Lock()
	need := f.cfg != nil && f.status.ConfigDigest != f.cfgDigest
	cfg := f.cfg
	f.mu.Unlock()
	if need {
		if err := f.postConfig(ctx, cfg); err != nil {
			return err
		}
		sent = true
	}
	for {
		known, rows := f.take()
		if len(rows) == 0 {
			break
		}
		if err := f.postRows(ctx, rows, known); err != nil {
			f.requeue(rows, known)
			return err
		}
		sent = true
	}
	if !sent && beat {
		return f.postRows(ctx, nil, true)
	}
	return nil
}

// take removes up to MaxRowsPerPost queued rows of one kind.
func (f *Forwarder) take() (bytesKnown bool, rows []*ikhnosv1.RawFlow) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := &f.known
	known := true
	if len(f.known) == 0 {
		q, known = &f.unknown, false
	}
	n := min(len(*q), MaxRowsPerPost)
	rows = (*q)[:n:n]
	*q = append([]*ikhnosv1.RawFlow(nil), (*q)[n:]...)
	return known, rows
}

// requeue puts rows back at the front after a failed send, within the bound.
func (f *Forwarder) requeue(rows []*ikhnosv1.RawFlow, bytesKnown bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if bytesKnown {
		f.known = append(append([]*ikhnosv1.RawFlow(nil), rows...), f.known...)
	} else {
		f.unknown = append(append([]*ikhnosv1.RawFlow(nil), rows...), f.unknown...)
	}
	f.trim()
}

func (f *Forwarder) postRows(ctx context.Context, rows []*ikhnosv1.RawFlow, bytesKnown bool) error {
	// Node carries a number that is different in every message, so that two messages sent within the same second are not the same signature:
	// the hub refuses a signature it has seen, and a retry of rows it had refused (no configuration yet) would otherwise be taken for a replay.
	body, err := protojson.Marshal(&ikhnosv1.FlowReport{Node: "m" + strconv.FormatUint(f.seq.Add(1), 10), BytesKnown: bytesKnown, Flows: rows})
	if err != nil {
		return err
	}
	st, err := f.post(ctx, PathRows, rowsContext, body)
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.sent += uint64(len(rows))
	f.mu.Unlock()
	f.learn(st)
	return nil
}

func (f *Forwarder) postConfig(ctx context.Context, c *ikhnosv1.Config) error {
	raw, err := protojson.Marshal(c)
	if err != nil {
		return err
	}
	plain, err := json.Marshal(configDoc{Version: Version, Config: raw})
	if err != nil {
		return err
	}
	defer clear(plain)
	ts := strconv.FormatInt(f.now().Unix(), 10)
	sealed, err := link.Seal(f.secret, configSeal, ts, plain)
	if err != nil {
		return err
	}
	st, err := f.postAt(ctx, PathConfig, configContext, sealed, ts)
	if err != nil {
		return err
	}
	f.learn(st)
	return nil
}

func (f *Forwarder) learn(st Status) {
	f.reached.Store(true)
	f.mu.Lock()
	f.status, f.lastErr, f.lastOK = st, "", f.now()
	f.mu.Unlock()
}

func (f *Forwarder) post(ctx context.Context, path, signContext string, body []byte) (Status, error) {
	return f.postAt(ctx, path, signContext, body, strconv.FormatInt(f.now().Unix(), 10))
}

func (f *Forwarder) postAt(ctx context.Context, path, signContext string, body []byte, ts string) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.url+path, bytes.NewReader(body))
	if err != nil {
		return Status{}, err
	}
	req.Header.Set(probe.HeaderNode, AgentNode)
	req.Header.Set(probe.HeaderTime, ts)
	req.Header.Set(probe.HeaderSig, probe.SignContext(signContext, f.secret, ts, AgentNode, body))
	resp, err := f.hc.Do(req)
	if err != nil {
		return Status{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusServiceUnavailable {
		// "I hold no configuration": the answer carries the status, so that it is known to be none.
		var st Status
		if json.NewDecoder(io.LimitReader(resp.Body, maxStatusBody)).Decode(&st) == nil {
			f.learn(st)
		}
		return Status{}, errNoConfig
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return Status{}, fmt.Errorf("the link hub answered %s", resp.Status)
	}
	var st Status
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxStatusBody)).Decode(&st); err != nil {
		return Status{}, fmt.Errorf("the link hub's answer is not a status: %w", err)
	}
	return st, nil
}
