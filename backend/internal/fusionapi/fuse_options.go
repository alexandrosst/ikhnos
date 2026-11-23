package fusionapi

// What a fused read is asked for (FuseOptions), its defaults and bounds, and the checks made before anything is read.

import "time"

// FuseOptions say what to join to a trace.
type FuseOptions struct {
	Logs    bool
	Metrics bool
	// Pad widens the window around the trace in which logs and metrics are looked for. Default 2 minutes.
	Pad time.Duration
	// MetricRegex limits which metric names are returned. Default: all.
	MetricRegex string
	// MaxLogs is the most log lines returned (default 500). MaxSeries the most series per resource (default 15).
	MaxLogs   int
	MaxSeries int
	// Points is the target number of samples per series (default 60).
	Points int
	// SpanPad widens each span's own time when its metrics are cut out of its resource's series (default 30 seconds).
	// Samples arrive every 30 to 60 seconds, so a span that lasts milliseconds would otherwise often have none.
	SpanPad time.Duration

	// ContextLogs also reads, for each resource, the lines it wrote around the trace that carry no trace id (the
	// context a failing request sat in). They sit on the resource. MaxContextLogs is the most per resource (default 50).
	ContextLogs    bool
	MaxContextLogs int
	// SystemLogs also reads the lines of the system namespaces (SystemNamespaces, default kube-system) on the nodes the
	// trace ran on, over the same window. They sit on the fused object. MaxSystemLogs is the most in all (default 100).
	SystemLogs       bool
	SystemNamespaces []string
	MaxSystemLogs    int
	// LogSeverity ("error" or "error,warn") and LogContains narrow every log read of this fused read.
	LogSeverity string
	LogContains string
	// MetricViews says which series are read for a resource: the service's own, its pod's, its node's.
	MetricViews MetricViews
	// OmitAttributes and OmitEvents leave the span and resource attributes, or the span events, out of the answer.
	OmitAttributes bool
	OmitEvents     bool
	// Compact leaves out of each span what its resource already says (service, namespace, pod, node, cluster): the span's
	// resource key names the resource, where they are given once (shape=compact).
	Compact bool
	// Spans keeps only the spans that match (the trace's own totals stay those of the whole trace).
	Spans SpanFilter

	// PromQL are the caller's own queries (see promql_query.go and promql_template.go). PromQLSpans also cuts the
	// per-resource ones to each span's time.
	// PromQLStep and PromQLPad override the step and the window (default: those of the metrics); PromQLServices limits
	// the per-resource queries to resources of these services.
	PromQL         []PromQuery
	PromQLSpans    bool
	PromQLStep     time.Duration
	PromQLPad      time.Duration
	PromQLServices []string
	// PromQLAsWritten keeps the queries about the whole trace as written when the read is narrowed to an application; by
	// default they are narrowed to it too (narrowToApplication).
	PromQLAsWritten bool

	// Topology adds what each service calls and is called by; Changes the events around the trace (ChangesBefore before
	// it, Pad after it, at most MaxChanges). Both come from Extras, which the server sets; without it they are unavailable.
	Topology      bool
	Changes       bool
	ChangesBefore time.Duration
	MaxChanges    int
	Extras        Extras

	// Applications are the ikhnos applications the caller may know of: each resource is tagged with those it is part of.
	// Application, when set, narrows what is joined to the trace to that application's resources (see focus.go); it
	// also gives ${application} and ${application_id} to the caller's PromQL.
	Applications []AppGroup
	Application  *AppGroup
	// Names are the names of the organisation and its clusters, read by the server once for the request (nil when it knows
	// none): the resources and the context of the answer carry them beside the ids.
	Names *Names
}

// MetricViews picks which metric series a resource carries. None set means the default, App and Pod.
type MetricViews struct{ App, Pod, Node bool }

func (v MetricViews) orDefault() MetricViews {
	if !v.App && !v.Pod && !v.Node {
		return MetricViews{App: true, Pod: true}
	}
	return v
}

// SpanFilter narrows the spans a fused read returns. Every field is optional and combines with AND.
type SpanFilter struct {
	Service     string
	Status      string // error | ok | unset
	MinDuration time.Duration
}

func (f SpanFilter) active() bool { return f.Service != "" || f.Status != "" || f.MinDuration > 0 }

func (f SpanFilter) keeps(sp *Span) bool {
	return (f.Service == "" || sp.Service == f.Service) && (f.Status == "" || sp.Status == f.Status) &&
		sp.DurationMs >= float64(f.MinDuration)/float64(time.Millisecond)
}

const (
	defaultPad     = 2 * time.Minute
	maxPad         = time.Hour
	defaultMaxLogs = 500
	hardMaxLogs    = 2000
	defaultSpanPad = 30 * time.Second
	// maxSpanPoints is the most metric points all the spans of one fused read carry between them. Spans of one resource
	// that overlap in time repeat the same samples, so a trace with thousands of spans would otherwise multiply the answer;
	// past it a span keeps each series' min, max, average and last value but not its points.
	maxSpanPoints         = 20000
	defaultMaxContextLogs = 50
	hardMaxContextLogs    = 500
	defaultMaxSystemLogs  = 100
	hardMaxSystemLogs     = 1000
	maxSystemNodes        = 5
	defaultMaxSeries      = 15
	hardMaxSeries         = 100
	// fuseConcurrency is how many reads of one fused read are in flight at once, its per-resource joins (context logs, metrics,
	// queries) together; the Client's own lanes bound the stores across reads.
	fuseConcurrency = 8
	// maxFusedResources is the most resources one fused read looks up metrics for; a trace with more says so.
	maxFusedResources = 20
)

// CheckApplicationVars refuses a query that names the application (${application}, ${application_id}) in a read that is
// not narrowed to one.
func (o *FuseOptions) CheckApplicationVars() error {
	if o.Application != nil {
		return nil
	}
	for i := range o.PromQL {
		if o.PromQL[i].uses(promAppVars...) {
			return badRequest("promql %s uses ${application} or ${application_id}: name the application with application=", o.PromQL[i].Name)
		}
	}
	return nil
}

func (o *FuseOptions) defaults() error {
	if err := o.CheckApplicationVars(); err != nil {
		return err
	}
	if o.Pad == 0 {
		o.Pad = defaultPad
	}
	if o.Pad < 0 || o.Pad > maxPad {
		return badRequest("pad must be between 0 and %s", maxPad)
	}
	if o.SpanPad == 0 {
		o.SpanPad = defaultSpanPad
	}
	if o.SpanPad < 0 || o.SpanPad > maxPad {
		return badRequest("span_pad must be between 0 and %s", maxPad)
	}
	if o.MaxLogs <= 0 {
		o.MaxLogs = defaultMaxLogs
	}
	if o.MaxLogs > hardMaxLogs {
		o.MaxLogs = hardMaxLogs
	}
	if o.MaxSeries <= 0 {
		o.MaxSeries = defaultMaxSeries
	}
	if o.MaxSeries > hardMaxSeries {
		o.MaxSeries = hardMaxSeries
	}
	if o.Points <= 0 {
		o.Points = 60
	}
	if o.MaxContextLogs <= 0 {
		o.MaxContextLogs = defaultMaxContextLogs
	}
	if o.MaxContextLogs > hardMaxContextLogs {
		o.MaxContextLogs = hardMaxContextLogs
	}
	if o.MaxSystemLogs <= 0 {
		o.MaxSystemLogs = defaultMaxSystemLogs
	}
	if o.MaxSystemLogs > hardMaxSystemLogs {
		o.MaxSystemLogs = hardMaxSystemLogs
	}
	if len(o.SystemNamespaces) == 0 {
		o.SystemNamespaces = []string{"kube-system"}
	}
	if o.PromQLPad < 0 || o.PromQLPad > maxPad {
		return badRequest("promql_pad must be between 0 and %s", maxPad)
	}
	if o.PromQLStep < 0 || (o.PromQLStep > 0 && o.PromQLStep < time.Second) {
		return badRequest("promql_step must be at least one second")
	}
	if o.ChangesBefore == 0 {
		o.ChangesBefore = defaultChangesBefore
	}
	if o.ChangesBefore < 0 || o.ChangesBefore > maxChangesBefore {
		return badRequest("changes_before must be between 0 and %s", maxChangesBefore)
	}
	if o.MaxChanges <= 0 {
		o.MaxChanges = defaultMaxChanges
	}
	if o.MaxChanges > hardMaxChanges {
		o.MaxChanges = hardMaxChanges
	}
	switch o.Spans.Status {
	case "", "error", "ok", "unset":
	default:
		return badRequest("span_status must be error, ok or unset")
	}
	return nil
}

// checkFilters refuses the options that are the caller's mistake whatever the stores are doing, before anything is read.
func (o *FuseOptions) checkFilters(s Scope) error {
	if o.MetricRegex != "" {
		if _, err := (MetricFilter{NameRegex: o.MetricRegex}).matchers(s); err != nil {
			return err
		}
	}
	if o.Logs || o.ContextLogs || o.SystemLogs {
		if _, err := (LogFilter{Severity: o.LogSeverity, Contains: o.LogContains}).logQL(s); err != nil {
			return err
		}
	}
	if o.SystemLogs {
		for _, ns := range o.SystemNamespaces {
			if err := checkValue("system_namespaces", ns); err != nil {
				return err
			}
		}
	}
	return nil
}
