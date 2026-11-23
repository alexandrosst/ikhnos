package fusionapi

import (
	"time"
)

// SpanEvent is something that happened during a span (an exception, a log-like annotation).
type SpanEvent struct {
	Time       time.Time      `json:"time"`
	Name       string         `json:"name"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// Span is one unit of work in a trace, with the identity of the thing that did it.
type Span struct {
	SpanID        string         `json:"spanId"`
	ParentSpanID  string         `json:"parentSpanId,omitempty"`
	Name          string         `json:"name"`
	Kind          string         `json:"kind,omitempty"`
	Service       string         `json:"service,omitempty"`
	Namespace     string         `json:"namespace,omitempty"`
	Pod           string         `json:"pod,omitempty"`
	Node          string         `json:"node,omitempty"`
	Cluster       string         `json:"cluster,omitempty"`
	Resource      string         `json:"resource"` // the Resource key it belongs to
	Start         time.Time      `json:"start"`
	End           time.Time      `json:"end"`
	DurationMs    float64        `json:"durationMs"`
	Status        string         `json:"status"` // unset | ok | error
	StatusMessage string         `json:"statusMessage,omitempty"`
	Depth         int            `json:"depth"`
	Attributes    map[string]any `json:"attributes,omitempty"`
	Events        []SpanEvent    `json:"events,omitempty"`
	// Logs are the log lines saved with this span's id (filled in by a fused read).
	Logs []LogEntry `json:"logs,omitempty"`
	// Metrics are the points of the series saved for this span's resource that fall inside the span's own time, plus a
	// margin either side (a fused read; see FuseOptions.SpanPad). Associated by service, namespace and pod and by time,
	// not proven: a metric sample carries no trace or span id.
	Metrics []MetricSeries `json:"metrics,omitempty"`
	// Queries are the caller's own PromQL queries (a fused read's promql parameter) cut to this span's time, when
	// promql_spans asked for it. Only queries evaluated per resource can be cut to a span.
	Queries []QueryResult `json:"queries,omitempty"`
}

// Resource is what produced spans: a service in a pod in a namespace of a cluster. Metrics are the series saved for
// the same service, namespace and pod around the trace (filled in by a fused read).
type Resource struct {
	Key              string         `json:"key"`
	Service          string         `json:"service,omitempty"`
	Namespace        string         `json:"namespace,omitempty"`
	Pod              string         `json:"pod,omitempty"`
	Node             string         `json:"node,omitempty"`
	Cluster          string         `json:"cluster,omitempty"`
	Attributes       map[string]any `json:"attributes,omitempty"`
	Metrics          []MetricSeries `json:"metrics,omitempty"`
	MetricsTruncated bool           `json:"metricsTruncated,omitempty"`
	// Logs are lines this resource wrote around the trace that carry no trace id (include=context_logs).
	Logs          []LogEntry `json:"logs,omitempty"`
	LogsTruncated bool       `json:"logsTruncated,omitempty"`
	// Queries are the caller's own PromQL queries, evaluated for this resource over the trace's window (promql).
	Queries []QueryResult `json:"queries,omitempty"`
	// Topology is what ikhnos knows about the service: what it calls and what calls it (include=topology).
	Topology *ResourceTopology `json:"topology,omitempty"`
	// Applications are the ikhnos applications the resource is part of (fused reads; see focus.go).
	Applications []AppRef `json:"applications,omitempty"`
}

// Trace is one trace as Tempo has it: its spans in start order, and the resources behind them.
type Trace struct {
	TraceID    string      `json:"traceId"`
	Start      time.Time   `json:"start"`
	End        time.Time   `json:"end"`
	DurationMs float64     `json:"durationMs"`
	Services   []string    `json:"services"`
	SpanCount  int         `json:"spanCount"`
	ErrorCount int         `json:"errorCount"`
	Roots      []string    `json:"roots"`
	Spans      []*Span     `json:"spans"`
	Resources  []*Resource `json:"resources"`
	// Truncated says the trace had more spans than this API returns.
	Truncated bool `json:"truncated,omitempty"`
}

// MaxSpans is the most spans one trace read returns.
const MaxSpans = 5000

// keepSpans cuts a trace's spans (in start order) to n: its error spans first, since they are what a reader of a large trace
// is usually after, then the earliest of the others; the result is in start order again.
func keepSpans(spans []*Span, n int) []*Span {
	keep := make([]bool, len(spans))
	left := n
	for i, sp := range spans {
		if left > 0 && sp.Status == "error" {
			keep[i] = true
			left--
		}
	}
	for i := range spans {
		if left > 0 && !keep[i] {
			keep[i] = true
			left--
		}
	}
	out := make([]*Span, 0, n)
	for i, sp := range spans {
		if keep[i] {
			out = append(out, sp)
		}
	}
	return out
}
