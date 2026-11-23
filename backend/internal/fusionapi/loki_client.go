package fusionapi

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// lokiStreams is Loki's "streams" result. With the categorize-labels flag a value is [ns, line, {structuredMetadata,
// parsed}]; without it (an older Loki) it is [ns, line] and the structured metadata sits among the stream's labels.
type lokiStreams struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Stream map[string]string   `json:"stream"`
			Values [][]json.RawMessage `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

var lokiHeaders = map[string]string{"X-Loki-Response-Encoding-Flags": "categorize-labels"}

// Logs returns the lines matching the filter in the range, at most limit of them. truncated says there were more (a read
// that returns exactly limit lines because that is all there are is not truncated).
func (c *Client) Logs(ctx context.Context, s Scope, f LogFilter, tr TimeRange, limit int) (entries []LogEntry, truncated bool, err error) {
	if err := s.needSignal(SignalLogs); err != nil {
		return nil, false, err
	}
	q, err := f.logQL(s)
	if err != nil {
		return nil, false, err
	}
	return c.lokiQuery(ctx, s, q, f.Backward, tr, limit)
}

// RawLogQuery runs a LogQL query exactly as written. Only a Scope with no namespace or cluster limit may do this.
func (c *Client) RawLogQuery(ctx context.Context, s Scope, query string, backward bool, tr TimeRange, limit int) ([]LogEntry, bool, error) {
	if err := s.needSignal(SignalLogs); err != nil {
		return nil, false, err
	}
	if err := s.needUnrestricted("a LogQL query"); err != nil {
		return nil, false, err
	}
	if query == "" || len(query) > 4096 {
		return nil, false, badRequest("query is required and at most 4096 characters")
	}
	return c.lokiQuery(ctx, s, query, backward, tr, limit)
}

func (c *Client) lokiQuery(ctx context.Context, s Scope, query string, backward bool, tr TimeRange, limit int) ([]LogEntry, bool, error) {
	if err := tr.within(storeLoki, MaxLogWindow); err != nil {
		return nil, false, err
	}
	dir := "forward"
	if backward {
		dir = "backward"
	}
	var res lokiStreams
	err := c.get(ctx, storeLoki, c.Loki, "/loki/api/v1/query_range", url.Values{
		"query": {query}, "start": {unixNano(tr.From)}, "end": {unixNano(tr.To)}, "limit": {strconv.Itoa(limit + 1)}, "direction": {dir}, // (one more: see MetricNames)
	}, lokiHeaders, &res)
	if err != nil {
		return nil, false, err
	}
	if res.Status != "success" || (res.Data.ResultType != "streams" && res.Data.ResultType != "") {
		return nil, false, badRequest("that LogQL query does not return log lines (it returned %q)", res.Data.ResultType)
	}
	var out []LogEntry
	for _, st := range res.Data.Result {
		for _, v := range st.Values {
			e, ok := lokiEntry(st.Stream, v)
			if !ok {
				continue
			}
			if !s.NamespaceVisible(e.Namespace) || !s.ClusterVisible(e.Cluster) {
				continue
			}
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if backward {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Time.Before(out[j].Time)
	})
	if len(out) > limit {
		return out[:limit], true, nil
	}
	return out, false, nil
}

func lokiEntry(stream map[string]string, v []json.RawMessage) (LogEntry, bool) {
	if len(v) < 2 {
		return LogEntry{}, false
	}
	var ns, line string
	if json.Unmarshal(v[0], &ns) != nil || json.Unmarshal(v[1], &line) != nil {
		return LogEntry{}, false
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return LogEntry{}, false
	}
	meta := map[string]string{}
	if len(v) > 2 {
		var extra struct {
			StructuredMetadata map[string]string `json:"structuredMetadata"`
		}
		if json.Unmarshal(v[2], &extra) == nil {
			meta = extra.StructuredMetadata
		}
	}
	get := func(k string) string {
		if x := meta[k]; x != "" {
			return x
		}
		return stream[k]
	}
	e := LogEntry{
		Time: time.Unix(0, n).UTC(), Line: line,
		Severity: firstNonEmpty(get(lokiSeverity), get("detected_level"), get("level")),
		TraceID:  get(lokiTraceID), SpanID: get(lokiSpanID),
		Service: stream[lblService], Namespace: stream[lblNamespace], Pod: stream[lblPod], Cluster: get(lblCluster),
		Labels: stream,
	}
	e.Category = LogCategory(e.Namespace)
	if len(meta) > 0 {
		e.Metadata = meta
	}
	return e, true
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
