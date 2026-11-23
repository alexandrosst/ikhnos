package fusionapi

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// attrsVisible is the second check on a span's own attributes: its namespace and cluster are ones the Scope may see. A
// trace read whole and a trace search both apply it, behind the matchers their queries carry.
func (s Scope) attrsVisible(attrs map[string]any) bool {
	return s.NamespaceVisible(strAttr(attrs, attrNamespace)) && s.ClusterVisible(strAttr(attrs, attrCluster))
}

// buildTrace turns Tempo's resource spans into a Trace, keeping only what the Scope may see.
func buildTrace(id string, rss []otlpResourceSpans, s Scope) *Trace {
	tr := &Trace{TraceID: id, Services: []string{}, Roots: []string{}}
	resIdx := map[string]*Resource{}
	services := map[string]bool{}
	for _, rs := range rss {
		attrs := kvMap(rs.Resource.Attributes)
		ns, cl := strAttr(attrs, attrNamespace), strAttr(attrs, attrCluster)
		if !s.attrsVisible(attrs) {
			continue
		}
		ident := strings.Join([]string{strAttr(attrs, attrService), ns, strAttr(attrs, attrPod), strAttr(attrs, attrInstance), cl}, "\x00")
		res := resIdx[ident]
		if res == nil {
			res = &Resource{Key: "r" + strconv.Itoa(len(resIdx)+1), Service: strAttr(attrs, attrService), Namespace: ns,
				Pod: strAttr(attrs, attrPod), Node: strAttr(attrs, attrNode), Cluster: cl, Attributes: attrs}
			resIdx[ident] = res
			tr.Resources = append(tr.Resources, res)
		}
		var spans []otlpSpan
		for _, ss := range rs.ScopeSpans {
			spans = append(spans, ss.Spans...)
		}
		for _, ss := range rs.LibrarySpans {
			spans = append(spans, ss.Spans...)
		}
		for _, sp := range spans {
			start, end := nanos(sp.Start), nanos(sp.End)
			if end.Before(start) {
				end = start
			}
			out := &Span{
				SpanID: normID(sp.SpanID, 16), ParentSpanID: normID(sp.ParentSpanID, 16), Name: sp.Name, Kind: spanKind(sp.Kind),
				Service: res.Service, Namespace: res.Namespace, Pod: res.Pod, Node: res.Node, Cluster: res.Cluster, Resource: res.Key,
				Start: start, End: end, DurationMs: float64(end.Sub(start)) / float64(time.Millisecond),
				Status: spanStatus(sp.Status.Code), StatusMessage: sp.Status.Message, Attributes: kvMap(sp.Attributes),
			}
			for _, ev := range sp.Events {
				out.Events = append(out.Events, SpanEvent{Time: nanos(ev.Time), Name: ev.Name, Attributes: kvMap(ev.Attributes)})
			}
			tr.Spans = append(tr.Spans, out)
			if res.Service != "" {
				services[res.Service] = true
			}
		}
	}
	sort.SliceStable(tr.Spans, func(i, j int) bool {
		if !tr.Spans[i].Start.Equal(tr.Spans[j].Start) {
			return tr.Spans[i].Start.Before(tr.Spans[j].Start)
		}
		return tr.Spans[i].SpanID < tr.Spans[j].SpanID
	})
	// The totals are those of the whole trace, so they are taken before the spans are cut: the end of a long trace is often where it
	// failed, and the window every other signal is read in is the trace's start to its end.
	tr.SpanCount = len(tr.Spans)
	for _, sp := range tr.Spans {
		// A span with no start time (a sender that left it out) would put the start of the trace in the year 1 and stretch its length,
		// and with it the window of every other signal read around the trace.
		if !sp.Start.IsZero() {
			if tr.Start.IsZero() || sp.Start.Before(tr.Start) {
				tr.Start = sp.Start
			}
			if sp.End.After(tr.End) {
				tr.End = sp.End
			}
		}
		if sp.Status == "error" {
			tr.ErrorCount++
		}
	}
	if len(tr.Spans) > MaxSpans {
		tr.Spans, tr.Truncated = keepSpans(tr.Spans, MaxSpans), true
	}
	byID := make(map[string]*Span, len(tr.Spans))
	for _, sp := range tr.Spans {
		byID[sp.SpanID] = sp
	}
	for _, sp := range tr.Spans {
		// A span whose parent is not here (or is hidden from this Scope) is a root of what is visible.
		if p, ok := byID[sp.ParentSpanID]; !ok || sp.ParentSpanID == "" || p == sp {
			tr.Roots = append(tr.Roots, sp.SpanID)
			// A parent this Scope cannot see is not named: its id would confirm a span exists outside the Scope.
			if !s.Unrestricted() {
				sp.ParentSpanID = ""
			}
		}
	}
	for _, sp := range tr.Spans { // depth: walk up, bounded so a cycle in bad data cannot loop
		d := 0
		for p, ok := byID[sp.ParentSpanID]; ok && p != sp && d < 64; p, ok = byID[p.ParentSpanID] {
			d++
		}
		sp.Depth = d
	}
	tr.DurationMs = float64(tr.End.Sub(tr.Start)) / float64(time.Millisecond)
	for svc := range services {
		tr.Services = append(tr.Services, svc)
	}
	sort.Strings(tr.Services)
	return tr
}

// Trace reads one trace by id. A Scope that limits namespaces or clusters sees only the spans of its own; a trace
// with none of them reads as not found, which is also what a trace that does not exist reads as.
func (c *Client) Trace(ctx context.Context, s Scope, id string) (*Trace, error) {
	if err := s.needSignal(SignalTraces); err != nil {
		return nil, err
	}
	id, err := NormalizeTraceID(id)
	if err != nil {
		return nil, err
	}
	var body traceBody
	if err := c.get(ctx, storeTempo, c.Tempo, "/api/v2/traces/"+id, nil, nil, &body); err != nil {
		// A trace Tempo does not have reads exactly as one this Scope may not see, so the two cannot be told apart.
		var e *Error
		if errors.As(err, &e) && e.Status == http.StatusNotFound {
			return nil, errf(http.StatusNotFound, "no trace %s", id)
		}
		return nil, err
	}
	tr := buildTrace(id, body.resourceSpans(), s)
	if tr.SpanCount == 0 {
		return nil, errf(http.StatusNotFound, "no trace %s", id)
	}
	return tr, nil
}
