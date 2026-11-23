package fusionapi

// The metric series of a fused read's resources: the views of each (its service, its pod, its node), each question asked once.

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// metricReads shares the range reads of one fused read. Resources that differ only in their pod (the replicas of a service)
// ask for the same app view, and those on one node for the same node view; each question is asked once and every resource
// that asked carries the answer. (The answer is read-only from then on, so they share its slices.)
type metricReads struct {
	c         *Client
	s         Scope
	window    TimeRange
	step      time.Duration
	maxSeries int
	mu        sync.Mutex
	reads     map[string]*metricRead
}

type metricRead struct {
	once      sync.Once
	series    []MetricSeries
	truncated bool
	err       error
}

func newMetricReads(c *Client, s Scope, window TimeRange, step time.Duration, maxSeries int) *metricReads {
	return &metricReads{c: c, s: s, window: window, step: step, maxSeries: maxSeries, reads: map[string]*metricRead{}}
}

// metricKey is what tells two views apart: two filters alike ask the same question.
func metricKey(f MetricFilter) string { return fmt.Sprintf("%+v", f) }

func (m *metricReads) get(ctx context.Context, f MetricFilter) ([]MetricSeries, bool, error) {
	r := m.read(metricKey(f))
	r.once.Do(func() { r.series, r.truncated, r.err = m.c.MetricRange(ctx, m.s, f, m.window, m.step, m.maxSeries) })
	return r.series, r.truncated, r.err
}

// metricFilters are the views a resource's series are read by, as the options choose them: what the service reported
// under its name and namespace, what was reported about its pod, and about its node.
func metricFilters(r *Resource, opts FuseOptions) []MetricFilter {
	var filters []MetricFilter
	views := opts.MetricViews.orDefault()
	if views.App && r.Service != "" {
		filters = append(filters, MetricFilter{NameRegex: opts.MetricRegex, Service: r.Service, Namespace: r.Namespace, Cluster: r.Cluster})
	}
	if views.Pod && r.Pod != "" {
		filters = append(filters, MetricFilter{NameRegex: opts.MetricRegex, Pod: r.Pod, Namespace: r.Namespace, Cluster: r.Cluster})
	}
	if views.Node && r.Node != "" {
		filters = append(filters, MetricFilter{NameRegex: opts.MetricRegex, Node: r.Node, Cluster: r.Cluster, NoPod: true})
	}
	return filters
}

// resourceMetrics reads the metric series saved for one resource: what the service itself reported under its name
// and namespace, and what was reported about its pod (the infrastructure view, which usually carries no service name).
func resourceMetrics(ctx context.Context, r *Resource, opts FuseOptions, reads *metricReads) ([]MetricSeries, bool, error) {
	filters := metricFilters(r, opts)
	var out []MetricSeries
	var perView [][]MetricSeries
	seen := map[string]bool{}
	truncated := false
	for _, f := range filters {
		series, trunc, err := reads.get(ctx, f)
		if err != nil {
			return nil, false, err
		}
		truncated = truncated || trunc
		var own []MetricSeries
		for _, m := range series {
			k := m.Name + "{" + labelKey(m.Labels) + "}"
			if !seen[k] {
				seen[k] = true
				own = append(own, m)
				out = append(out, m)
			}
		}
		perView = append(perView, own)
	}
	if len(out) > opts.MaxSeries {
		// Cut by taking one series of each view in turn, so that a service with many series of its own does not leave the pod's
		// (its CPU, its memory, its restarts) out of the answer for good.
		out = out[:0]
		for i := 0; len(out) < opts.MaxSeries; i++ {
			more := false
			for _, v := range perView {
				if i < len(v) && len(out) < opts.MaxSeries {
					out, more = append(out, v[i]), true
				}
			}
			if !more {
				break
			}
		}
		truncated = true
	}
	return out, truncated, nil
}

// prefetch reads the views of all the resources in one range query, the selectors of every view joined with "or", and
// keeps for each view the answer its own read (MetricRange) would have had; a view it cannot tell so is left to get,
// which reads it on its own as before. A query Prometheus refuses for what it is (batchRefused: too long, an answer too
// large) leaves them all to get. Any other failure would have been each view's own read's too, so it is kept as every
// view's answer, and each resource reports it as it did when its reads failed.
//
// Why the split is exact:
//   - "or" keeps every series of either side once, and every series of a view is in the answer: the views differ only in
//     their label matchers (the name matchers are the options', alike for all), so a series of one view that has the labels
//     of another's apart from its name is that other's too, and nothing is dropped as a duplicate.
//   - A series is told to every view whose selector it matches, by Prometheus' own matchers.
//   - Prometheus (3.2 and later; the chart runs 3.5) sorts a range query's series by their labels and then cuts them to
//     the limit. A view's own read asked for maxSeries+1, so it had the first maxSeries+1 of its series in that order; the
//     series of a view in this answer, sorted the same way, are the first of its series too, as many as the limit left.
//     Where that is maxSeries+1 or more, or the answer was not full (so it holds every series of every view), the first
//     maxSeries+1 of them are what the view's own read had, and cutMatrix makes of them what decodeMatrix made of that.
//     A view with fewer in a full answer may have more past its end, and is read on its own.
//   - A Prometheus that does not take the limit answers every series, more than the limit; each view's own read then had
//     every series of its own too, and so it has here.
func (m *metricReads) prefetch(ctx context.Context, filters []MetricFilter) {
	type view struct {
		key  string
		sels [][]*labels.Matcher
	}
	var views []view
	var exprs []string
	seen := map[string]bool{}
	for _, f := range filters {
		key := metricKey(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		sels, err := f.selectors(m.s)
		if err != nil {
			continue // its own read fails as it always did, and alone
		}
		v := view{key: key}
		for _, sel := range sels {
			ms, err := parser.ParseMetricSelector(sel)
			if err != nil {
				v.sels = nil
				break
			}
			v.sels = append(v.sels, ms)
		}
		if v.sels == nil {
			continue
		}
		views = append(views, v)
		exprs = append(exprs, sels...)
	}
	if len(views) < 2 {
		return
	}
	limit := len(views) * (m.maxSeries + 1)
	env, err := m.c.promForm(ctx, "/api/v1/query_range", url.Values{
		"query": {strings.Join(uniqueInOrder(exprs), " or ")},
		"start": {unixFloat(m.window.From)}, "end": {unixFloat(m.window.To)},
		"step": {strconv.FormatFloat(m.step.Seconds(), 'f', -1, 64)}, "limit": {strconv.Itoa(limit)},
	})
	var rs []promSeries
	if err == nil {
		rs, err = parseMatrix(env.Data)
	}
	if err != nil {
		if !batchRefused(err) {
			for _, v := range views {
				m.keepErr(v.key, err)
			}
		}
		return
	}
	unlimited := len(rs) > limit
	full := len(rs) >= limit && !unlimited
	type keyed struct {
		promSeries
		ls labels.Labels
	}
	sorted := make([]keyed, len(rs))
	for i, r := range rs {
		sorted[i] = keyed{r, labels.FromMap(r.Metric)}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return labels.Compare(sorted[i].ls, sorted[j].ls) < 0 })
	again := 0
	for _, v := range views {
		var mine []promSeries
		for _, r := range sorted {
			if slices.ContainsFunc(v.sels, func(ms []*labels.Matcher) bool { return matchesAll(ms, r.Metric) }) {
				mine = append(mine, r.promSeries)
			}
		}
		if full && len(mine) <= m.maxSeries {
			again++
			continue
		}
		if !unlimited && len(mine) > m.maxSeries+1 {
			mine = mine[:m.maxSeries+1] // what its own read, asking for maxSeries+1, was answered
		}
		series, truncated := cutMatrix(mine, m.s, m.maxSeries)
		m.keep(v.key, series, truncated)
	}
	if again > 0 {
		slog.Debug("fused read: the metric views of all the resources were a full answer; some are read again on their own",
			"views", len(views), "again", again)
	}
}

// matchesAll says whether a series' labels satisfy every matcher of a selector (a label it does not have is empty).
func matchesAll(ms []*labels.Matcher, metric map[string]string) bool {
	for _, mt := range ms {
		if !mt.Matches(metric[mt.Name]) {
			return false
		}
	}
	return true
}

// keep records a view's answer, which get then gives without asking Prometheus.
func (m *metricReads) keep(key string, series []MetricSeries, truncated bool) {
	r := m.read(key)
	r.once.Do(func() { r.series, r.truncated = series, truncated })
}

// keepErr records that a view's read failed, as get then says without asking Prometheus.
func (m *metricReads) keepErr(key string, err error) {
	r := m.read(key)
	r.once.Do(func() { r.err = err })
}

// read is a view's entry, made when there is none.
func (m *metricReads) read(key string) *metricRead {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.reads[key]
	if r == nil {
		r = &metricRead{}
		m.reads[key] = r
	}
	return r
}
