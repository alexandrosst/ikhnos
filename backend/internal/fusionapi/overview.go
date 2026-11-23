package fusionapi

// The lists and overviews that are not about one trace: the services with telemetry, and one service or application at a glance.

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Overview is one application at a glance: which signals it has and a few of the most recent and most interesting
// things in each, ready to follow into a fused trace.
type Overview struct {
	// ID and Services are set for an ikhnos application: its id and the service names its telemetry may carry.
	ID       string   `json:"id,omitempty"`
	Services []string `json:"services,omitempty"`
	// Members are, for an ikhnos application, its services as ikhnos knows them (name, namespace, cluster, aliases), each with the
	// signals it has where it runs: which of them the overview's traces, lines and metric names can have come from.
	Members     []AppServiceView `json:"members,omitempty"`
	Name        string           `json:"name"`
	Signals     []Signal         `json:"signals"`
	Traces      []TraceSummary   `json:"traces"`      // the most recent
	ErrorTraces []TraceSummary   `json:"errorTraces"` // the most recent with an error span
	ErrorLogs   []LogEntry       `json:"errorLogs"`   // the most recent error-level lines
	Metrics     []string         `json:"metrics"`     // metric names it reports
	// MetricsTruncated says it reports more than are listed.
	MetricsTruncated bool              `json:"metricsTruncated,omitempty"`
	Sources          map[string]string `json:"sources"`
	Warnings         []string          `json:"warnings,omitempty"`
}

// ServiceOverview reads the overview of one service.
//
// A service.name is shared by every service of that name, in every namespace and cluster; svc.Namespace and svc.Cluster, when
// given, keep the one in that namespace or cluster (a service of the same name elsewhere would otherwise be merged into it).
func (c *Client) ServiceOverview(ctx context.Context, s Scope, svc AppMember, tr TimeRange) (*Overview, error) {
	if err := checkValue("service", svc.Name); err != nil {
		return nil, err
	}
	for _, v := range []struct{ what, val string }{{"namespace", svc.Namespace}, {"cluster", svc.Cluster}} {
		if v.val != "" {
			if err := checkValue(v.what, v.val); err != nil {
				return nil, err
			}
		}
	}
	if svc.Namespace != "" || svc.Cluster != "" {
		// The same narrowing as an application of this one service, where it was said to run.
		fs, err := s.FocusOn(&AppGroup{Name: svc.Name, Members: []AppMember{{Name: svc.Name, Namespace: svc.Namespace, Cluster: svc.Cluster}}})
		if err != nil { // (only a namespace or cluster outside the caller's own can fail here)
			return nil, errf(http.StatusNotFound, "no service %q there that this credential may see", svc.Name)
		}
		s = fs
	}
	return c.overview(ctx, s, svc.Name, svc.Name, tr)
}

// ApplicationOverview reads the overview of one ikhnos application: the same things as for a service, over all of its
// services (the focus the Scope gets from the application).
func (c *Client) ApplicationOverview(ctx context.Context, s Scope, g *AppGroup, tr TimeRange) (*Overview, error) {
	fs, err := s.FocusOn(g)
	if err != nil {
		return nil, err
	}
	o, err := c.overview(ctx, fs, "", g.Name, tr)
	if err != nil {
		return nil, err
	}
	o.ID, o.Services = g.ID, fs.FocusServices
	if note := g.UnionFocusNote(); note != "" {
		o.Warnings = append(o.Warnings, note)
	}
	if per, _, warns, err := c.GroupSignals(ctx, s, []AppGroup{*g}, tr); err == nil {
		if views := DescribeGroupsIn([]AppGroup{*g}, per); len(views) == 1 {
			o.Members = views[0].Services
		}
		o.Warnings = uniqueInOrder(append(o.Warnings, warns...))
	} else {
		o.Warnings = append(o.Warnings, fmt.Sprintf("members: which of them have which signals could not be read: %v", err))
	}
	return o, nil
}

// OverviewReads is how many store reads a service's overview makes for a caller with Scope s, of the signals it may
// read: two trace searches (the recent and the failing), two log reads (the errors, and whether there is any line) and
// one list of metric names. The server charges a caller's rate limit by it.
func OverviewReads(s Scope) int {
	n := 0
	for sig, reads := range map[string]int{SignalTraces: 2, SignalLogs: 2, SignalMetrics: 1} {
		if s.Allows(sig) {
			n += reads
		}
	}
	return max(n, 1)
}

// ApplicationOverviewReads is OverviewReads for an application's overview, which also reads its members' signals
// (GroupSignals of the one application).
func ApplicationOverviewReads(s Scope) int { return OverviewReads(s) + GroupSignalsReads(s, 1) }

// signalReads reads each signal type a Scope allows at once, one goroutine each, and keeps what an answer made of the
// reads says about them: each signal's source state (SourceNotAllowed until its read is done, then SourceOK or the
// sourceState of its error), a warning for each read that failed, in the order they failed, and the first error.
type signalReads struct {
	s        Scope
	mu       sync.Mutex
	wg       sync.WaitGroup
	sources  map[string]string
	warnings []string
	firstErr error
}

func newSignalReads(s Scope) *signalReads {
	sources := map[string]string{SignalMetrics: SourceNotAllowed, SignalLogs: SourceNotAllowed, SignalTraces: SourceNotAllowed}
	return &signalReads{s: s, sources: sources}
}

// run starts the read of a signal, unless the Scope does not allow it. When the read succeeds, the function it returns is
// called with the lock held, after the source is marked SourceOK, to put its answer in place; it may add to warnings.
func (r *signalReads) run(signal string, read func() (keep func(), err error)) {
	if !r.s.Allows(signal) {
		return
	}
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		keep, err := read()
		r.mu.Lock()
		defer r.mu.Unlock()
		if err != nil {
			r.sources[signal] = sourceState(err)
			r.warnings = append(r.warnings, fmt.Sprintf("%s: %v", signal, err))
			if r.firstErr == nil {
				r.firstErr = err
			}
			return
		}
		r.sources[signal] = SourceOK
		keep()
	}()
}

// wait waits for the reads, and is the error of the answer as a whole: the caller's (it has gone), or, when no read
// succeeded, the first that failed. An answer with one store read is answered, and says what the others did.
func (r *signalReads) wait(ctx context.Context) error {
	r.wg.Wait()
	if err := callerGone(ctx); err != nil {
		return err
	}
	for _, v := range r.sources {
		if v == SourceOK {
			return nil
		}
	}
	return r.firstErr
}

// overview reads what a service (or, with no service named, whatever the Scope's focus selects) has. label is what the
// answer is called.
func (c *Client) overview(ctx context.Context, s Scope, name, label string, tr TimeRange) (*Overview, error) {
	reads := newSignalReads(s)
	o := &Overview{Name: label, Sources: reads.sources,
		Traces: []TraceSummary{}, ErrorTraces: []TraceSummary{}, ErrorLogs: []LogEntry{}, Metrics: []string{}}
	has := map[string]bool{}
	reads.run(SignalTraces, func() (func(), error) {
		recent, _, err := c.SearchTraces(ctx, s, TraceFilter{Service: name}, tr, 10)
		if err != nil {
			return nil, err
		}
		failed, _, err := c.SearchTraces(ctx, s, TraceFilter{Service: name, Status: "error"}, tr, 5)
		if err != nil {
			return nil, err
		}
		return func() { o.Traces, o.ErrorTraces, has[SignalTraces] = recent, failed, len(recent) > 0 }, nil
	})
	reads.run(SignalLogs, func() (func(), error) {
		lines, _, err := c.Logs(ctx, s, LogFilter{Service: name, Severity: "error", Backward: true}, tr, 10)
		if err != nil {
			return nil, err
		}
		one, _, err := c.Logs(ctx, s, LogFilter{Service: name, Backward: true}, tr, 1)
		if err != nil {
			return nil, err
		}
		return func() { o.ErrorLogs, has[SignalLogs] = lines, len(one) > 0 }, nil
	})
	reads.run(SignalMetrics, func() (func(), error) {
		names, truncated, err := c.MetricNames(ctx, s, MetricFilter{Service: name}, tr, 100)
		if err != nil {
			return nil, err
		}
		return func() { o.Metrics, o.MetricsTruncated, has[SignalMetrics] = names, truncated, len(names) > 0 }, nil
	})
	if err := reads.wait(ctx); err != nil {
		return nil, err
	}
	o.Warnings = reads.warnings
	for _, sig := range Signals {
		if has[sig] {
			o.Signals = append(o.Signals, Signal(sig))
		}
	}
	if len(o.Signals) == 0 && reads.firstErr == nil {
		return nil, errf(http.StatusNotFound, "no telemetry for %q in this range", label)
	}
	return o, nil
}
