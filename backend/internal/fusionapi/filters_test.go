package fusionapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func TestTracesCanBeFoundByPodAndNode(t *testing.T) {
	q, err := TraceFilter{Pod: "web-0", Node: "n1"}.traceQL(AllSignals())
	if err != nil || !strings.Contains(q, `resource.k8s.pod.name = "web-0"`) || !strings.Contains(q, `resource.k8s.node.name = "n1"`) {
		t.Fatalf("%v %s", err, q)
	}
	if q, err := (TraceFilter{Pod: `a"b`}).traceQL(AllSignals()); err != nil || !strings.Contains(q, `"a\"b"`) {
		t.Fatalf("a quote in a value is escaped, not let through: %v %s", err, q)
	}
}

// A service.name is shared by every service of that name; the overview of one in a given namespace reads that one alone, and one
// outside the caller's namespaces is not found.
func TestAServicesOverviewCanBeOfTheOneInANamespace(t *testing.T) {
	f := newFake(t)
	var mu sync.Mutex
	var promQs, lokiQs, tempoQs []string
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		promQs = append(promQs, formOf(r)["match[]"]...)
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": []string{"up"}})
	}
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lokiQs = append(lokiQs, formOf(r).Get("query"))
		mu.Unlock()
		writeJSON(w, lokiResult())
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		tempoQs = append(tempoQs, formOf(r).Get("q"))
		mu.Unlock()
		writeJSON(w, map[string]any{"traces": []any{}})
	}
	if _, err := f.client().ServiceOverview(context.Background(), AllSignals(), AppMember{Name: "web", Namespace: "shop"}, rangeAll); err != nil {
		t.Fatal(err)
	}
	for _, q := range append(append(promQs, lokiQs...), tempoQs...) {
		if !strings.Contains(q, "shop") {
			t.Fatalf("a read of web not narrowed to namespace shop: %s", q)
		}
	}
	if len(promQs) == 0 || len(lokiQs) == 0 || len(tempoQs) == 0 {
		t.Fatalf("each store is read: %d %d %d", len(promQs), len(lokiQs), len(tempoQs))
	}
	_, err := f.client().ServiceOverview(context.Background(), limited, AppMember{Name: "web", Namespace: "secret"}, rangeAll)
	var e *Error
	if !errors.As(err, &e) || e.Status != http.StatusNotFound {
		t.Fatalf("a namespace outside the token's: %v", err)
	}
}
