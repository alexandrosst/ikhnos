package fusionapi

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// What a part of a fused read reports about itself.
const (
	SourceOK           = "ok"
	SourceNotRequested = "not requested"
	SourceNotAllowed   = "not allowed" // the token's scope does not include this signal
	SourceUnavailable  = "unavailable" // the store could not be reached: FUSION off or starting
	SourceError        = "error"

	// The keys of Fused.Sources beyond the three signals, for the log reads that are not tied to a span.
	SourceContextLogs = "contextLogs"
	SourceSystemLogs  = "systemLogs"
)

// sourceState is what a part of a read reports about itself when err ended it.
func sourceState(err error) string {
	if IsUnavailable(err) {
		return SourceUnavailable
	}
	return SourceError
}

// callerGone is the error of a context whose caller went away. A request that merely ran out of its time is not that: it
// still returns what the stores answered, with the parts that did not make it named in the sources and warnings.
func callerGone(ctx context.Context) error {
	if err := ctx.Err(); errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// FusedLogs says what became of the trace's log lines. The lines whose span is in the trace sit on that span; these
// are the rest.
type FusedLogs struct {
	Total   int `json:"total"`
	Matched int `json:"matched"` // lines attached to a span
	// Unmatched carries a trace id but no span id, or the id of a span this read does not have.
	Unmatched []LogEntry `json:"unmatched,omitempty"`
	Truncated bool       `json:"truncated,omitempty"`
	// OnFilteredSpans counts lines that sit on spans span_* filters left out of the answer (they are in Total and Matched).
	OnFilteredSpans int `json:"onFilteredSpans,omitempty"`
}

// SystemLogs are the lines of the system namespaces on the nodes a trace ran on.
type SystemLogs struct {
	Namespaces []string   `json:"namespaces"`
	Nodes      []string   `json:"nodes"`
	Entries    []LogEntry `json:"entries"`
	Truncated  bool       `json:"truncated,omitempty"`
}

// Fused is a trace with the logs and metrics saved around it: each span carries the log lines written under its id,
// and each resource the metric series of the same service, namespace and pod over the trace's time. A part that could
// not be read is named in Sources and Warnings; the trace itself always comes back or the whole read fails.
type Fused struct {
	*Trace
	Logs FusedLogs `json:"logs"`
	// SystemLogs is set when include=system_logs was asked for and the logs could be read.
	SystemLogs *SystemLogs `json:"systemLogs,omitempty"`
	// Queries are the caller's own queries that are not about one resource (promql); the others sit on the resources.
	Queries []QueryResult `json:"queries,omitempty"`
	// Changes are events ikhnos recorded about the trace's services, nodes and clusters around the trace (include=changes).
	Changes []ChangeEvent `json:"changes,omitempty"`
	// SpansOmitted counts spans the span_* filters left out; the trace's spanCount is that of the whole trace.
	SpansOmitted int `json:"spansOmitted,omitempty"`
	// Application is the application the joins were narrowed to (application=); the trace's spans are all there regardless.
	Application *AppRef           `json:"application,omitempty"`
	Sources     map[string]string `json:"sources"`
	// Joins says in words how each requested signal was tied to the trace, because the two are not alike: a log line
	// carries the trace and span id, a metric sample carries neither.
	Joins map[string]string `json:"joins,omitempty"`
	// JoinedBy says the same as Joins, source by source, in the words of a closed list (the Join* constants), so a program
	// can tell an exact join from an associated one without reading the prose. It has the same keys as Joins.
	JoinedBy map[string]string `json:"joinedBy,omitempty"`
	// Context names the organisation, the clusters and the applications the trace is part of, where ikhnos knows them.
	Context  *FusedContext `json:"context,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
}

// How a part of a fused read is tied to the trace (Fused.JoinedBy). Only the first is exact; the rest are associated, not
// proven: what was read sits where the trace ran, when it ran, but carries nothing that says it belongs to the trace.
const (
	// JoinTraceID: the records carry the trace's id, and each sits on the span whose id it carries (include=logs).
	JoinTraceID = "trace_id"
	// JoinResourceWindow: what the same service, namespace and pod as a resource wrote or reported in the trace's window,
	// padded (include=context_logs, include=metrics, and promql queries that name a resource variable).
	JoinResourceWindow = "resource_window"
	// JoinNodeWindow: what was written on the nodes the trace ran on, in the trace's window (include=system_logs).
	JoinNodeWindow = "node_window"
	// JoinWindow: read once for the whole trace over its window, tied to no resource (promql queries that name none).
	JoinWindow = "window"
	// JoinResourceOrWindow is promql when some queries name a resource variable and some do not: each query is read the
	// one way or the other, its results on a resource (resource_window) or in queries (window). One word, so that a
	// program compares it and never splits it.
	JoinResourceOrWindow = "resource_or_window"
	// JoinTopology: the service ikhnos knows by the same name, namespace and cluster, as of now (include=topology).
	JoinTopology = "topology"
	// JoinChangesWindow: what ikhnos recorded about the trace's services, nodes and clusters around its window
	// (include=changes).
	JoinChangesWindow = "changes_window"
)

// JoinWords is every value Fused.JoinedBy may hold.
var JoinWords = []string{JoinTraceID, JoinResourceWindow, JoinNodeWindow, JoinWindow, JoinResourceOrWindow,
	JoinTopology, JoinChangesWindow}

// join says how a source was tied to the trace, in words and in the Join* words, so that the two cannot drift apart.
func (f *Fused) join(source, by, how string) {
	f.Joins[source], f.JoinedBy[source] = how, by
}

// FuseTrace reads one trace and joins logs and metrics to it as asked.
//
// It runs in three steps. Everything that can be the caller's mistake (a bad filter, a window or step out of bounds) is checked
// before any store is asked. Then each requested join is started (fuseRun's join methods, one per part of the answer), all
// at once, their store reads sharing one bound on how many are in flight. Last, when all are done, what was read about
// the resources is cut to each span and the options that only narrow the answer are applied.
func (c *Client) FuseTrace(ctx context.Context, s Scope, id string, opts FuseOptions) (*Fused, error) {
	if err := opts.defaults(); err != nil {
		return nil, err
	}
	if err := opts.checkFilters(s); err != nil {
		return nil, err
	}
	tr, err := c.Trace(ctx, s, id)
	if err != nil {
		return nil, err
	}
	run, err := c.newFuseRun(ctx, s, tr, opts)
	if err != nil {
		return nil, err
	}
	if tr.Start.IsZero() {
		// No span says when it ran, so there is no window to read anything in (it would be read around the year 1). Only what
		// needs no window, the topology as of now, is joined; the rest is said to be what it is: read, with nothing in it.
		run.note("the trace has no span with a start time, so nothing read in a window around it is joined: no logs, metrics, queries or changes")
		for _, k := range []struct {
			asked  bool
			source string
		}{{opts.Logs, SignalLogs}, {opts.ContextLogs, SourceContextLogs}, {opts.SystemLogs, SourceSystemLogs}, {opts.Metrics, SignalMetrics},
			{len(opts.PromQL) > 0, SourcePromQL}, {opts.Changes, SourceChanges}} {
			if k.asked {
				run.setSource(k.source, SourceOK)
			}
		}
		if opts.Topology {
			untimed := opts
			untimed.Changes = false
			run.opts = untimed
			run.ikhnos()
		}
	} else {
		run.traceLogs()
		run.contextLogs()
		run.systemLogs()
		run.metrics()
		run.promQL()
		run.ikhnos()
	}
	run.wg.Wait()
	if err := callerGone(ctx); err != nil {
		return nil, err
	}
	f := run.f
	metricsHit, queriesHit := attachToSpans(f, opts, opts.Metrics, len(opts.PromQL) > 0 && opts.PromQLSpans)
	for _, k := range []struct {
		kind string
		hit  bool
	}{{SignalMetrics, metricsHit}, {SourcePromQL, queriesHit}} {
		if k.hit {
			f.Warnings = append(f.Warnings, fmt.Sprintf("%s: the spans carry more than %d metric and query points between them; later spans keep each series' summary but not its points", k.kind, maxSpanPoints))
		}
	}
	shape(f, opts)
	nameFused(f, s, opts.Names)
	return f, nil
}

// fuseRun is one fused read in progress: the answer being built, and what every join of it shares.
type fuseRun struct {
	c    *Client
	ctx  context.Context
	s    Scope
	opts FuseOptions
	f    *Fused

	// looked is the resources worth a lookup of their own: those with a service or a pod (of the application, when the read is
	// narrowed to one), at most maxFusedResources. logServices is whose trace-linked lines are read.
	looked      []*Resource
	logServices []string
	// window is the trace's time padded by pad, which logs and metrics are read over; step the metrics' step. qwin and qstep
	// are the caller's queries' (promql_pad, promql_step), the same unless asked otherwise.
	window, qwin TimeRange
	step, qstep  time.Duration

	sem chan struct{} // bounds the store reads of all the joins together (fuseConcurrency)
	wg  sync.WaitGroup
	mu  sync.Mutex // guards f (and the resources' fields) while the joins run
}

// newFuseRun sets up the answer (its sources, joins and the warnings known before anything is read), chooses the resources to
// look up and works out the windows and steps, refusing a window or step that cannot be read.
func (c *Client) newFuseRun(ctx context.Context, s Scope, tr *Trace, opts FuseOptions) (*fuseRun, error) {
	f := &Fused{Trace: tr, Sources: map[string]string{SignalTraces: SourceOK, SignalLogs: SourceNotRequested, SignalMetrics: SourceNotRequested},
		Joins: map[string]string{}, JoinedBy: map[string]string{}}
	if opts.Logs {
		f.join(SignalLogs, JoinTraceID, "exact: log records of the trace's services that carry this trace's id, each placed on the span whose id it carries")
	}
	if opts.ContextLogs {
		f.Sources[SourceContextLogs] = SourceNotRequested
		f.join(SourceContextLogs, JoinResourceWindow, "associated, not proven: lines with no trace id from the same service, namespace and pod, in the trace's window (padded by pad); on the resource, not on a span")
	}
	if opts.SystemLogs {
		f.Sources[SourceSystemLogs] = SourceNotRequested
		f.join(SourceSystemLogs, JoinNodeWindow, "associated, not proven: lines of "+strings.Join(opts.SystemNamespaces, ", ")+" on the nodes the trace's pods ran on, in the trace's window (padded by pad)")
	}
	if opts.Metrics {
		f.join(SignalMetrics, JoinResourceWindow, "associated, not proven: series saved for the same service, namespace and pod; each resource carries them over the trace's time and each span the points inside its own time (plus span_pad either side); a metric sample carries no trace id")
	}
	if len(opts.PromQL) > 0 {
		f.Sources[SourcePromQL] = SourceNotRequested
		how := "associated, not proven: your own queries, each evaluated for a resource with its service, namespace and pod filled in (or once for the trace), over the trace's time; a metric sample carries no trace id"
		if opts.Application != nil && !opts.PromQLAsWritten {
			how += "; a query for the trace as a whole reads the application's series only: its service names, namespaces and clusters are on every selector (promql_as_written=true keeps it as written)"
		}
		f.join(SourcePromQL, promQLJoin(opts.PromQL), how)
	}
	opts.ikhnosJoins(f)
	run := &fuseRun{c: c, ctx: ctx, s: s, opts: opts, f: f, sem: make(chan struct{}, fuseConcurrency)}

	// A trace that spans weeks must not turn into an unbounded store read (nor one Loki refuses).
	var cut bool
	if run.window, cut = windowAround(tr, opts.Pad, MaxLogWindow); cut {
		run.note("the trace is longer than %d days; logs and metrics cover its last %d days", int(MaxLogWindow.Hours()/24), int(MaxLogWindow.Hours()/24))
	}
	if opts.Metrics && s.Allows(SignalMetrics) {
		var err error
		if run.step, err = ChooseStep(run.window, 0, opts.Points); err != nil {
			return nil, err
		}
	}
	if len(opts.PromQL) > 0 && s.Allows(SignalMetrics) {
		run.qwin = run.window
		if opts.PromQLPad > 0 {
			if run.qwin, cut = windowAround(tr, opts.PromQLPad, MaxWindow); cut {
				run.note("promql: the trace and promql_pad are longer than %d days; the queries cover the last %d days", int(MaxWindow.Hours()/24), int(MaxWindow.Hours()/24))
			}
		}
		chosen, err := ChooseStep(run.qwin, opts.PromQLStep, opts.Points)
		if err != nil {
			return nil, err
		}
		if run.qstep = opts.PromQLStep; run.qstep == 0 {
			run.qstep = chosen
		}
	}
	run.chooseResources()
	return run, nil
}

// chooseResources tags every resource with its applications and picks those to look up (looked, logServices), saying what was
// left out and which resources cannot be pinned to one cluster or namespace.
func (r *fuseRun) chooseResources() {
	tr, opts := r.f.Trace, r.opts
	tagApplications(tr.Resources, opts.Applications)
	looked := tr.Resources
	r.logServices = tr.Services
	if opts.Application != nil {
		ref := opts.Application.Ref()
		r.f.Application = &ref
		var warns []string
		looked, warns = focusResources(opts.Application, tr.Resources)
		r.f.Warnings = append(r.f.Warnings, warns...)
		r.logServices = servicesOf(looked)
	}
	if opts.Spans.active() {
		// Only the spans the span_* filters keep are returned, so what is joined to the resources is what those spans ran on: a
		// resource none of whose spans is kept has no span left in the answer for its metrics, context lines or queries. (The
		// trace's own lines are still read for every service: those on spans left out are counted, in logs.onFilteredSpans.)
		kept := map[string]bool{}
		for _, sp := range tr.Spans {
			if opts.Spans.keeps(sp) {
				kept[sp.Resource] = true
			}
		}
		looked = slices.DeleteFunc(slices.Clone(looked), func(res *Resource) bool { return !kept[res.Key] })
		if len(looked) == 0 {
			r.note("no span matches the span_* filters, so nothing is joined")
		}
	}
	looked = slices.DeleteFunc(slices.Clone(looked), func(res *Resource) bool { return res.Service == "" && res.Pod == "" })
	if len(looked) > maxFusedResources {
		r.note("the trace has more than %d resources; the rest have no metrics, context logs, queries or topology here", maxFusedResources)
		looked = looked[:maxFusedResources]
	}
	r.looked = looked
	if opts.Metrics || opts.ContextLogs {
		// A resource that does not say which cluster (or namespace) it is from cannot be pinned to one: the series and lines of the
		// same service or pod in every cluster (or namespace) are read for it - a pod called web-0 exists in each - and the answer
		// must say so.
		for _, d := range []struct {
			what, attr, where string
			of                func(*Resource) string
		}{
			{"cluster id", attrCluster, "cluster", func(res *Resource) string { return res.Cluster }},
			{"namespace", attrNamespace, "namespace", func(res *Resource) string { return res.Namespace }},
		} {
			var unpinned []string
			for _, res := range looked {
				if d.of(res) == "" {
					unpinned = append(unpinned, res.Key)
				}
			}
			if len(unpinned) > 0 {
				sort.Strings(unpinned)
				r.note("%d resource(s) carry no %s (%s), so their metrics and context logs may include those of a service or pod of the same name in another %s: %s", len(unpinned), d.what, d.attr, d.where, names(unpinned))
			}
		}
	}
}

// setSource records how a part of the answer went. A part that already failed stays failed: later reads of it only add to it.
func (r *fuseRun) setSource(signal, state string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cur := r.f.Sources[signal]; cur == SourceError || cur == SourceUnavailable {
		return
	}
	r.f.Sources[signal] = state
}

// warn records that a part failed, and why.
func (r *fuseRun) warn(signal string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.f.Sources[signal] = sourceState(err)
	r.f.Warnings = append(r.f.Warnings, fmt.Sprintf("%s: %v", signal, err))
}

// note adds a warning.
func (r *fuseRun) note(format string, a ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.f.Warnings = append(r.f.Warnings, fmt.Sprintf(format, a...))
}

// apply runs fn holding the answer's lock.
func (r *fuseRun) apply(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// start runs fn in its own goroutine, waited for before the answer is shaped.
func (r *fuseRun) start(fn func()) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		fn()
	}()
}

// forEachLooked runs fn for every looked-up resource, each in its own goroutine but at most fuseConcurrency at a time across all
// the joins of the read: a trace of many resources must not take every slot the stores share. A resource that cannot get a slot
// before the deadline is recorded as a failure of signal.
func (r *fuseRun) forEachLooked(signal string, fn func(*Resource) error) {
	r.forEach(r.looked, signal, fn)
}

// forEach is forEachLooked for some of the resources.
func (r *fuseRun) forEach(resources []*Resource, signal string, fn func(*Resource) error) {
	for _, res := range resources {
		res := res
		r.start(func() {
			release, err := acquire(r.ctx, r.sem)
			if err != nil {
				r.warn(signal, err)
				return
			}
			defer release()
			if err := fn(res); err != nil {
				r.warn(signal, err)
			}
		})
	}
}

// allowed says whether a requested part may be read at all, recording why not when it may not.
func (r *fuseRun) allowed(requested bool, source, signal string) bool {
	if !requested {
		return false
	}
	if !r.s.Allows(signal) {
		r.setSource(source, SourceNotAllowed)
		return false
	}
	return true
}

// uniqueInOrder is the strings with the repeats left out, the first of each where it was.
func uniqueInOrder(l []string) []string {
	if len(l) < 2 {
		return l
	}
	seen := make(map[string]bool, len(l))
	out := l[:0:0]
	for _, v := range l {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
