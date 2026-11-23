package fusionapi

import (
	"context"
	"encoding/json"
	"net/http"
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
	// (One more line than limit is asked for: see MetricNames.)
	lines, err := c.lokiRaw(ctx, s, query, backward, tr, limit+1)
	if err != nil {
		return nil, false, err
	}
	out, truncated := cutLines(lines, backward, limit)
	return out, truncated, nil
}

// lokiLine is one value of Loki's answer before anything is left out of it. decoded says it could be read at all; kept
// that the caller may also be given it (see lokiEntry and the Scope's checks). A read of several resources at once keeps
// the lines it may not hand out, because they still took a place in Loki's limit, as they would have in a read of each.
type lokiLine struct {
	e       LogEntry
	decoded bool
	kept    bool
}

// lokiRaw asks Loki for at most limit lines (sent as it is) and returns every value of its answer, in the answer's order.
func (c *Client) lokiRaw(ctx context.Context, s Scope, query string, backward bool, tr TimeRange,
	limit int) ([]lokiLine, error) {
	if err := tr.within(storeLoki, MaxLogWindow); err != nil {
		return nil, err
	}
	dir := "forward"
	if backward {
		dir = "backward"
	}
	var res lokiStreams
	q := url.Values{"query": {query}, "start": {unixNano(tr.From)}, "end": {unixNano(tr.To)}, "limit": {strconv.Itoa(limit)},
		"direction": {dir}}
	// A long question (the lines of many resources at once) goes as a form, which Loki takes alike: a URL that long may be
	// refused by a proxy on the way (see maxGetQuery).
	method := http.MethodGet
	if len(q.Encode()) > maxGetQuery {
		method = http.MethodPost
	}
	err := c.call(ctx, method, storeLoki, c.Loki, "/loki/api/v1/query_range", q, lokiHeaders, &res)
	if err != nil {
		return nil, err
	}
	if res.Status != "success" || (res.Data.ResultType != "streams" && res.Data.ResultType != "") {
		return nil, badRequest("that LogQL query does not return log lines (it returned %q)", res.Data.ResultType)
	}
	restricted := !s.Unrestricted()
	var out []lokiLine
	for _, st := range res.Data.Result {
		for _, v := range st.Values {
			e, mixed, ok := lokiEntry(st.Stream, v)
			kept := ok && !(mixed && restricted) && s.visibleAt(e.Namespace, e.Cluster)
			out = append(out, lokiLine{e: e, decoded: ok, kept: kept})
		}
	}
	return out, nil
}

// cutLines is what a read of limit lines, which asked Loki for limit+1, makes of the answer: the lines the caller may be
// given, newest first when backward (oldest first otherwise), at most limit of them, and whether there were more.
func cutLines(lines []lokiLine, backward bool, limit int) ([]LogEntry, bool) {
	var out []LogEntry
	for _, l := range lines {
		if l.kept {
			out = append(out, l.e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if backward {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].Time.Before(out[j].Time)
	})
	return cutAt(out, limit)
}

// lokiEntry decodes one line of a stream. mixed says the line's structured metadata names a namespace or a cluster
// other than its stream's, which a caller whose access is limited is not given the line for (see rightsLabel).
func lokiEntry(stream map[string]string, v []json.RawMessage) (e LogEntry, mixed, ok bool) {
	if len(v) < 2 {
		return LogEntry{}, false, false
	}
	var ts, line string
	if json.Unmarshal(v[0], &ts) != nil || json.Unmarshal(v[1], &line) != nil {
		return LogEntry{}, false, false
	}
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return LogEntry{}, false, false
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
	// The labels the rights are checked by come from the stream. A record's own attributes, which Loki keeps as structured
	// metadata, are whatever its sender wrote; the stream's labels are what the agent stamped on the way in, and what
	// Loki's own filter on the query matched. The namespace is a stream label only; the cluster id is one too, but older
	// lines carry it as structured metadata alone (see logServices), so that is the fallback. A line whose metadata names
	// another namespace or cluster than its stream is told as its stream has it, and not at all to a limited caller, which
	// is not to be shown a line that claims to be from elsewhere.
	ns, nsMixed := rightsLabel(stream, meta, lblNamespace, false)
	cl, clMixed := rightsLabel(stream, meta, lblCluster, true)
	e = LogEntry{
		Time: time.Unix(0, n).UTC(), Line: line,
		Severity: firstNonEmpty(get(lokiSeverity), get("detected_level"), get("level")),
		TraceID:  get(lokiTraceID), SpanID: get(lokiSpanID),
		Service: stream[lblService], Namespace: ns, Pod: stream[lblPod], Cluster: cl,
		Labels: stream,
	}
	e.Category = LogCategory(e.Namespace)
	if len(meta) > 0 {
		e.Metadata = meta
	}
	return e, nsMixed || clMixed, true
}

// rightsLabel is the value of a label the rights are checked by: the stream's, else (when orMeta) the structured
// metadata's. mixed says both carry one and they differ.
func rightsLabel(stream, meta map[string]string, k string, orMeta bool) (v string, mixed bool) {
	st, md := stream[k], meta[k]
	switch {
	case st != "":
		return st, md != "" && md != st
	case orMeta:
		return md, false
	}
	return "", false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
