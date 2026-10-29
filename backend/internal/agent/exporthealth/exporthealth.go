// Package exporthealth tells whether each telemetry destination is actually receiving data.
//
// The telemetry collectors (the chart's host DaemonSet and cluster Deployment) count what they send, per
// exporter and signal type, and serve those counters on their own pod address (the chart's telemetry.health).
// This package reads them from every collector pod on a timer, keeps the order of what it saw, and turns the
// counters into a state per route: waiting, exporting, silent or failing. It reads counters only. It never
// sees telemetry, and what it reads is not sent anywhere but in the agent's own Diagnostics.
//
// Why here and not on the server: only whoever reads the counters sees every reading in order against one
// clock, which is what "did anything go out recently" needs. A counter on its own says how much went out
// since the pod started, never when.
package exporthealth

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

const (
	// DefaultEvery is how often the counters are read.
	DefaultEvery = 30 * time.Second
	// DefaultWindow is how long ago something may have gone out and still count as "recently": a few
	// collection intervals, since the slowest signal here is read every minute.
	DefaultWindow = 3 * time.Minute
	// DefaultForget is how long a route that no collector reports any more is kept before it is dropped from the report: an exporter that was
	// removed or renamed would otherwise stay as a silent row until the agent restarts. It is long, because a collector that restarted serves
	// no series for a route until something goes through it again, and that must not look like a removed route.
	DefaultForget = time.Hour

	defaultTimeout = 3 * time.Second
	// maxBody caps what is read from one collector: its own self-metrics are a few hundred kilobytes at most.
	maxBody = 16 << 20
	// maxPods caps the pods read from one target, and parallel reads the requests in flight at once, so a
	// large cluster costs a bounded amount of the agent's time and memory.
	maxPods  = 512
	parallel = 8
)

// Config is what a Monitor needs. Only Targets is required.
type Config struct {
	// Targets are host:port names, each resolved to every address it has (the chart gives one headless
	// Service per collector, which resolves to one address per pod) and each address read at /metrics.
	Targets []string
	// Every is the time between readings (default DefaultEvery); Window how recent "recently" is (default
	// DefaultWindow); Timeout the limit on one request (default 3 s).
	Every, Window, Timeout time.Duration
	// Forget is how long a route that no collector reports is kept (default DefaultForget).
	Forget time.Duration
	// Lookup and Get replace name resolution and the HTTP read (tests). Now replaces the clock (tests).
	Lookup func(ctx context.Context, host string) ([]string, error)
	Get    func(ctx context.Context, url string) ([]byte, error)
	Now    func() time.Time
	Log    *slog.Logger
}

// ParseTargets splits a comma-separated list of host:port names, dropping blanks and repeats.
func ParseTargets(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

type route struct{ exporter, signal string }

// counts are the counters of one route. queued is not a counter but a gauge: how many items wait in the exporter's queue to be sent. An
// exporter retries for a long time before it counts a send as failed (that is what the chart asks of it), so a destination that cannot be
// reached shows first as a queue that does not empty, and the retries are the failures the counters do not yet show.
//
// capacity is how many items the exporter's queue holds at most, and dropped how many it refused because it was full (the data is gone, unlike a
// failed send, which is retried).
type counts struct{ sent, failed, queued, capacity, dropped uint64 }

// routeState is what is known about one route: the latest sum of its counters over the pods read, and when
// each was last seen to grow.
type routeState struct {
	counts
	lastSent, lastFailed time.Time
	// lastDropped is when the queue was last seen to refuse something. It is not a failure of the route for its state: a route that is still
	// sending and loses some is exporting, and the page says so beside the state.
	lastDropped time.Time
	// lastSeen is the last reading some collector reported the route in; a route not reported for Config.Forget is dropped.
	lastSeen time.Time
	// ready is set once a reading had an earlier one of the same pod to compare with. Before that a counter
	// above zero says only that something went out at some time, so no state is claimed from it.
	ready bool
}

// Monitor reads the collectors' counters. Report is safe to call from any goroutine.
type Monitor struct {
	cfg Config
	log *slog.Logger

	mu        sync.Mutex
	prev      map[string]map[route]counts // pod address -> its counters at the last reading
	routes    map[route]*routeState
	scrapedAt time.Time
	reached   int
	failed    int
	lastErr   string
	// tools is what the last reading found about each scrape job's targets (job -> instance -> state): gauges, so only the last reading counts.
	tools map[string]map[string]toolTarget
	// uptime is each collector pod's uptime (by its address) at the last reading that had one; an uptime lower than the one before is a restart.
	uptime      map[string]float64
	restarts    uint32
	lastRestart time.Time
}

// New returns a Monitor; call Run to start it.
func New(cfg Config) *Monitor {
	if cfg.Every <= 0 {
		cfg.Every = DefaultEvery
	}
	if cfg.Window <= 0 {
		cfg.Window = DefaultWindow
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.Forget <= 0 {
		cfg.Forget = DefaultForget
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	m := &Monitor{cfg: cfg, log: cfg.Log, prev: map[string]map[route]counts{}, routes: map[route]*routeState{}, uptime: map[string]float64{}}
	if cfg.Lookup == nil {
		m.cfg.Lookup = func(ctx context.Context, host string) ([]string, error) {
			return net.DefaultResolver.LookupHost(ctx, host)
		}
	}
	if cfg.Get == nil {
		client := &http.Client{Timeout: cfg.Timeout}
		m.cfg.Get = func(ctx context.Context, url string) ([]byte, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
			}
			return io.ReadAll(io.LimitReader(resp.Body, maxBody))
		}
	}
	return m
}

// Run reads the counters at once and then every Config.Every until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	m.Scrape(ctx)
	t := time.NewTicker(m.cfg.Every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Scrape(ctx)
		}
	}
}

// Scrape does one reading of every target.
func (m *Monitor) Scrape(ctx context.Context) {
	type read struct {
		addr   string
		routes map[route]counts
		tools  map[string]map[string]toolTarget
		uptime float64
		hasUp  bool
		err    error
	}
	var (
		reads    []read
		resolved = map[string]bool{}
		failed   int
		lastErr  string
	)
	note := func(err error) {
		failed++
		lastErr = clip(err.Error(), 200)
	}
	var addrs []string
	for _, target := range m.cfg.Targets {
		host, port, err := net.SplitHostPort(target)
		if err != nil {
			note(fmt.Errorf("target %q: %w", target, err))
			continue
		}
		ips := []string{host}
		if net.ParseIP(host) == nil {
			lctx, cancel := context.WithTimeout(ctx, m.cfg.Timeout)
			ips, err = m.cfg.Lookup(lctx, host)
			cancel()
			if err != nil {
				note(fmt.Errorf("find %s: %w", host, err))
				continue
			}
			if len(ips) == 0 {
				note(fmt.Errorf("find %s: no pods behind it", host))
				continue
			}
		}
		sort.Strings(ips)
		if len(ips) > maxPods {
			ips = ips[:maxPods]
		}
		for _, ip := range ips {
			a := net.JoinHostPort(ip, port)
			resolved[a] = true
			addrs = append(addrs, a)
		}
	}

	var (
		wg  sync.WaitGroup
		rmu sync.Mutex
		sem = make(chan struct{}, parallel)
	)
	for _, a := range addrs {
		wg.Add(1)
		sem <- struct{}{}
		go func(a string) {
			defer wg.Done()
			defer func() { <-sem }()
			rctx, cancel := context.WithTimeout(ctx, m.cfg.Timeout)
			defer cancel()
			body, err := m.cfg.Get(rctx, "http://"+a+"/metrics")
			var rs map[route]counts
			var ts map[string]map[string]toolTarget
			var up float64
			var hasUp bool
			if err == nil {
				rs, err = parse(strings.NewReader(string(body)))
				ts = parseTools(string(body))
				up, hasUp = parseUptime(string(body))
			}
			rmu.Lock()
			reads = append(reads, read{addr: a, routes: rs, tools: ts, uptime: up, hasUp: hasUp, err: err})
			rmu.Unlock()
		}(a)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return // shutting down: a half-finished reading would only look like failures
	}
	sort.Slice(reads, func(i, j int) bool { return reads[i].addr < reads[j].addr })

	now := m.cfg.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	// A pod that serves its counters on two ports (the cluster collector's own and its source health) is read twice and is one pod: it is
	// reached when any of its ports could be read, and failed only when none could. A port that failed beside one that answered is still
	// the last error, so that the page can say it, but it does not make a pod of the count.
	agg := map[route]counts{}
	seenNow := map[string]map[route]counts{}
	tools := map[string]map[string]toolTarget{}
	hostOK, hostBad := map[string]bool{}, map[string]bool{}
	upNow := map[string]float64{}
	for _, r := range reads {
		host, _, _ := net.SplitHostPort(r.addr)
		if r.err != nil {
			lastErr = clip(fmt.Errorf("read %s: %w", r.addr, r.err).Error(), 200)
			hostBad[host] = true
			continue
		}
		hostOK[host] = true
		if r.hasUp {
			upNow[host] = r.uptime
		}
		seenNow[r.addr] = r.routes
		for job, ts := range r.tools {
			if tools[job] == nil {
				tools[job] = map[string]toolTarget{}
			}
			for inst, t := range ts {
				tools[job][inst] = t
			}
		}
		before, known := m.prev[r.addr]
		for rt, c := range r.routes {
			a := agg[rt]
			a.sent += c.sent
			a.failed += c.failed
			a.queued += c.queued
			a.capacity += c.capacity
			a.dropped += c.dropped
			agg[rt] = a
			st := m.routes[rt]
			if st == nil {
				st = &routeState{}
				m.routes[rt] = st
			}
			st.lastSeen = now
			if !known {
				continue // the first reading of a pod is only a baseline to measure growth from
			}
			st.ready = true
			p := before[rt] // a series that is new since the last reading counts from zero
			if grew(p.sent, c.sent) {
				st.lastSent = now
			}
			if grew(p.failed, c.failed) {
				st.lastFailed = now
			}
			if grew(p.dropped, c.dropped) {
				st.lastDropped = now
			}
			// Items still waiting in the queue at two readings in a row with nothing sent in between: the destination is not taking them. The
			// exporter has not given up on them yet, so no failure is counted, but they are not on their way either.
			if c.queued > 0 && p.queued > 0 && !grew(p.sent, c.sent) {
				st.lastFailed = now
			}
		}
	}
	for rt, c := range agg {
		m.routes[rt].counts = c
	}
	reached := len(hostOK)
	for h := range hostBad {
		if !hostOK[h] {
			failed++
		}
	}
	// Routes that no collector has reported for a long time are gone (an exporter removed or renamed), not silent. Only judged on a reading
	// that reached a collector: with none reached, nothing says what the collectors report.
	if reached > 0 {
		for rt, st := range m.routes {
			if now.Sub(st.lastSeen) > m.cfg.Forget {
				delete(m.routes, rt)
			}
		}
	}
	// A pod whose uptime went down since the last reading started again at its own address: a crash or a kill, not a new pod.
	for h, up := range upNow {
		if before, ok := m.uptime[h]; ok && up+1 < before {
			m.restarts++
			m.lastRestart = now
		}
	}
	for h := range m.uptime {
		if _, ok := upNow[h]; !ok && !resolvedHost(resolved, h) {
			delete(m.uptime, h) // the pod is gone
		}
	}
	for h, up := range upNow {
		m.uptime[h] = up
	}
	// A pod that could not be read this time keeps its baseline while it still resolves; one that is gone does not.
	for a, c := range m.prev {
		if _, ok := seenNow[a]; !ok && resolved[a] {
			seenNow[a] = c
		}
	}
	m.prev = seenNow
	m.scrapedAt, m.reached, m.failed, m.lastErr = now, reached, failed, lastErr
	m.tools = tools
}

func resolvedHost(resolved map[string]bool, host string) bool {
	for a := range resolved {
		if h, _, err := net.SplitHostPort(a); err == nil && h == host {
			return true
		}
	}
	return false
}

// grew reports whether a counter went up since the last reading. A counter that went down was reset by a
// restart, and whatever it holds now is growth.
func grew(before, now uint64) bool {
	if now < before {
		return now > 0
	}
	return now > before
}

// Report is what the agent says about export health, or nil before the first reading has finished.
func (m *Monitor) Report() *ikhnosv1.ExportHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.scrapedAt.IsZero() {
		return nil
	}
	now := m.cfg.Now()
	out := &ikhnosv1.ExportHealth{
		ScrapedAt: timestamppb.New(m.scrapedAt), PodsReached: uint32(m.reached), PodsFailed: uint32(m.failed), LastError: m.lastErr,
		CollectorRestarts: m.restarts,
	}
	if !m.lastRestart.IsZero() {
		out.LastCollectorRestartAt = timestamppb.New(m.lastRestart)
	}
	keys := make([]route, 0, len(m.routes))
	for k := range m.routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].signal != keys[j].signal {
			return keys[i].signal < keys[j].signal
		}
		return keys[i].exporter < keys[j].exporter
	})
	for _, k := range keys {
		st := m.routes[k]
		if strings.HasPrefix(k.exporter, sourcePrefix) {
			recv := strings.TrimPrefix(k.exporter, sourcePrefix)
			sh := &ikhnosv1.SourceHealth{Receiver: recv, Signals: SourceReceivers[recv], State: m.sourceState(st, now), Accepted: st.sent, Failed: st.failed}
			if !st.lastSent.IsZero() {
				sh.LastAcceptedAt = timestamppb.New(st.lastSent)
			}
			if !st.lastFailed.IsZero() {
				sh.LastFailedAt = timestamppb.New(st.lastFailed)
			}
			out.Sources = append(out.Sources, sh)
			continue
		}
		if strings.HasPrefix(k.exporter, receiverPrefix) {
			rc := &ikhnosv1.ReceiverHealth{Signal: k.signal, Accepted: st.sent, Refused: st.failed}
			switch m.state(st, now) {
			case ikhnosv1.ExportRouteHealth_EXPORTING:
				rc.State = ikhnosv1.ReceiverHealth_RECEIVING
			case ikhnosv1.ExportRouteHealth_SILENT:
				rc.State = ikhnosv1.ReceiverHealth_SILENT
			case ikhnosv1.ExportRouteHealth_FAILING:
				rc.State = ikhnosv1.ReceiverHealth_REFUSING
			}
			if !st.lastSent.IsZero() {
				rc.LastAcceptedAt = timestamppb.New(st.lastSent)
			}
			if !st.lastFailed.IsZero() {
				rc.LastRefusedAt = timestamppb.New(st.lastFailed)
			}
			out.Receivers = append(out.Receivers, rc)
			continue
		}
		r := &ikhnosv1.ExportRouteHealth{Exporter: k.exporter, Signal: k.signal, State: m.state(st, now), Sent: st.sent, Failed: st.failed, Queued: st.queued, QueueCapacity: st.capacity, Dropped: st.dropped}
		if !st.lastDropped.IsZero() {
			r.LastDroppedAt = timestamppb.New(st.lastDropped)
		}
		if !st.lastSent.IsZero() {
			r.LastSentAt = timestamppb.New(st.lastSent)
		}
		if !st.lastFailed.IsZero() {
			r.LastFailedAt = timestamppb.New(st.lastFailed)
		}
		out.Routes = append(out.Routes, r)
	}
	out.Sources = append(out.Sources, m.toolSources()...)
	return out
}

func (m *Monitor) state(st *routeState, now time.Time) ikhnosv1.ExportRouteHealth_State {
	recent := func(t time.Time) bool { return !t.IsZero() && now.Sub(t) <= m.cfg.Window }
	switch {
	case recent(st.lastFailed) && !st.lastFailed.Before(st.lastSent):
		return ikhnosv1.ExportRouteHealth_FAILING
	case recent(st.lastSent):
		return ikhnosv1.ExportRouteHealth_EXPORTING
	case !st.ready:
		return ikhnosv1.ExportRouteHealth_WAITING
	case st.sent > 0 || st.failed > 0:
		return ikhnosv1.ExportRouteHealth_SILENT
	}
	return ikhnosv1.ExportRouteHealth_WAITING
}

// parse reads the collector's Prometheus text for the exporter counters. It accepts the counter names with
// or without the "_total" suffix (which collector releases have disagreed on), sums series that differ only in
// labels this package does not use (the address a gRPC exporter talks to, a URL path), and ignores everything
// else, including the "debug" exporter, which writes to the collector's own log and is nobody's destination.
func parse(r io.Reader) (map[route]counts, error) {
	out := map[route]counts{}
	caps := map[route]uint64{}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "otelcol_receiver_") {
			parseReceiver(line, out)
			continue
		}
		if strings.HasPrefix(line, "otelcol_scraper_errored_") {
			parseScraperErrors(line, out)
			continue
		}
		if !strings.HasPrefix(line, "otelcol_exporter_") {
			continue
		}
		name, labels, value, ok := splitSample(line)
		if !ok {
			continue
		}
		name = strings.TrimSuffix(strings.TrimPrefix(name, "otelcol_exporter_"), "_total")
		if name == "queue_size" {
			parseQueue(labels, value, out)
			continue
		}
		if name == "queue_capacity" {
			parseCapacity(labels, value, caps)
			continue
		}
		var failedCounter, droppedCounter bool
		switch {
		case strings.HasPrefix(name, "send_failed_"):
			failedCounter, name = true, strings.TrimPrefix(name, "send_failed_")
		case strings.HasPrefix(name, "enqueue_failed_"):
			droppedCounter, name = true, strings.TrimPrefix(name, "enqueue_failed_")
		case strings.HasPrefix(name, "sent_"):
			name = strings.TrimPrefix(name, "sent_")
		default:
			continue
		}
		var signal string
		switch name {
		case "spans":
			signal = "traces"
		case "metric_points":
			signal = "metrics"
		case "log_records":
			signal = "logs"
		default:
			continue
		}
		exp := labels["exporter"]
		if exp == "" || exp == "debug" || strings.HasPrefix(exp, "debug/") {
			continue
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			continue
		}
		k := route{exp, signal}
		c := out[k]
		switch {
		case failedCounter:
			c.failed += uint64(value)
		case droppedCounter:
			c.dropped += uint64(value)
		default:
			c.sent += uint64(value)
		}
		out[k] = c
	}
	// A queue's capacity says nothing on its own, so it is kept only for a route that has something else to say.
	for k, v := range caps {
		if c, ok := out[k]; ok {
			c.capacity = v
			out[k] = c
		}
	}
	return out, sc.Err()
}

// parseCapacity reads one otelcol_exporter_queue_capacity line: how many items an exporter's sending queue holds at most, per kind of data.
func parseCapacity(labels map[string]string, value float64, caps map[route]uint64) {
	exp := labels["exporter"]
	if exp == "" || exp == "debug" || strings.HasPrefix(exp, "debug/") || math.IsNaN(value) || math.IsInf(value, 0) || value < 1 {
		return
	}
	signal := labels["data_type"]
	if signal != "metrics" && signal != "logs" && signal != "traces" {
		return
	}
	caps[route{exp, signal}] += uint64(value)
}

// parseUptime reads otelcol_process_uptime, the seconds since the collector started. It is on the collector's own port only.
func parseUptime(body string) (float64, bool) {
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "otelcol_process_uptime") {
			continue
		}
		name, _, value, ok := splitSample(line)
		if !ok || (name != "otelcol_process_uptime" && name != "otelcol_process_uptime_seconds") || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			continue
		}
		return value, true
	}
	return 0, false
}

// parseQueue reads one otelcol_exporter_queue_size line: the items waiting in an exporter's sending queue, per kind of data.
func parseQueue(labels map[string]string, value float64, out map[route]counts) {
	exp := labels["exporter"]
	if exp == "" || exp == "debug" || strings.HasPrefix(exp, "debug/") || math.IsNaN(value) || math.IsInf(value, 0) || value < 1 {
		return // an empty queue is the usual case and says nothing: it does not make a route of its own
	}
	signal := labels["data_type"]
	if signal != "metrics" && signal != "logs" && signal != "traces" {
		return
	}
	k := route{exp, signal}
	c := out[k]
	c.queued += uint64(value)
	out[k] = c
}

// receiverPrefix marks a route that is a receiver's, not an exporter's: the counters of what the collectors' OTLP receiver took in, kept
// in the same table as the exporters' so that one history and one state rule serve both. Report splits them apart again.
const receiverPrefix = "receiver:"

// parseReceiver reads one otelcol_receiver_accepted_* or refused_* line. Only the OTLP receiver counts: it is the one applications push
// to, and the stage before the exporters. The collectors' other receivers (the Kubernetes watches, the scrapes of Kepler and the cluster
// links) are the agent's own and say nothing about whether an application is sending.
func parseReceiver(line string, out map[route]counts) {
	name, labels, value, ok := splitSample(line)
	if !ok {
		return
	}
	r := labels["receiver"]
	if _, ok := SourceReceivers[r]; ok {
		parseSource(r, name, value, out)
		return
	}
	if r != "otlp" && !strings.HasPrefix(r, "otlp/") {
		return
	}
	name = strings.TrimSuffix(strings.TrimPrefix(name, "otelcol_receiver_"), "_total")
	var refused bool
	switch {
	case strings.HasPrefix(name, "refused_"):
		refused, name = true, strings.TrimPrefix(name, "refused_")
	case strings.HasPrefix(name, "accepted_"):
		name = strings.TrimPrefix(name, "accepted_")
	default:
		return
	}
	var signal string
	switch name {
	case "spans":
		signal = "traces"
	case "metric_points":
		signal = "metrics"
	case "log_records":
		signal = "logs"
	default:
		return
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return
	}
	k := route{receiverPrefix + "otlp", signal}
	c := out[k]
	if refused {
		c.failed += uint64(value)
	} else {
		c.sent += uint64(value)
	}
	out[k] = c
}

// sourcePrefix marks a route that is the counters of a receiver reading a source of the cluster, kept in the same table as the rest.
const sourcePrefix = "source:"

// SourceReceivers are the collector receivers that read a source of the cluster itself, by the name the chart gives them, and the telemetry
// signals of the install each one reads for. A receiver that serves two signals (the kubelet's serves resource usage and node runtime) tells
// about both. The Prometheus scrapes (Kepler, DCGM, the cluster links) are not here: they share one receiver, and a scrape that fails still
// puts out its own up=0 point, so the counters of the receiver cannot tell a dead target from a live one.
var SourceReceivers = map[string][]string{
	"hostmetrics":        {"resourceUsage"},
	"kubelet_stats":      {"resourceUsage", "nodeRuntime"},
	"k8s_cluster":        {"kubernetesState"},
	"k8sobjects":         {"kubernetesEvents"},
	"filelog/containers": {"systemLogs"},
	"journald":           {"systemLogs"},
}

// dataType is the signal type a counter name ends in: spans, metric_points or log_records.
func dataType(name string) string {
	switch name {
	case "spans":
		return "traces"
	case "metric_points":
		return "metrics"
	case "log_records":
		return "logs"
	}
	return ""
}

// parseSource adds one accepted or refused counter of a source receiver. name is what follows otelcol_receiver_ without its _total.
func parseSource(receiver, name string, value float64, out map[route]counts) {
	name = strings.TrimSuffix(strings.TrimPrefix(name, "otelcol_receiver_"), "_total")
	var refused bool
	switch {
	case strings.HasPrefix(name, "refused_"):
		refused, name = true, strings.TrimPrefix(name, "refused_")
	case strings.HasPrefix(name, "accepted_"):
		name = strings.TrimPrefix(name, "accepted_")
	default:
		return
	}
	sig := dataType(name)
	if sig == "" || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return
	}
	k := route{sourcePrefix + receiver, sig}
	c := out[k]
	if refused {
		c.failed += uint64(value)
	} else {
		c.sent += uint64(value)
	}
	out[k] = c
}

// parseScraperErrors reads one otelcol_scraper_errored_* line: the points a scraper could not read, which for the receivers that scrape
// (the host and the kubelet) is where a failing read shows, since nothing is refused.
func parseScraperErrors(line string, out map[route]counts) {
	name, labels, value, ok := splitSample(line)
	if !ok {
		return
	}
	recv := labels["receiver"]
	if _, known := SourceReceivers[recv]; !known {
		return
	}
	sig := dataType(strings.TrimSuffix(strings.TrimPrefix(name, "otelcol_scraper_errored_"), "_total"))
	if sig == "" || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return
	}
	k := route{sourcePrefix + recv, sig}
	c := out[k]
	c.failed += uint64(value)
	out[k] = c
}

// sourceState is the state of a source receiver. It differs from a route's in one thing: reads that fail while others succeed are
// degraded, not failing, since a scraper that cannot read one thing (a process it may not see) still brings the rest.
func (m *Monitor) sourceState(st *routeState, now time.Time) ikhnosv1.SourceHealth_State {
	recent := func(t time.Time) bool { return !t.IsZero() && now.Sub(t) <= m.cfg.Window }
	switch {
	case recent(st.lastFailed) && !st.lastSent.Before(st.lastFailed):
		// Data came in as late as the failures did, so the failing reads are a part of what is read, not all of it.
		return ikhnosv1.SourceHealth_DEGRADED
	case recent(st.lastFailed):
		return ikhnosv1.SourceHealth_FAILING
	case recent(st.lastSent):
		return ikhnosv1.SourceHealth_RECEIVING
	case !st.ready:
		return ikhnosv1.SourceHealth_WAITING
	case st.sent > 0 || st.failed > 0:
		return ikhnosv1.SourceHealth_SILENT
	}
	return ikhnosv1.SourceHealth_WAITING
}

// ToolJobs are the scrape jobs of the cluster collector that read a tool of the cluster, by the job_name the chart gives them, and the signals of the
// install each one is for. The scrape of an application's own targets (telemetry.applicationMetrics) is not here: it is the owner's, not a tool.
var ToolJobs = map[string][]string{
	"kepler":                {"energy"},
	"energy-existing":       {"energy"},
	"dcgm-exporter":         {"accelerators"},
	"accelerators-existing": {"accelerators"},
	"cluster-links":         {"clusterLinks"},
	"cluster-links-probe":   {"clusterLinks"},
}

// quietWithoutFault are the jobs whose service is up with no series while there is nothing for it to report, which is not a fault: it is waiting.
// A tool that is up and serves nothing (Kepler, dcgm-exporter) is a fault, since it has something to measure from the moment it runs.
var quietWithoutFault = map[string]bool{"cluster-links": true, "cluster-links-probe": true}

// maxDown is how many nodes whose target did not answer are named.
const maxDown = 10

// toolTarget is what one scrape target is, from the collector's own up and scrape_samples_scraped series for it.
type toolTarget struct {
	node    string
	up      bool
	samples float64
	hasUp   bool
}

// answered: the target replied to the scrape and gave series. A tool that replies with nothing is not telling anything.
func (t toolTarget) answered() bool { return t.hasUp && t.up && t.samples > 0 }

// parseTools reads, from what the cluster collector serves on its source-health port, the scrape's own up and scrape_samples_scraped of every target of a
// job that is a tool (ToolJobs), by job and instance. Everything else in the text is ignored.
func parseTools(body string) map[string]map[string]toolTarget {
	var out map[string]map[string]toolTarget
	for _, line := range strings.Split(body, "\n") {
		isUp := strings.HasPrefix(line, "up{")
		if !isUp && !strings.HasPrefix(line, "scrape_samples_scraped{") {
			continue
		}
		name, labels, value, ok := splitSample(line)
		if !ok || math.IsNaN(value) || math.IsInf(value, 0) {
			continue
		}
		job := labels["job"]
		if _, tool := ToolJobs[job]; !tool {
			continue
		}
		inst := labels["instance"]
		if inst == "" {
			continue
		}
		if out == nil {
			out = map[string]map[string]toolTarget{}
		}
		if out[job] == nil {
			out[job] = map[string]toolTarget{}
		}
		t := out[job][inst]
		if n := labels["node"]; n != "" {
			t.node = n
		}
		if name == "up" {
			t.up, t.hasUp = value >= 1, true
		} else {
			t.samples = value
		}
		out[job][inst] = t
	}
	return out
}

// toolSources are the tools' entries of the report: one per job that has targets, in a stable order. All targets answering is receiving, some is
// degraded, none is failing. It is a reading of the moment (gauges), so unlike a receiver's counters it needs no history; a target that
// is gone expires from what the collector serves.
func (m *Monitor) toolSources() []*ikhnosv1.SourceHealth {
	jobs := make([]string, 0, len(m.tools))
	for j := range m.tools {
		jobs = append(jobs, j)
	}
	sort.Strings(jobs)
	var out []*ikhnosv1.SourceHealth
	for _, job := range jobs {
		ts := m.tools[job]
		sh := &ikhnosv1.SourceHealth{Receiver: job, Signals: ToolJobs[job], Targets: uint32(len(ts))}
		insts := make([]string, 0, len(ts))
		for i := range ts {
			insts = append(insts, i)
		}
		sort.Strings(insts)
		empty := 0 // targets that answered the scrape but gave no series
		for _, i := range insts {
			t := ts[i]
			if t.answered() {
				sh.TargetsUp++
				continue
			}
			if t.hasUp && t.up {
				empty++
			}
			name := t.node
			if name == "" {
				name = i
			}
			if len(sh.Down) < maxDown {
				sh.Down = append(sh.Down, clip(name, 80))
			}
		}
		switch {
		case sh.TargetsUp == sh.Targets:
			sh.State = ikhnosv1.SourceHealth_RECEIVING
		case sh.TargetsUp == 0 && empty == int(sh.Targets) && quietWithoutFault[job]:
			// The service answers and has nothing to report yet: the agent's link service has no link to show until one is measured.
			sh.State, sh.Down = ikhnosv1.SourceHealth_WAITING, nil
		case sh.TargetsUp > 0:
			sh.State = ikhnosv1.SourceHealth_DEGRADED
		default:
			sh.State = ikhnosv1.SourceHealth_FAILING
		}
		out = append(out, sh)
	}
	return out
}

// splitSample splits "name{k=\"v\",...} 12 [timestamp]" into its parts. Label values are quoted and may hold
// commas, braces and escaped quotes, so they are read character by character rather than split on punctuation.
func splitSample(line string) (name string, labels map[string]string, value float64, ok bool) {
	i := strings.IndexAny(line, "{ ")
	if i < 0 {
		return
	}
	name, rest := line[:i], line[i:]
	labels = map[string]string{}
	if rest[0] == '{' {
		j := 1
		for j < len(rest) && rest[j] != '}' {
			eq := strings.IndexByte(rest[j:], '=')
			if eq < 0 || j+eq+1 >= len(rest) || rest[j+eq+1] != '"' {
				return "", nil, 0, false
			}
			key := strings.TrimSpace(rest[j : j+eq])
			j += eq + 2
			var v strings.Builder
			for j < len(rest) && rest[j] != '"' {
				if rest[j] == '\\' && j+1 < len(rest) {
					j++
					switch rest[j] {
					case 'n':
						v.WriteByte('\n')
					default:
						v.WriteByte(rest[j])
					}
				} else {
					v.WriteByte(rest[j])
				}
				j++
			}
			if j >= len(rest) {
				return "", nil, 0, false
			}
			j++ // the closing quote
			labels[key] = v.String()
			for j < len(rest) && (rest[j] == ',' || rest[j] == ' ') {
				j++
			}
		}
		if j >= len(rest) {
			return "", nil, 0, false
		}
		rest = rest[j+1:]
	}
	f := strings.Fields(rest)
	if len(f) == 0 {
		return "", nil, 0, false
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return "", nil, 0, false
	}
	return name, labels, v, true
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
