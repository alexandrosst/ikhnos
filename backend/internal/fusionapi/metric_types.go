package fusionapi

// lblName is Prometheus' label for a series' metric name; the others are the vocabulary's (vocab.go).
const lblName = "__name__"

// infoMetrics are the series the server itself writes into Prometheus to describe what telemetry carries only ids of. They are
// not telemetry, and a read of telemetry leaves them out unless it names one.
var infoMetrics = []string{AppInfoMetric, ClusterInfoMetric, OrgInfoMetric}

// Point is one sample, [unix seconds, value] in JSON.
type Point [2]float64

// MetricSeries is one series and what it did over the asked range.
type MetricSeries struct {
	Name string `json:"name"`
	// Category is system, kubernetes or application, by the metric's name (see MetricCategory).
	Category string            `json:"category,omitempty"`
	Labels   map[string]string `json:"labels"`
	Points   []Point           `json:"points,omitempty"`
	// Min, Max, Avg and Last summarise the points. They are absent (not zero) when the series has no usable point in the
	// range: a pod that stopped reporting has no last value, which is not the same as an idle one.
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
	Avg  *float64 `json:"avg,omitempty"`
	Last *float64 `json:"last,omitempty"`
}

// MetricFilter picks series. Every field is an exact match except NameRegex, which is a regular expression over the
// metric name; all of them are optional and combine with AND.
type MetricFilter struct {
	Name      string `json:"name,omitempty"`
	NameRegex string `json:"nameRegex,omitempty"`
	Service   string `json:"service,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Node      string `json:"node,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	// Categories keeps metrics of these categories (system, kubernetes, application) by their names; empty keeps all.
	Categories []string `json:"categories,omitempty"`
	// NoPod keeps only series that name no pod: with Node, what was reported about the node itself.
	NoPod bool `json:"noPod,omitempty"`
}
