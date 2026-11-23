package fusionapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// MetricRange returns the values of every series matching the filter over the range, at most maxSeries of them (the
// answer says when it left some out).
func (c *Client) MetricRange(ctx context.Context, s Scope, f MetricFilter, tr TimeRange, step time.Duration, maxSeries int) (series []MetricSeries, truncated bool, err error) {
	if err := s.needSignal(SignalMetrics); err != nil {
		return nil, false, err
	}
	sel, err := f.selectors(s)
	if err != nil {
		return nil, false, err
	}
	data, err := c.prom(ctx, "/api/v1/query_range", url.Values{
		"query": {strings.Join(sel, " or ")}, "start": {unixFloat(tr.From)}, "end": {unixFloat(tr.To)}, "step": {strconv.FormatFloat(step.Seconds(), 'f', -1, 64)}, "limit": {strconv.Itoa(maxSeries + 1)},
	})
	if err != nil {
		return nil, false, err
	}
	return decodeMatrix(data, s, maxSeries)
}

// decodeMatrix reads the "data" of a Prometheus range query into series, leaving out those the Scope may not see and
// keeping at most maxSeries of them (sorted by name and labels, so the cut is stable).
func decodeMatrix(data json.RawMessage, s Scope, maxSeries int) (series []MetricSeries, truncated bool, err error) {
	var res struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][2]any          `json:"values"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &res); err != nil || res.ResultType != "matrix" {
		return nil, false, errf(http.StatusBadGateway, "%s answered with something unexpected", storeProm)
	}
	for _, r := range res.Result {
		if !s.NamespaceVisible(r.Metric[lblNamespace]) || !s.ClusterVisible(r.Metric[lblCluster]) {
			continue
		}
		ms := MetricSeries{Name: r.Metric[lblName], Category: MetricCategory(r.Metric[lblName]), Labels: map[string]string{}}
		for k, v := range r.Metric {
			if k != lblName {
				ms.Labels[k] = v
			}
		}
		for _, v := range r.Values {
			t, ok1 := v[0].(float64)
			str, ok2 := v[1].(string)
			if !ok1 || !ok2 {
				continue
			}
			val, perr := strconv.ParseFloat(str, 64)
			if perr != nil || math.IsNaN(val) || math.IsInf(val, 0) {
				continue // JSON has no NaN or Inf
			}
			ms.Points = append(ms.Points, Point{t, val})
		}
		ms.summarise()
		series = append(series, ms)
	}
	sort.Slice(series, func(i, j int) bool {
		if series[i].Name != series[j].Name {
			return series[i].Name < series[j].Name
		}
		return labelKey(series[i].Labels) < labelKey(series[j].Labels)
	})
	if len(series) > maxSeries {
		series, truncated = series[:maxSeries], true
	}
	return series, truncated, nil
}

func (m *MetricSeries) summarise() {
	m.Min, m.Max, m.Avg, m.Last = nil, nil, nil, nil
	if len(m.Points) == 0 {
		return
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	var sum float64
	for _, p := range m.Points {
		lo, hi = math.Min(lo, p[1]), math.Max(hi, p[1])
		sum += p[1]
	}
	avg, last := sum/float64(len(m.Points)), m.Points[len(m.Points)-1][1]
	m.Min, m.Max, m.Avg, m.Last = &lo, &hi, &avg, &last
}

func labelKey(l map[string]string) string {
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%s,", k, l[k])
	}
	return b.String()
}
