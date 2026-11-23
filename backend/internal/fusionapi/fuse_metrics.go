package fusionapi

// The metric series of a fused read's resources: the views of each (its service, its pod, its node), each question asked once.

import (
	"context"
	"fmt"
	"sync"
	"time"
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

func (m *metricReads) get(ctx context.Context, f MetricFilter) ([]MetricSeries, bool, error) {
	key := fmt.Sprintf("%+v", f)
	m.mu.Lock()
	r := m.reads[key]
	if r == nil {
		r = &metricRead{}
		m.reads[key] = r
	}
	m.mu.Unlock()
	r.once.Do(func() { r.series, r.truncated, r.err = m.c.MetricRange(ctx, m.s, f, m.window, m.step, m.maxSeries) })
	return r.series, r.truncated, r.err
}

// resourceMetrics reads the metric series saved for one resource: what the service itself reported under its name
// and namespace, and what was reported about its pod (the infrastructure view, which usually carries no service name).
func resourceMetrics(ctx context.Context, r *Resource, opts FuseOptions, reads *metricReads) ([]MetricSeries, bool, error) {
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
