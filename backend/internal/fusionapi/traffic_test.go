package fusionapi

import (
	"context"
	"strings"
	"testing"
)

// A real Prometheus drops the metric name from what increase() returns, so the counters are told apart by a label the query gives them,
// not by the series' name; and the query fits what the proxy to the store allows.
func TestTheGatewayCountersAreNamedByALabelNotBySeriesName(t *testing.T) {
	if strings.Contains(trafficQuery, "by (__name__)") || strings.Contains(trafficQuery, "__name__=~") {
		t.Errorf("the counters' names are read from the series, which increase() drops: %s", trafficQuery)
	}
	if n := strings.Count(trafficQuery, `label_replace(sum(increase(otelcol_`); n != 15 {
		t.Errorf("%d counters asked, want 3 kinds of telemetry by 5 counters: %s", n, trafficQuery)
	}
	if len(trafficQuery) > 4096 {
		t.Errorf("a query of %d characters is refused by RawMetricQuery", len(trafficQuery))
	}
	for _, want := range []string{"otelcol_receiver_accepted_spans_total", "otelcol_exporter_send_failed_log_records_total", "otelcol_exporter_enqueue_failed_metric_points_total"} {
		if !strings.Contains(trafficQuery, `"counter", "`+want+`"`) {
			t.Errorf("%s is not asked for", want)
		}
	}
}

// The gateway's counters say something about every sender, so only a caller with no limit may ask, and only one that may read metrics.
func TestOnlyAnUnrestrictedCallerMayAskTheGatewaysTraffic(t *testing.T) {
	c := &Client{Prometheus: "http://127.0.0.1:1"} // never reached
	for name, s := range map[string]Scope{
		"limited to a namespace": {Signals: Signals, Namespaces: []string{"shop"}},
		"limited to a cluster":   {Signals: Signals, Clusters: []string{"c1"}},
		"not allowed metrics":    {Signals: []string{SignalLogs}},
	} {
		if tr, err := c.GatewayTraffic(context.Background(), s); err == nil || tr != nil {
			t.Errorf("%s: %+v, %v", name, tr, err)
		}
	}
}
