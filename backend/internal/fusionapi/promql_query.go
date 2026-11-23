package fusionapi

import (
	"regexp"
)

// The caller's own PromQL in a fused read.
//
// A query is given as promql=<name>=<expression> (repeat the parameter for several). The expression may name the
// resource it is read for with ${service}, ${namespace}, ${pod}, ${node}, ${cluster} and ${workload} (the Deployment,
// StatefulSet, DaemonSet, Job or CronJob its pod belongs to), so one query serves every service of a trace: such a query
// is evaluated once per resource (and once per distinct expression, so two pods of one service that the query tells apart
// only by ${service} cost one read) and its series sit on that resource. A query that names none of them is evaluated
// once for the whole trace and sits on the fused object. ${trace_id} and ${range} (the length of the window, in seconds,
// as a PromQL duration) are available to both, and so are ${application} and ${application_id} when the read is narrowed
// to an application (application=): a query about the whole application joins on the info series of the applications,
// e.g. sum by (service_name) (rate(http_server_request_duration_seconds_count[${range}]) * on(service_name,
// k8s_namespace_name, ikhnos_cluster_id) group_left ikhnos_application_info{application_id="${application_id}"}).
// When the read is narrowed, the per-resource queries are evaluated for the application's resources only.
//
// Substitution never lets a value change the shape of the query: a value that holds a quote, a backslash, a brace or
// a control character is not substituted (the resource is skipped and the answer says so), and ${service:re} gives the
// value escaped for use inside a regular expression.

// Names of the sources a query adds to Fused.Sources.
const SourcePromQL = "promql"

const (
	maxPromQueries   = 5
	maxPromQueryLen  = 2048
	maxPromRuns      = 60 // distinct expressions evaluated by one fused read
	maxPromValueLen  = 253
	promQueryNameMax = 32
)

var promNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// The variables an expression may use.
var (
	promResourceVars = []string{"service", "namespace", "pod", "node", "cluster", "workload"}
	promOtherVars    = []string{"trace_id", "range", "application", "application_id"}
	// promAppVars are the variables that need the read to be narrowed to an application.
	promAppVars = []string{"application", "application_id"}
)

// PromQuery is one query the caller asked to have in the fused object.
type PromQuery struct {
	Name string
	Expr string

	tpl         []tplPart
	perResource bool
}

type tplPart struct {
	lit string // literal text, when v is empty
	v   string // a variable name
	re  bool   // ${v:re}
}

// QueryResult is what one of the caller's queries returned: on a resource for the resource's own expansion of it, on the
// fused object for a query that is not about one resource, on a span cut to the span's time.
type QueryResult struct {
	Name string `json:"name"`
	// Query is the expression as it was evaluated, with the variables replaced (not repeated on spans).
	Query     string         `json:"query,omitempty"`
	Series    []MetricSeries `json:"series"`
	Truncated bool           `json:"truncated,omitempty"`
	// Error says why this query has no series; the others are not affected.
	Error string `json:"error,omitempty"`
}
