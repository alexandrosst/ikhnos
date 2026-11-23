package fusionapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// A fused read narrowed to an application joins only that application's resources, while the trace stays whole; and every
// resource says which applications it is part of.
func TestAFusedReadNarrowedToAnApplicationJoinsOnlyItsOwnResources(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	var mu sync.Mutex
	var promQs, lokiQs []string
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lokiQs = append(lokiQs, formOf(r).Get("query"))
		mu.Unlock()
		writeJSON(w, lokiResult())
	}
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		promQs = append(promQs, formOf(r).Get("query"))
		mu.Unlock()
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}
	shop := AppGroup{ID: "app-shop", Name: "Shop", Members: []AppMember{{Name: "cart", Namespace: "shop", Cluster: "cl-1"}}}
	pay := AppGroup{ID: "app-pay", Name: "Payments", Members: []AppMember{{Name: "billing", Namespace: "pay", Cluster: "cl-1"}}}
	qs, err := ParsePromQueries([]string{
		`cpu=rate(container_cpu_usage_seconds_total{k8s_pod_name="${pod}"}[1m])`,
		`app=sum(up * on(service_name) group_left ikhnos_application_info{application_id="${application_id}"})`,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Logs: true, ContextLogs: true, Metrics: true, PromQL: qs,
		Applications: []AppGroup{shop, pay}, Application: &shop})
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Spans) != 3 {
		t.Fatalf("the trace stays whole: %d spans", len(got.Spans))
	}
	if got.Application == nil || got.Application.ID != "app-shop" {
		t.Fatalf("application = %+v", got.Application)
	}
	for _, q := range append(append([]string{}, promQs...), lokiQs...) {
		if strings.Contains(q, "billing") || strings.Contains(q, "audit") {
			t.Fatalf("a read about a resource outside the application: %s", q)
		}
	}
	all := strings.Join(promQs, "\n")
	if !strings.Contains(all, `k8s_pod_name="cart-1"`) || !strings.Contains(all, `application_id="app-shop"`) {
		t.Fatalf("the application's own reads are missing:\n%s", all)
	}
	if !strings.Contains(strings.Join(lokiQs, "\n"), "cart") {
		t.Fatalf("no log read for cart: %v", lokiQs)
	}
	tags := map[string]string{}
	for _, r := range got.Resources {
		var names []string
		for _, a := range r.Applications {
			names = append(names, a.Name)
		}
		tags[r.Service] = strings.Join(names, ",")
	}
	if tags["cart"] != "Shop" || tags["billing"] != "Payments" || tags["audit"] != "" {
		t.Fatalf("each resource names the applications it is part of: %v", tags)
	}
	if !strings.Contains(strings.Join(got.Warnings, "\n"), "2 of the trace's resources are not part of it") {
		t.Fatalf("the answer must say what was left out: %v", got.Warnings)
	}
}

func TestApplicationVariablesNeedAnApplication(t *testing.T) {
	qs, err := ParsePromQueries([]string{`app=count(ikhnos_application_info{application="${application}"})`})
	if err != nil {
		t.Fatal(err)
	}
	f := newFake(t)
	_, err = f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{PromQL: qs})
	var e *Error
	if !errors.As(err, &e) || e.Status != http.StatusBadRequest || !strings.Contains(e.Msg, "application=") {
		t.Fatalf("err = %v", err)
	}
	if len(f.reqs) != 0 {
		t.Fatalf("a mistake of the caller's is found before any store is asked: %v", f.paths())
	}
}

func TestAMemberMatchesByWorkloadAndOnlyInItsOwnPlace(t *testing.T) {
	m := AppMember{Name: "checkout", Namespace: "shop", Cluster: "cl-1", Aliases: []string{"co"}}
	res := func(svc, ns, cl, deploy string) *Resource {
		r := &Resource{Service: svc, Namespace: ns, Cluster: cl, Attributes: map[string]any{}}
		if deploy != "" {
			r.Attributes["k8s.deployment.name"] = deploy
		}
		return r
	}
	for _, tc := range []struct {
		r    *Resource
		want match
	}{
		{res("checkout", "shop", "cl-1", ""), byPlace},
		{res("co", "shop", "cl-1", ""), byPlace},                       // an alias
		{res("checkout-service", "shop", "cl-1", "checkout"), byPlace}, // the SDK's own name, the workload's name
		{res("checkout", "blog", "cl-1", ""), noMatch},                 // the same name in another namespace
		{res("checkout", "shop", "cl-2", ""), noMatch},                 // ... or cluster
		{res("checkout", "", "cl-1", ""), byName},                      // a resource that does not say where it is
		{res("cart", "shop", "cl-1", "cart"), noMatch},
	} {
		if got := m.matchResource(tc.r); got != tc.want {
			t.Errorf("%+v (%v): got %v, want %v", tc.r, tc.r.Attributes, got, tc.want)
		}
	}
}

func TestPerResourceQueriesCanNameTheWorkload(t *testing.T) {
	qs, err := ParsePromQueries([]string{`w=kube_deployment_status_replicas_available{deployment="${workload}"}`})
	if err != nil || !qs[0].perResource {
		t.Fatalf("${workload} is a per-resource variable: %v %+v", err, qs)
	}
	expr, skip := qs[0].expand(resourceVars(&Resource{Service: "web", Attributes: map[string]any{"k8s.statefulset.name": "web"}}))
	if skip != "" || expr != `kube_deployment_status_replicas_available{deployment="web"}` {
		t.Fatalf("expr = %q, skip = %q", expr, skip)
	}
	if _, skip := qs[0].expand(resourceVars(&Resource{Service: "web"})); skip == "" {
		t.Fatal("a resource that names no workload is skipped, not queried with an empty name")
	}
}

// In a read narrowed to an application, a query about the whole trace is about the application: it reads the application's own
// series. Kept as written on request.
func TestATraceWideQueryOfAnApplicationsReadReadsTheApplicationOnly(t *testing.T) {
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
	shop := AppGroup{ID: "app-shop", Name: "Shop", Members: []AppMember{{Name: "cart", Namespace: "shop", Cluster: "cl-1"}, {Name: "web", Namespace: "shop", Cluster: "cl-1", Aliases: []string{"web-ui"}}}}
	qs, err := ParsePromQueries([]string{`cpu=sum by (k8s_pod_name) (increase(container_cpu_time_seconds_total[1m]))`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{PromQL: qs, Applications: []AppGroup{shop}, Application: &shop})
	if err != nil {
		t.Fatal(err)
	}
	want := `sum by (k8s_pod_name) (increase(container_cpu_time_seconds_total{ikhnos_cluster_id=~"cl-1",k8s_namespace_name=~"shop",service_name=~"cart|web|web-ui"}[1m]))`
	if len(sent) != 1 || sent[0] != want {
		t.Fatalf("sent %q, want %q", sent, want)
	}
	if len(got.Queries) != 1 || got.Queries[0].Query != want || !strings.Contains(got.Joins[SourcePromQL], "application's series only") {
		t.Fatalf("queries %+v, joins %q", got.Queries, got.Joins[SourcePromQL])
	}

	sent = nil
	if _, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{PromQL: qs, Applications: []AppGroup{shop}, Application: &shop, PromQLAsWritten: true}); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != qs[0].Expr {
		t.Fatalf("as written: %q", sent)
	}
}

// A narrowed query matches the application by service_name only, which cannot be widened to the workload's name without
// changing the query. When it comes back with no series and some resource of the application in the trace is part of it
// by its workload's name alone, the answer says that this may be why; when the resources are the application's by their
// service.name, or the query found series, it does not.
func TestAnEmptyNarrowedQueryWarnsWhenAResourceIsTheApplicationsByItsWorkloadOnly(t *testing.T) {
	f := newFake(t)
	trace := tempoTrace()
	cart := trace["trace"].(map[string]any)["resourceSpans"].([]any)[0].(map[string]any)["resource"].(map[string]any)
	cart["attributes"] = append(cart["attributes"].([]any), kv("k8s.deployment.name", "cart-deploy"))
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, trace) }
	var mu sync.Mutex
	found := false // whether the store has series for the queries
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		res := []any{}
		if found {
			start, _ := strconv.ParseFloat(formOf(r).Get("start"), 64)
			res = append(res, map[string]any{"metric": map[string]string{"service_name": "cart-deploy"},
				"values": [][]any{{start, "1"}}})
		}
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": res}})
	}
	qs, err := ParsePromQueries([]string{`cpu=sum(rate(container_cpu_time_seconds_total[1m]))`,
		`ns=sum(up{k8s_namespace_name="${namespace}"})`})
	if err != nil {
		t.Fatal(err)
	}
	read := func(g AppGroup) *Fused {
		t.Helper()
		got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex,
			FuseOptions{PromQL: qs, Applications: []AppGroup{g}, Application: &g})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	warned := func(got *Fused, query string) bool {
		for _, w := range got.Warnings {
			if strings.HasPrefix(w, "promql "+query+": no series.") && strings.Contains(w, "by service_name only") &&
				strings.Contains(w, "(r1)") { // (the key of the cart resource)
				return true
			}
		}
		return false
	}
	shop := func(member string) AppGroup {
		m := AppMember{Name: member, Namespace: "shop", Cluster: "cl-1"}
		return AppGroup{ID: "app-shop", Name: "Shop", Members: []AppMember{m}}
	}
	byWorkload := shop("cart-deploy")
	got := read(byWorkload)
	if !warned(got, "cpu") || !warned(got, "ns") {
		t.Fatalf("an empty narrowed query of an application known by its workload's name does not say why: %q",
			got.Warnings)
	}
	if !strings.Contains(got.Joins[SourcePromQL], "matched by service_name only") {
		t.Fatalf("the join does not say how a narrowed query matches the application: %s", got.Joins[SourcePromQL])
	}

	if got := read(shop("cart")); warned(got, "cpu") || warned(got, "ns") {
		t.Fatalf("a query that matches the resources' own service_name warns: %q", got.Warnings)
	}
	mu.Lock()
	found = true
	mu.Unlock()
	if got := read(byWorkload); warned(got, "cpu") || warned(got, "ns") {
		t.Fatalf("a narrowed query that found series warns: %q", got.Warnings)
	}
}
