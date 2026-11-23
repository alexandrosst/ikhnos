package fusionapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// bigTrace is one resource with n short spans and, last, one error span an hour later that lasts an hour.
func bigTrace(n int) map[string]any {
	base := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC).UnixNano()
	var spans []any
	for i := 0; i < n; i++ {
		spans = append(spans, map[string]any{"traceId": traceHex, "spanId": strconv.FormatInt(int64(0x1000000000000000+i), 16), "name": "short",
			"startTimeUnixNano": json.Number(itoa(base + int64(i))), "endTimeUnixNano": json.Number(itoa(base + int64(i) + 1e6)), "status": map[string]any{"code": "STATUS_CODE_UNSET"}})
	}
	spans = append(spans, map[string]any{"traceId": traceHex, "spanId": "2000000000000000", "name": "failing",
		"startTimeUnixNano": json.Number(itoa(base + int64(time.Hour))), "endTimeUnixNano": json.Number(itoa(base + int64(2*time.Hour))), "status": map[string]any{"code": "STATUS_CODE_ERROR"}})
	return map[string]any{"trace": map[string]any{"resourceSpans": []any{map[string]any{
		"resource":   map[string]any{"attributes": []any{kv("service.name", "web"), kv("k8s.namespace.name", "shop"), kv("ikhnos.cluster.id", "cl-1")}},
		"scopeSpans": []any{map[string]any{"spans": spans}},
	}}}}
}

// Past MaxSpans the spans are cut, but the totals stay the whole trace's, and the error span is among those kept: it is often what
// the reader of a large trace is after, and the window every other signal is read in runs to the trace's real end.
func TestALargeTraceKeepsItsTotalsAndItsErrorSpans(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, bigTrace(MaxSpans)) }
	got, err := f.client().Trace(context.Background(), AllSignals(), traceHex)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.SpanCount != MaxSpans+1 || len(got.Spans) != MaxSpans {
		t.Fatalf("truncated %v, %d spans of %d", got.Truncated, len(got.Spans), got.SpanCount)
	}
	if got.ErrorCount != 1 || got.DurationMs < float64(2*time.Hour/time.Millisecond) {
		t.Fatalf("errors %d, duration %v ms: the totals must be the whole trace's", got.ErrorCount, got.DurationMs)
	}
	last := got.Spans[len(got.Spans)-1]
	if last.Name != "failing" {
		t.Fatalf("the error span was cut away; last kept is %q", last.Name)
	}
	for i := 1; i < len(got.Spans); i++ {
		if got.Spans[i].Start.Before(got.Spans[i-1].Start) {
			t.Fatal("the spans kept are no longer in start order")
		}
	}
}

// The point budget of the spans is spent only on spans the answer keeps, and metrics and queries share it.
func TestTheSpanBudgetIsSpentOnlyOnTheSpansKept(t *testing.T) {
	series := func() []MetricSeries {
		var p []Point
		for i := 0; i < 100; i++ {
			p = append(p, Point{float64(1000 + i), 1})
		}
		return []MetricSeries{{Name: "m", Points: p}}
	}
	big, small := &Resource{Key: "big", Service: "big", Metrics: series()}, &Resource{Key: "small", Service: "small", Metrics: series()}
	f := &Fused{Trace: &Trace{Resources: []*Resource{big, small}}}
	start := time.Unix(1000, 0)
	for i := 0; i < maxSpanPoints/100+5; i++ {
		f.Spans = append(f.Spans, &Span{Resource: "big", Service: "big", Start: start, End: start.Add(100 * time.Second)})
	}
	one := &Span{Resource: "small", Service: "small", Start: start, End: start.Add(100 * time.Second)}
	f.Spans = append(f.Spans, one)
	hit, _ := attachToSpans(f, FuseOptions{Spans: SpanFilter{Service: "small"}}, true, false)
	if hit || len(one.Metrics) != 1 || len(one.Metrics[0].Points) != 100 {
		t.Fatalf("budget hit %v, the kept span has %+v", hit, one.Metrics)
	}
	if len(f.Spans[0].Metrics) != 0 {
		t.Fatal("a span the filter leaves out was given points")
	}

	// Shared: queries take what metrics left.
	for _, sp := range f.Spans {
		sp.Metrics = nil
	}
	for _, r := range []*Resource{big, small} {
		r.Queries = []QueryResult{{Name: "q", Series: series()}}
	}
	mHit, qHit := attachToSpans(f, FuseOptions{}, true, true)
	if !mHit && !qHit {
		t.Fatal("two kinds of points beyond one budget were not reported")
	}
	total := 0
	for _, sp := range f.Spans {
		for _, m := range sp.Metrics {
			total += len(m.Points)
		}
		for _, q := range sp.Queries {
			for _, m := range q.Series {
				total += len(m.Points)
			}
		}
	}
	if total > maxSpanPoints {
		t.Fatalf("%d points on the spans, more than the %d of one read", total, maxSpanPoints)
	}
}

// One resource's metric read failing costs that resource, not the span metrics of the others. (Prometheus refuses each
// query that names billing, so it refuses the query of every view at once too, and the views are then read one by one.)
func TestOneResourceFailingKeepsTheOthersSpanMetrics(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		q := formOf(r).Get("query")
		if strings.Contains(q, "billing") {
			http.Error(w, `{"error":"bad query"}`, http.StatusBadRequest)
			return
		}
		start, _ := strconv.ParseFloat(formOf(r).Get("start"), 64)
		end, _ := strconv.ParseFloat(formOf(r).Get("end"), 64)
		var vals [][]any
		for t := start; t <= end; t += 15 {
			vals = append(vals, []any{t, "1"})
		}
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{
			map[string]any{"metric": map[string]string{"__name__": "cpu", "service_name": "cart", "k8s_namespace_name": "shop",
				"ikhnos_cluster_id": "cl-1"}, "values": vals}}}})
	}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Metrics: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Sources[SignalMetrics] != SourceError {
		t.Fatalf("a failed read is still reported: %v", got.Sources)
	}
	for _, sp := range got.Spans {
		if sp.Name == "GET /cart" && len(sp.Metrics) == 0 {
			t.Fatalf("cart's span lost its metrics because billing's read failed: %+v", sp)
		}
	}
}

// The queries about the whole trace are evaluated before the per-resource ones fill the read's limit, and an expansion past it
// says so in its own result.
func TestTheTraceWideQueriesAreNotCrowdedOutByPerPodOnes(t *testing.T) {
	var looked []*Resource
	for i := 0; i < maxPromRuns; i++ {
		looked = append(looked, &Resource{Key: "r" + itoa(int64(i)), Service: "web", Namespace: "shop", Pod: "web-" + itoa(int64(i)), Cluster: "cl-1"})
	}
	qs, err := ParsePromQueries([]string{`a=up{k8s_pod_name="${pod}"}`, `b=rate(x{k8s_pod_name="${pod}"}[1m])`, `g=sum(up)`})
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	var mu sync.Mutex
	asked := map[string]bool{}
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		asked[formOf(r).Get("query")] = true
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}
	tr := &Trace{TraceID: traceHex, Start: rangeAll.From, End: rangeAll.To}
	global, warns, err := f.client().fusePromQL(context.Background(), AllSignals(), tr, looked, FuseOptions{PromQL: qs, MaxSeries: 5}, rangeAll, time.Minute, make(chan struct{}, 4), func(fn func()) { fn() })
	if err != nil {
		t.Fatalf("the read's own limit is not a store failure: %v", err)
	}
	if len(global) != 1 || global[0].Name != "g" || global[0].Error != "" || !asked["sum(up)"] {
		t.Fatalf("the trace-wide query was not evaluated: %+v", global)
	}
	if len(asked) != maxPromRuns {
		t.Fatalf("%d queries asked, the limit is %d", len(asked), maxPromRuns)
	}
	notEvaluated := 0
	for _, r := range looked {
		for _, q := range r.Queries {
			if strings.HasPrefix(q.Error, "not evaluated") {
				notEvaluated++
			}
		}
	}
	if notEvaluated != maxPromRuns+1 || !strings.Contains(strings.Join(warns, "\n"), "b") {
		t.Fatalf("%d expansions say they were not evaluated; warnings %v", notEvaluated, warns)
	}
}

// The nodes' system lines are one read; when the store fails it (here, with an error of its own), each node's own read
// would have failed the same way, so the part fails as a whole, as any join whose read failed does, and is not read
// again node by node.
func TestTheSystemLinesOfAllTheNodesFailTogether(t *testing.T) {
	f := newFake(t)
	var mu sync.Mutex
	reads := 0
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reads++
		mu.Unlock()
		http.Error(w, "boom", http.StatusInternalServerError)
	}
	res := []*Resource{{Key: "a", Node: "n1", Cluster: "cl-1"}, {Key: "b", Node: "n2", Cluster: "cl-1"}}
	opts := FuseOptions{SystemLogs: true}
	if err := opts.defaults(); err != nil {
		t.Fatal(err)
	}
	sl, warns, err := f.client().systemLogs(context.Background(), AllSignals(), res, opts, rangeAll, make(chan struct{}, 1))
	mu.Lock()
	defer mu.Unlock()
	if err == nil || reads != 1 || len(sl.Entries) != 0 || len(sl.Nodes) != 0 || len(warns) != 0 {
		t.Fatalf("err %v, %d reads, lines %+v, nodes %v, warnings %v", err, reads, sl.Entries, sl.Nodes, warns)
	}
}

// When Loki refuses the read of all the nodes for what it is (a 4xx), the nodes are read one by one as they used to be,
// and one node failing costs that node's lines, not every node's.
func TestOneNodeFailingKeepsTheOtherNodesSystemLines(t *testing.T) {
	f := newFake(t)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		q := formOf(r).Get("query")
		switch {
		case strings.Contains(q, " or "):
			http.Error(w, `{"error":"query too long"}`, http.StatusBadRequest)
			return
		case strings.Contains(q, `"n2"`):
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		writeJSON(w, lokiResult([]any{map[string]string{"k8s_namespace_name": "kube-system"}, []any{"1791200000000000000", "kubelet says hi"}}))
	}
	res := []*Resource{{Key: "a", Node: "n1", Cluster: "cl-1"}, {Key: "b", Node: "n2", Cluster: "cl-1"}}
	opts := FuseOptions{SystemLogs: true}
	if err := opts.defaults(); err != nil {
		t.Fatal(err)
	}
	sl, warns, err := f.client().systemLogs(context.Background(), AllSignals(), res, opts, rangeAll, make(chan struct{}, 1))
	if err != nil || len(sl.Entries) != 1 || len(sl.Nodes) != 1 || sl.Nodes[0] != "n1" || !strings.Contains(strings.Join(warns, ""), "n2") {
		t.Fatalf("err %v, lines %+v, nodes %v, warnings %v", err, sl.Entries, sl.Nodes, warns)
	}
}

func TestAPromQLMarginOfZeroIsNoMarginLikeTheOthers(t *testing.T) {
	opts, err := ParseFuseOptions(url.Values{"promql_pad": {"0"}})
	if err != nil || opts.PromQLPad != zeroPad {
		t.Fatalf("promql_pad=0: %v %v", opts.PromQLPad, err)
	}
}

// A trace no span of which says when it ran has no window to read anything in; nothing is joined, and the answer says why.
func TestATraceWithNoTimedSpanJoinsNothing(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"trace": map[string]any{"resourceSpans": []any{map[string]any{
			"resource":   map[string]any{"attributes": []any{kv("service.name", "web")}},
			"scopeSpans": []any{map[string]any{"spans": []any{map[string]any{"traceId": traceHex, "spanId": "00f067aa0ba902b7", "name": "x", "startTimeUnixNano": json.Number("0"), "endTimeUnixNano": json.Number("0")}}}},
		}}}})
	}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Logs: true, Metrics: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range f.paths() {
		if !strings.HasPrefix(p, "/api/v2/traces/") && !strings.HasPrefix(p, "/api/traces/") {
			t.Fatalf("a store other than the trace store was asked: %s", p)
		}
	}
	if !strings.Contains(strings.Join(got.Warnings, " "), "no span with a start time") {
		t.Fatalf("warnings = %v", got.Warnings)
	}
	if got.Sources[SignalLogs] != SourceOK || got.Sources[SignalMetrics] != SourceOK {
		t.Fatalf("what was asked for says it is read, with nothing in it: %v", got.Sources)
	}
}

func TestAPlaceWithNoNameIsNoPlace(t *testing.T) {
	g := &AppGroup{Members: []AppMember{{Name: "api", Namespace: "shop"}, {Namespace: "data"}}}
	if p := g.places(); len(p) != 1 || p[0].Namespace != "shop" {
		t.Fatalf("places = %+v", p)
	}
}
