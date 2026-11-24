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
//
// The call is GET only, and the selectors of a caller limited to applications name each of its places, so a question longer
// than maxGetQuery is split: its selectors are asked a batch at a time, each batch's question within the bound (a selector
// longer than that alone is asked alone), and the answers united. That is the same answer: the call returns the values of
// the series any selector matches, and so does the union of the calls. With a limit, each batch's answer is the first
// values of its own, sorted (Prometheus sorts before it cuts), so the first of the union are among them, and the caller's
// cut of the sorted union is the cut of the whole answer.
func (c *Client) promLabelValues(ctx context.Context, label string, q url.Values) ([]string, error) {
	rest, batches := labelValueBatches(q)
	var out []string
	for _, b := range batches {
		bq := url.Values{}
		for k, v := range rest {
			bq[k] = v
		}
		if len(b) > 0 {
			bq["match[]"] = b
		}
		data, err := c.prom(ctx, "/api/v1/label/"+label+"/values", bq)
		if err != nil {
			return nil, err
		}
		var values []string
		if err := json.Unmarshal(data, &values); err != nil {
			return nil, errf(http.StatusBadGateway, "%s answered with something unexpected", storeProm)
		}
		out = append(out, values...)
	}
	if len(batches) == 1 {
		return out, nil // (as the store gave them)
	}
	return distinctSorted(out), nil
}

// labelValueBatches splits the question of a label-values call (q) as promLabelValues asks it: rest is what every call
// carries (the range), batches the selectors of each call, at least one (with no selectors, one call that names none).
// The calls a list of services is charged by are counted with it too (ServiceListReads).
func labelValueBatches(q url.Values) (rest url.Values, batches [][]string) {
	rest = url.Values{}
	for k, v := range q {
		if k != "match[]" {
			rest[k] = v
		}
	}
	var cur []string
	size := len(rest.Encode())
	for _, sel := range q["match[]"] {
		n := len(url.Values{"match[]": {sel}}.Encode()) + 1
		if len(cur) > 0 && size+n > maxGetQuery {
			batches, cur, size = append(batches, cur), nil, len(rest.Encode())
		}
		cur, size = append(cur, sel), size+n
	}
	if len(cur) > 0 || len(batches) == 0 {
		batches = append(batches, cur)
	}
	return rest, batches
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
	env, err := c.promForm(ctx, "/api/v1/series", url.Values{"match[]": sel, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}, "limit": {strconv.Itoa(limit + 1)}})
	if err != nil {
		return nil, false, err
	}
	data := env.Data
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
		if s.seriesVisible(l) {
			out = append(out, l)
		}
	}
	return out
}
