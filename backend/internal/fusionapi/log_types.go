package fusionapi

import (
	"time"
)

// What Loki calls the log record's own fields, kept as structured metadata: trace_id and span_id are the join keys to Tempo.
// The labels of the resource are the vocabulary's (vocab.go), which also says which of them are index labels.
const (
	lokiTraceID  = "trace_id"
	lokiSpanID   = "span_id"
	lokiSeverity = "severity_text"
)

// LogEntry is one log line with the context it was saved with.
type LogEntry struct {
	Time      time.Time `json:"time"`
	Line      string    `json:"line"`
	Severity  string    `json:"severity,omitempty"`
	TraceID   string    `json:"traceId,omitempty"`
	SpanID    string    `json:"spanId,omitempty"`
	Service   string    `json:"service,omitempty"`
	Namespace string    `json:"namespace,omitempty"`
	Pod       string    `json:"pod,omitempty"`
	Cluster   string    `json:"cluster,omitempty"`
	// Category is system, kubernetes or application, by the namespace the line came from (see LogCategory).
	Category string `json:"category,omitempty"`
	// Labels are the stream's index labels; Metadata the record's structured metadata (everything else OTLP carried).
	Labels   map[string]string `json:"labels,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// LogFilter picks log lines. Every field is optional and combines with AND.
type LogFilter struct {
	Service   string `json:"service,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	// Categories keeps lines of these categories (system, kubernetes, application); empty keeps all.
	Categories []string `json:"categories,omitempty"`
	TraceID    string   `json:"traceId,omitempty"`
	SpanID     string   `json:"spanId,omitempty"`
	// Severity matches the record's severity text, ignoring case (error, warn, info, ...).
	Severity string `json:"severity,omitempty"`
	// Contains keeps only lines containing this text.
	Contains string `json:"contains,omitempty"`
	// Node keeps lines written on this Kubernetes node.
	Node string `json:"node,omitempty"`
	// Services keeps lines of any of these services (Service, when set, wins). It is an index label, so a read that knows
	// the services it wants (a trace's) does not have to open every stream.
	Services []string `json:"services,omitempty"`
	// Namespaces keeps lines of any of these namespaces (Namespace, when set, narrows it further).
	Namespaces []string `json:"namespaces,omitempty"`
	// NoTrace keeps only lines that carry no trace id: the context around a trace rather than what it wrote itself.
	NoTrace bool `json:"noTrace,omitempty"`
	// Backward lists newest first (Loki's default); otherwise oldest first.
	Backward bool `json:"backward,omitempty"`
}
