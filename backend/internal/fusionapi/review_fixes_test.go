package fusionapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestANonFiniteDoubleAttributeDoesNotBreakTheTrace(t *testing.T) {
	for in, want := range map[string]any{`{"doubleValue":1.5}`: 1.5, `{"doubleValue":"NaN"}`: "NaN", `{"doubleValue":"-Infinity"}`: "-Infinity"} {
		var v otlpValue
		if err := json.Unmarshal([]byte(in), &v); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if got := v.any(); got != want {
			t.Errorf("%s = %#v", in, got)
		}
		if _, err := json.Marshal(v.any()); err != nil {
			t.Errorf("%s cannot be written back: %v", in, err)
		}
	}
}

func TestAFocusedUnrestrictedCallerStillSeesTheRootOfATrace(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"traces": []any{map[string]any{"traceID": traceHex, "rootServiceName": "gateway", "rootTraceName": "GET /",
			"startTimeUnixNano": "1791200000000000000", "durationMs": 120}}})
	}
	fs, err := AllSignals().FocusOn(&testGroups()[0])
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := f.client().SearchTraces(context.Background(), fs, TraceFilter{}, rangeAll, 20)
	if err != nil || len(got) != 1 || got[0].RootService != "gateway" || got[0].DurationMs != 120 {
		t.Fatalf("a choice of application must not hide what an unrestricted caller may see: %+v %v", got, err)
	}
	if q := f.last("/api/search").Get("q"); !strings.Contains(q, `resource.service.name`) || !strings.Contains(q, `resource.k8s.namespace.name = "shop"`) {
		t.Fatalf("but the search is still narrowed to the application: %s", q)
	}
}

func TestTimesAndDurationsOutOfRangeAreRefusedPlainly(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	for _, from := range []string{"1696000000000", "Inf", "1e30", "NaN"} {
		_, err := ParseRange(from, "", now)
		if statusOf(err) != http.StatusBadRequest {
			t.Errorf("from=%s: %v", from, err)
		}
	}
	if _, err := ParseRange("1696000000000", "", now); err == nil || !strings.Contains(err.Error(), "milliseconds") {
		t.Errorf("epoch milliseconds are named as such: %v", err)
	}
	for _, d := range []string{"1e30", "1e10", "NaN", "-1"} {
		if v, err := DurationParam(d); err == nil || v != 0 {
			t.Errorf("duration %s = %v, %v", d, v, err)
		}
	}
	if _, err := ChooseStep(TimeRange{From: now.Add(-time.Hour), To: now}, 500*time.Millisecond, 120); err == nil || !strings.Contains(err.Error(), "at least one second") {
		t.Errorf("a step under a second: %v", err)
	}
	if v, err := DurationParam("1.5"); err != nil || v != 1500*time.Millisecond {
		t.Errorf("%v %v", v, err)
	}
}

func TestFusedMetricsAndContextLogsStayInTheClustersOfTheResource(t *testing.T) {
	f := newFake(t)
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []any{}}})
	}
	r := &Resource{Service: "cart", Namespace: "shop", Pod: "cart-0", Cluster: "cl-1"}
	c := f.client()
	if _, _, err := resourceMetrics(context.Background(), r, FuseOptions{MaxSeries: 5, MetricViews: MetricViews{App: true, Pod: true}}, newMetricReads(c, AllSignals(), rangeAll, time.Minute, 5)); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	var queries []string
	for _, req := range f.reqs {
		queries = append(queries, req.URL.Query().Get("query"))
	}
	f.mu.Unlock()
	if len(queries) != 2 {
		t.Fatalf("%v", queries)
	}
	for _, q := range queries {
		if !strings.Contains(q, `ikhnos_cluster_id="cl-1"`) {
			t.Errorf("a resource of cl-1 read the series of every cluster: %s", q)
		}
	}
	if _, _, err := c.Logs(context.Background(), AllSignals(), LogFilter{Service: r.Service, Namespace: r.Namespace, Pod: r.Pod, Cluster: r.Cluster, NoTrace: true}, rangeAll, 10); err != nil {
		t.Fatal(err)
	}
}

func TestTheInfoSeriesIsNotCountedAsTelemetry(t *testing.T) {
	m, err := MetricFilter{Service: "cart"}.matchers(AllSignals())
	if err != nil || !strings.Contains(strings.Join(m, ","), `__name__!="ikhnos_application_info"`) {
		t.Fatalf("%v %v", m, err)
	}
	m, _ = MetricFilter{Name: AppInfoMetric}.matchers(AllSignals())
	if strings.Contains(strings.Join(m, ","), `!=`) {
		t.Fatalf("asked for by name, it is readable: %v", m)
	}
}

func TestAVolumeNameWithADotMakesValidPromQL(t *testing.T) {
	f := newFake(t)
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{}}})
	}
	if _, err := f.client().VolumeUsage(context.Background(), AllSignals(), []string{"data.vol-0"}); err != nil {
		t.Fatal(err)
	}
	q := f.last("/api/v1/query").Get("query")
	if strings.Contains(q, `"data\.vol-0"`) || !strings.Contains(q, `data\\.vol-0`) {
		t.Fatalf("a backslash in a PromQL string must itself be escaped: %s", q)
	}
}

// Tempo's answer is checked once more for a limited caller, as a trace read whole is: a matched span whose namespace or
// cluster the caller cannot see is neither counted nor named, and a hit with nothing left is not listed.
func TestATraceSearchDropsMatchedSpansTheScopeCannotSee(t *testing.T) {
	span := func(start, svc, ns, cl string) map[string]any {
		return map[string]any{"startTimeUnixNano": start, "durationNanos": "10000000",
			"attributes": []any{kv("service.name", svc), kv("k8s.namespace.name", ns), kv("ikhnos.cluster.id", cl)}}
	}
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"traces": []any{
			map[string]any{"traceID": traceHex, "rootServiceName": "gateway", "rootTraceName": "GET /",
				"startTimeUnixNano": "1791200000000000000", "durationMs": 500,
				"spanSet": map[string]any{"matched": 3, "spans": []any{
					span("1791200000100000000", "cart", "shop", "cl-1"),
					span("1791200000000000000", "vault", "secrets", "cl-1"),
					span("1791200000300000000", "ledger", "pay", "cl-2")}}},
			map[string]any{"traceID": "00000000000000000000000000000002", "rootServiceName": "vault", "rootTraceName": "GET /secret",
				"startTimeUnixNano": "1791200000000000000", "durationMs": 50,
				"spanSet": map[string]any{"matched": 1, "spans": []any{span("1791200000000000000", "vault", "secrets", "cl-1")}}},
		}})
	}
	got, _, err := f.client().SearchTraces(context.Background(), limited, TraceFilter{}, rangeAll, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TraceID != traceHex {
		t.Fatalf("a hit with no span the caller may see was listed: %+v", got)
	}
	g := got[0]
	if g.MatchedSpans != 1 || len(g.Services) != 1 || g.Services[0] != "cart" || g.RootService != "" || g.RootName != "" {
		t.Fatalf("the summary still tells of spans outside the scope: %+v", g)
	}
	if !g.Start.Equal(time.Unix(0, 1791200000100000000)) || g.DurationMs != 10 {
		t.Fatalf("the extent reaches spans outside the scope: %v %v", g.Start, g.DurationMs)
	}
	full, _, err := f.client().SearchTraces(context.Background(), AllSignals(), TraceFilter{}, rangeAll, 20)
	if err != nil || len(full) != 2 || full[0].MatchedSpans+full[1].MatchedSpans != 4 {
		t.Fatalf("an unlimited caller sees every hit as Tempo gave it: %+v %v", full, err)
	}
}

// The rights are checked by the stream's labels, which the agent stamped and Loki's filter matched, not by a record's
// own attributes, which are the sender's. A line whose structured metadata claims another namespace or cluster than its
// stream is not given to a limited caller, and an unlimited one is told it as its stream has it.
func TestALogLineIsPlacedByItsStreamAndNotByWhatItsRecordClaims(t *testing.T) {
	line := func(text string, md map[string]string) []any {
		return []any{map[string]string{"service_name": "cart", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"},
			[]any{"1791200000000000000", text, map[string]any{"structuredMetadata": md}}}
	}
	f := newFake(t)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lokiResult(
			line("plain", map[string]string{"trace_id": traceHex}),
			line("other cluster", map[string]string{"ikhnos_cluster_id": "cl-2"}),
			line("other namespace", map[string]string{"k8s_namespace_name": "secret"}),
			line("same", map[string]string{"ikhnos_cluster_id": "cl-1", "k8s_namespace_name": "shop"}),
		))
	}
	got, _, err := f.client().Logs(context.Background(), limited, LogFilter{}, rangeAll, 10)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, e := range got {
		texts = append(texts, e.Line)
	}
	if strings.Join(texts, ",") != "plain,same" {
		t.Fatalf("a limited caller was given lines whose record names another place: %v", texts)
	}
	all, _, err := f.client().Logs(context.Background(), AllSignals(), LogFilter{}, rangeAll, 10)
	if err != nil || len(all) != 4 {
		t.Fatalf("%+v %v", all, err)
	}
	for _, e := range all {
		if e.Cluster != "cl-1" || e.Namespace != "shop" {
			t.Fatalf("an unlimited caller was told the record's place rather than the stream's: %+v", e)
		}
	}
}

// Older lines carry the cluster id only as structured metadata; that is still read when the stream has none.
func TestALogLineWhoseStreamHasNoClusterIsPlacedByItsMetadata(t *testing.T) {
	f := newFake(t)
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lokiResult([]any{map[string]string{"service_name": "cart", "k8s_namespace_name": "shop"},
			[]any{"1791200000000000000", "old", map[string]any{"structuredMetadata": map[string]string{"ikhnos_cluster_id": "cl-1"}}}}))
	}
	got, _, err := f.client().Logs(context.Background(), limited, LogFilter{}, rangeAll, 10)
	if err != nil || len(got) != 1 || got[0].Cluster != "cl-1" {
		t.Fatalf("%+v %v", got, err)
	}
}

// Tempo fills spanSet, the older form of spanSets, with a copy of the first set: a hit that has both counts its matched
// spans once.
func TestATraceSearchHitWithBothFormsOfItsSpanSetsCountsItsSpansOnce(t *testing.T) {
	f := newFake(t)
	set := map[string]any{"matched": 1, "spans": []any{map[string]any{"startTimeUnixNano": "1791200000050000000",
		"durationNanos": "30000000", "attributes": []any{kv("service.name", "cart")}}}}
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"traces": []any{map[string]any{"traceID": traceHex,
			"startTimeUnixNano": "1791200000000000000", "durationMs": 120, "spanSet": set, "spanSets": []any{set}}}})
	}
	got, _, err := f.client().SearchTraces(context.Background(), AllSignals(), TraceFilter{}, rangeAll, 20)
	if err != nil || len(got) != 1 || got[0].MatchedSpans != 1 {
		t.Fatalf("%+v %v", got, err)
	}
}
