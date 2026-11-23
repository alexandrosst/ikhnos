package fusionapi

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// TraceSummary is one hit of a trace search.
type TraceSummary struct {
	TraceID     string    `json:"traceId"`
	RootService string    `json:"rootService,omitempty"`
	RootName    string    `json:"rootName,omitempty"`
	Start       time.Time `json:"start"`
	DurationMs  float64   `json:"durationMs"`
	// Services are the services of the spans that matched the search; MatchedSpans how many matched.
	Services     []string `json:"services,omitempty"`
	MatchedSpans int      `json:"matchedSpans"`
}

type tempoSearch struct {
	Traces []struct {
		TraceID    string         `json:"traceID"`
		RootSvc    string         `json:"rootServiceName"`
		RootName   string         `json:"rootTraceName"`
		Start      json.Number    `json:"startTimeUnixNano"`
		DurationMs float64        `json:"durationMs"`
		SpanSet    *tempoSpanSet  `json:"spanSet"`
		SpanSets   []tempoSpanSet `json:"spanSets"`
	} `json:"traces"`
}

type tempoSpanSet struct {
	Matched int `json:"matched"`
	Spans   []struct {
		Start      json.Number `json:"startTimeUnixNano"`
		Duration   json.Number `json:"durationNanos"`
		Attributes []otlpKV    `json:"attributes"`
	} `json:"spans"`
}

// SearchTraces lists traces with spans matching the filter that started in the range, newest first, at most limit of them.
// truncated says there were more.
func (c *Client) SearchTraces(ctx context.Context, s Scope, f TraceFilter, tr TimeRange, limit int) ([]TraceSummary, bool, error) {
	if err := s.needSignal(SignalTraces); err != nil {
		return nil, false, err
	}
	q, err := f.traceQL(s)
	if err != nil {
		return nil, false, err
	}
	return c.tempoSearch(ctx, s, q, tr, limit)
}

// RawTraceSearch runs a TraceQL query exactly as written. Only a Scope with no namespace or cluster limit may.
func (c *Client) RawTraceSearch(ctx context.Context, s Scope, query string, tr TimeRange, limit int) ([]TraceSummary, bool, error) {
	if err := s.needSignal(SignalTraces); err != nil {
		return nil, false, err
	}
	if err := s.needUnrestricted("a TraceQL query"); err != nil {
		return nil, false, err
	}
	if query == "" || len(query) > 4096 {
		return nil, false, badRequest("q is required and at most 4096 characters")
	}
	return c.tempoSearch(ctx, s, query, tr, limit)
}

func (c *Client) tempoSearch(ctx context.Context, s Scope, q string, tr TimeRange, limit int) ([]TraceSummary, bool, error) {
	if err := tr.within(storeTempo, MaxSearchWindow); err != nil {
		return nil, false, err
	}
	var res tempoSearch
	if err := c.get(ctx, storeTempo, c.Tempo, "/api/search", url.Values{
		"q": {q}, "start": {unixSec(tr.From)}, "end": {unixSec(tr.To)}, "limit": {strconv.Itoa(limit + 1)}, "spss": {"20"}, // (one more: see MetricNames)
		"most_recent": {"true"}, // else the limit takes an arbitrary part of the range, and "newest first" is only the order of what it took
	}, nil, &res); err != nil {
		return nil, false, err
	}
	truncated := len(res.Traces) > limit
	if truncated {
		res.Traces = res.Traces[:limit]
	}
	restricted := !s.Unrestricted()
	out := make([]TraceSummary, 0, len(res.Traces))
	for _, t := range res.Traces {
		sum := TraceSummary{TraceID: normID(t.TraceID, 32), Start: nanos(t.Start), DurationMs: t.DurationMs}
		sets := t.SpanSets
		if t.SpanSet != nil {
			sets = append(sets, *t.SpanSet)
		}
		seen := map[string]bool{}
		var first, last time.Time
		for _, ss := range sets {
			sum.MatchedSpans += ss.Matched
			for _, sp := range ss.Spans {
				if st := nanos(sp.Start); !st.IsZero() {
					en := st
					if d, err := strconv.ParseInt(sp.Duration.String(), 10, 64); err == nil && d > 0 {
						en = st.Add(time.Duration(d))
					}
					if first.IsZero() || st.Before(first) {
						first = st
					}
					if en.After(last) {
						last = en
					}
				}
				if svc := strAttr(kvMap(sp.Attributes), attrService); svc != "" && !seen[svc] {
					seen[svc] = true
					sum.Services = append(sum.Services, svc)
				}
			}
		}
		sort.Strings(sum.Services)
		// The root of a trace may belong to a namespace this Scope cannot see; only what matched is its to know.
		if !restricted {
			sum.RootService, sum.RootName = t.RootSvc, t.RootName
		} else if !first.IsZero() {
			// Likewise the trace's own start and length cover spans outside the Scope; give the matched spans' extent.
			sum.Start = first
			sum.DurationMs = float64(last.Sub(first)) / float64(time.Millisecond)
		} else {
			sum.Start, sum.DurationMs = time.Time{}, 0
		}
		out = append(out, sum)
	}
	// Tempo does not promise an order: the newest come first here, the ones whose start is not known (a limited caller's trace with no
	// matched span) last.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start.IsZero() != out[j].Start.IsZero() {
			return !out[i].Start.IsZero()
		}
		return out[i].Start.After(out[j].Start)
	})
	return out, truncated, nil
}
