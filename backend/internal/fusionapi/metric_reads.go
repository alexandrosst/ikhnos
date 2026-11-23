package fusionapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
)

// MetricNames lists the metric names that have a series matching the filter in the range, at most limit of them (sorted).
// truncated says there were more.
func (c *Client) MetricNames(ctx context.Context, s Scope, f MetricFilter, tr TimeRange, limit int) (names []string, truncated bool, err error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return nil, false, err
	}
	sel, err := f.selectors(s)
	if err != nil {
		return nil, false, err
	}
	// (One more than limit is asked for: that is how a list that is exactly limit long tells from one that was cut.)
	names, err = c.promLabelValues(ctx, "__name__", url.Values{"match[]": sel, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}, "limit": {strconv.Itoa(limit + 1)}})
	if err != nil {
		return nil, false, err
	}
	sort.Strings(names)
	names, truncated = cutAt(names, limit)
	return names, truncated, nil
}

// promLabelValues asks Prometheus for the values of a label (the label-values call), with the selectors and range of q.
func (c *Client) promLabelValues(ctx context.Context, label string, q url.Values) ([]string, error) {
	data, err := c.prom(ctx, "/api/v1/label/"+label+"/values", q)
	if err != nil {
		return nil, err
	}
	var values []string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, errf(http.StatusBadGateway, "%s answered with something unexpected", storeProm)
	}
	return values, nil
}

// Series lists the label sets of the series matching the filter in the range, at most limit of them. truncated says there
// were more.
func (c *Client) Series(ctx context.Context, s Scope, f MetricFilter, tr TimeRange, limit int) (out []map[string]string, truncated bool, err error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return nil, false, err
	}
	sel, err := f.selectors(s)
	if err != nil {
		return nil, false, err
	}
	data, err := c.prom(ctx, "/api/v1/series", url.Values{"match[]": sel, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}, "limit": {strconv.Itoa(limit + 1)}})
	if err != nil {
		return nil, false, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, false, errf(http.StatusBadGateway, "%s answered with something unexpected", storeProm)
	}
	out, truncated = cutAt(visibleSeries(s, out), limit)
	return out, truncated, nil
}

// visibleSeries drops label sets the Scope may not see. The query already carried the Scope's matchers; this is the
// second check, on what actually came back.
func visibleSeries(s Scope, in []map[string]string) []map[string]string {
	if s.Unrestricted() {
		return in
	}
	out := in[:0]
	for _, l := range in {
		if s.visibleAt(l[lblNamespace], l[lblCluster]) {
			out = append(out, l)
		}
	}
	return out
}
