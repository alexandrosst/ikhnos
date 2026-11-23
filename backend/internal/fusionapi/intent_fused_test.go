package fusionapi

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// With application=, the changes are those of the application's resources, as its other joins are: an event about a service
// the warnings say is not read is not joined either.
func TestTheChangesOfAnApplicationsReadAreTheApplications(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTraceOnNode()) }
	at := time.Date(2026, 10, 5, 11, 20, 0, 0, time.UTC)
	ex := &fakeExtras{view: testView(), events: []ChangeEvent{
		{Time: at, Kind: "service-scaled", TargetKind: "service", TargetID: "s-cart", Name: "cart", Cluster: "cl-1"},
		{Time: at, Kind: "service-scaled", TargetKind: "service", TargetID: "s-billing", Name: "billing", Cluster: "cl-1"},
		{Time: at, Kind: "node-status", TargetKind: "node", TargetID: "node-b", Name: "node-b", Cluster: "cl-1"}, // billing's node
	}}
	shop := AppGroup{ID: "app-shop", Name: "Shop", Members: []AppMember{{Name: "cart", Namespace: "shop", Cluster: "cl-1"}}}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Changes: true, Extras: ex, ChangesBefore: time.Hour, Applications: []AppGroup{shop}, Application: &shop})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range got.Changes {
		kinds = append(kinds, e.Kind+":"+e.TargetID)
	}
	if strings.Join(kinds, ",") != "service-scaled:s-cart" {
		t.Fatalf("changes = %v", kinds)
	}
}

// promql_services narrows every query, the trace-wide ones too; and with application=, a per-resource query that only names the
// resource's namespace or cluster is narrowed to the application, as one that names nothing is.
func TestPromQLIsNarrowedToWhatTheReadIsAbout(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	var mu sync.Mutex
	var sent []string
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent = append(sent, formOf(r).Get("query"))
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}
	qs, _ := ParsePromQueries([]string{`g=sum(up)`})
	if _, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{PromQL: qs, PromQLServices: []string{"cart"}}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != `sum(up{service_name=~"cart"})` {
		t.Fatalf("promql_services on a trace-wide query: %q", sent)
	}

	sent = nil
	shop := AppGroup{ID: "app-shop", Name: "Shop", Members: []AppMember{{Name: "cart", Namespace: "shop", Cluster: "cl-1"}}}
	qs, _ = ParsePromQueries([]string{`ns=sum(rate(http_requests_total{k8s_namespace_name="${namespace}"}[1m]))`, `pod=up{k8s_pod_name="${pod}"}`})
	if _, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{PromQL: qs, Applications: []AppGroup{shop}, Application: &shop}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(sent, "\n")
	if !strings.Contains(all, `sum(rate(http_requests_total{ikhnos_cluster_id=~"cl-1",k8s_namespace_name="shop",k8s_namespace_name=~"shop",service_name=~"cart"}[1m]))`) {
		t.Fatalf("a namespace-wide query of an application's read: %s", all)
	}
	if !strings.Contains(all, `up{k8s_pod_name="cart-1"}`) {
		t.Fatalf("a query about one pod is left as it is: %s", all)
	}
}

// With a span filter, what is joined to the resources is what the kept spans ran on.
func TestASpanFilterNarrowsWhatIsJoinedToTheResources(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	var mu sync.Mutex
	var sent []string
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sent = append(sent, formOf(r).Get("query"))
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}
	if _, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Metrics: true, Spans: SpanFilter{Service: "cart"}}); err != nil {
		t.Fatal(err)
	}
	for _, q := range sent {
		if strings.Contains(q, "billing") || strings.Contains(q, "audit") {
			t.Fatalf("metrics read for a resource no kept span ran on: %s", q)
		}
	}
	if len(sent) == 0 {
		t.Fatal("cart's metrics were not read")
	}
}

// Narrowed to an application, the trace's lines are those of its resources' own places: the same service name elsewhere on the
// trace is not what was asked for.
func TestAnApplicationsTraceLinesAreFromItsOwnPlaces(t *testing.T) {
	lines := []LogEntry{{Service: "cart", Namespace: "shop", Cluster: "cl-1"}, {Service: "cart", Namespace: "other", Cluster: "cl-1"}, {Service: "cart"}}
	looked := []*Resource{{Service: "cart", Namespace: "shop", Cluster: "cl-1"}}
	var kept []LogEntry
	for _, l := range lines {
		if fromOneOf(l, looked) {
			kept = append(kept, l)
		}
	}
	if len(kept) != 2 || kept[0].Namespace != "shop" || kept[1].Namespace != "" {
		t.Fatalf("kept %+v", kept)
	}
}
