package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/landscape"
	"ikhnos/internal/model"
)

// FUSION tokens limited to ikhnos applications, end to end: made through the API, resolved at every request, and held to
// each application's members where they run on every route.
//
// The stores below answer every question with the application's own telemetry mixed with what is near it: the same
// service name in another namespace, another service of the same namespace, telemetry that says no namespace, and the
// system lines of a node. Everything a token of the application must not be given carries the word LEAK.

const leak = "LEAK"

type appStores struct {
	mu                sync.Mutex
	asked             []string // "<store> <path> <query or form>"
	prom, loki, tempo *httptest.Server
}

func (s *appStores) record(name string, r *http.Request) {
	_ = r.ParseForm()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, name+" "+r.URL.Path+" "+r.Form.Encode())
}

func (s *appStores) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asked)
}

// since is what the stores were asked after the n-th question, with the queries decoded.
func (s *appStores) since(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, a := range s.asked[n:] {
		if d, err := url.QueryUnescape(a); err == nil {
			a = d
		}
		out = append(out, a)
	}
	return strings.Join(out, "\n")
}

const appTraceA, appTraceB, appTraceC = "000000000000000000000000000000a1", "000000000000000000000000000000b2", "000000000000000000000000000000c3"

func newAppStores(t *testing.T) *appStores {
	s := &appStores{}
	start := time.Date(2026, 10, 5, 11, 30, 0, 0, time.UTC).UnixNano()
	span := func(id, parent, name string) map[string]any {
		return map[string]any{"traceId": testTraceID, "spanId": id, "parentSpanId": parent, "name": name, "kind": 2,
			"startTimeUnixNano": json.Number(itoa64(start)), "endTimeUnixNano": json.Number(itoa64(start + 40e6)), "status": map[string]any{"code": 0}}
	}
	res := func(sp map[string]any, kvs ...string) any {
		var attrs []any
		for i := 0; i < len(kvs); i += 2 {
			attrs = append(attrs, attrKV(kvs[i], kvs[i+1]))
		}
		return map[string]any{"resource": map[string]any{"attributes": attrs}, "scopeSpans": []any{map[string]any{"spans": []any{sp}}}}
	}
	s.tempo = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record("tempo", r)
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/traces/"):
			jsonOut(w, map[string]any{"trace": map[string]any{"resourceSpans": []any{
				res(span("0000000000000001", "", "LEAK gateway"), "service.name", "gateway", "k8s.namespace.name", "edge", "ikhnos.cluster.id", "cl-1", "k8s.node.name", "n1"),
				res(span("00f067aa0ba902b7", "0000000000000001", "GET /cart"), "service.name", "cart", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1",
					"k8s.pod.name", "cart-1", "k8s.node.name", "n1"),
				res(span("0000000000000003", "00f067aa0ba902b7", "LEAK search"), "service.name", "search", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
				res(span("0000000000000004", "00f067aa0ba902b7", "LEAK other cart"), "service.name", "cart", "k8s.namespace.name", "other", "ikhnos.cluster.id", "cl-1"),
				res(span("0000000000000005", "00f067aa0ba902b7", "LEAK unplaced cart"), "service.name", "cart"),
				res(span("0000000000000006", "00f067aa0ba902b7", "render"), "service.name", "web-7f9", "k8s.deployment.name", "web", "k8s.namespace.name", "shop",
					"ikhnos.cluster.id", "cl-1"),
			}}})
		case r.URL.Path == "/api/search":
			hit := func(id string, kvs ...string) any {
				var attrs []any
				for i := 0; i < len(kvs); i += 2 {
					attrs = append(attrs, attrKV(kvs[i], kvs[i+1]))
				}
				return map[string]any{"traceID": id, "rootServiceName": "LEAK root", "rootTraceName": "LEAK root", "startTimeUnixNano": "1791200000000000000", "durationMs": 5,
					"spanSets": []any{map[string]any{"matched": 1, "spans": []any{map[string]any{"startTimeUnixNano": "1791200000000000000", "durationNanos": "1000", "attributes": attrs}}}}}
			}
			jsonOut(w, map[string]any{"traces": []any{
				hit(appTraceA, "service.name", "cart", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
				hit(appTraceB, "service.name", "cart", "k8s.namespace.name", "other", "ikhnos.cluster.id", "cl-1"),
				hit(appTraceC, "service.name", "search", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
			}})
		default:
			jsonOut(w, map[string]any{"tagValues": []any{map[string]any{"value": "cart"}}})
		}
	}))
	s.loki = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record("loki", r)
		if strings.HasSuffix(r.URL.Path, "/values") {
			jsonOut(w, map[string]any{"status": "success", "data": []string{"cart", "web"}})
			return
		}
		stream := func(line string, kvs ...string) any {
			st := map[string]string{}
			for i := 0; i < len(kvs); i += 2 {
				st[kvs[i]] = kvs[i+1]
			}
			return map[string]any{"stream": st, "values": []any{[]any{"1791200000000000000", line,
				map[string]any{"structuredMetadata": map[string]string{"trace_id": testTraceID, "span_id": "00f067aa0ba902b7"}}}}}
		}
		jsonOut(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "streams", "result": []any{
			stream("cart line", "service_name", "cart", "k8s_namespace_name", "shop", "ikhnos_cluster_id", "cl-1", "k8s_pod_name", "cart-1"),
			stream("web line", "service_name", "web", "k8s_namespace_name", "shop", "ikhnos_cluster_id", "cl-1"),
			stream("LEAK other cart line", "service_name", "cart", "k8s_namespace_name", "other", "ikhnos_cluster_id", "cl-1"),
			stream("LEAK search line", "service_name", "search", "k8s_namespace_name", "shop", "ikhnos_cluster_id", "cl-1"),
			stream("LEAK unplaced line", "service_name", "cart"),
			stream("LEAK system line", "service_name", "coredns", "k8s_namespace_name", "kube-system", "ikhnos_cluster_id", "cl-1", "k8s_node_name", "n1"),
		}}})
	}))
	series := []map[string]string{
		{"__name__": "up", "service_name": "cart", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"},
		{"__name__": "up", "k8s_deployment_name": "web", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"},
		{"__name__": "up", "service_name": "cart", "k8s_namespace_name": "other", "ikhnos_cluster_id": "cl-1", "marker": leak},
		{"__name__": "up", "service_name": "search", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1", "marker": leak},
		{"__name__": "up", "service_name": "cart", "marker": leak},
		{"__name__": "node_load1", "k8s_node_name": "n1", "ikhnos_cluster_id": "cl-1", "marker": leak},
	}
	s.prom = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.record("prom", r)
		switch {
		case r.URL.Path == "/api/v1/label/__name__/values":
			jsonOut(w, map[string]any{"status": "success", "data": []string{"up"}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/label/"):
			jsonOut(w, map[string]any{"status": "success", "data": []string{"cart"}})
		case r.URL.Path == "/api/v1/series":
			jsonOut(w, map[string]any{"status": "success", "data": series})
		case r.URL.Path == "/api/v1/query":
			jsonOut(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": []any{}}})
		default:
			var result []any
			for _, l := range series {
				result = append(result, map[string]any{"metric": l, "values": []any{[]any{1791200000.0, "1"}}})
			}
			jsonOut(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": result}})
		}
	}))
	t.Cleanup(func() { s.prom.Close(); s.loki.Close(); s.tempo.Close() })
	return s
}

// appTokenGroups: Shop is cart and web in shop on cl-1. Search is another application of the same namespace, and Other cart
// a service called cart in namespace other.
func appTokenGroups() []fusionapi.AppGroup {
	return []fusionapi.AppGroup{
		{ID: "app-shop", Name: "Shop", Members: []fusionapi.AppMember{
			{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment"},
			{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment"}}},
		{ID: "app-search", Name: "Search", Members: []fusionapi.AppMember{{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment"}}},
		{ID: "app-other", Name: "Other cart", Members: []fusionapi.AppMember{{ID: "s-cart2", Name: "cart", Namespace: "other", Cluster: "cl-1", Kind: "Deployment"}}},
	}
}

func appTokenView() *fusionapi.TopologyView {
	return &fusionapi.TopologyView{At: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		Services: []fusionapi.TopoService{
			// The services name every application they are in, the token's and others' (annotateApplications).
			{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1", Replicas: 1, Ready: 1, Applications: []string{"Shop", "LEAK secret"}, AppIDs: []string{"app-shop", "app-secret"}},
			{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Replicas: 1, Ready: 1, Applications: []string{"LEAK secret", "Shop"}, AppIDs: []string{"app-secret", "app-shop"}},
			{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1", Replicas: 1, Ready: 1},
			{ID: "s-cart2", Name: "cart", Namespace: "other", Cluster: "cl-1", Replicas: 1, Ready: 1},
			// Another service that has a member's name, in the member's place: not the member.
			{ID: "s-LEAK-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Replicas: 1, Ready: 1},
		},
		Links: []fusionapi.TopoLink{
			{ID: "d-web-cart", From: "s-web", To: "s-cart", FromKind: "service", ToKind: "service"},
			{ID: "d-LEAK", From: "s-cart", To: "s-LEAK-web", FromKind: "service", ToKind: "service"},
			{ID: "d-cart-search", From: "s-cart", To: "s-search", FromKind: "service", ToKind: "service"},
			{ID: "d-cart-cart2", From: "s-cart", To: "s-cart2", FromKind: "service", ToKind: "service"},
			{ID: "d-cart-ext", From: "s-cart", To: "x-1", FromKind: "service", ToKind: "external"},
		},
		Externals: map[string]string{"x-1": "LEAK GitHub"}}
}

// liveApps is extras whose applications can be edited, or made unreadable, while the test runs (as the server's are read
// again after appGroupTTL).
type liveApps struct {
	*topoExtras
	mu     sync.Mutex
	groups []fusionapi.AppGroup
	down   bool
}

func (e *liveApps) Applications(context.Context) ([]fusionapi.AppGroup, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.down {
		return nil, errors.New("the workspace store is down")
	}
	return append([]fusionapi.AppGroup(nil), e.groups...), nil
}

func (e *liveApps) set(groups []fusionapi.AppGroup, down bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.groups, e.down = groups, down
}

func appTokenRig(t *testing.T) (*dataRig, *appStores, *liveApps) {
	t.Helper()
	d := newDataRig(t)
	st := newAppStores(t)
	d.a.Fusion.Data = &fusionapi.Client{Prometheus: st.prom.URL, Loki: st.loki.URL, Tempo: st.tempo.URL}
	events := []fusionapi.ChangeEvent{
		{Time: time.Date(2026, 10, 5, 11, 29, 0, 0, time.UTC), Kind: "restarted", TargetKind: "service", TargetID: "s-cart", Cluster: "cl-1", Name: "cart restarted"},
		{Time: time.Date(2026, 10, 5, 11, 29, 0, 0, time.UTC), Kind: "restarted", TargetKind: "service", TargetID: "s-search", Cluster: "cl-1", Name: "LEAK search restarted"},
		{Time: time.Date(2026, 10, 5, 11, 29, 0, 0, time.UTC), Kind: "dependency-seen", TargetKind: "dependency", TargetID: "d-cart-search", Cluster: "cl-1", Name: "LEAK link"},
		{Time: time.Date(2026, 10, 5, 11, 29, 0, 0, time.UTC), Kind: "restarted", TargetKind: "service", TargetID: "s-LEAK-web", Cluster: "cl-1", Name: "LEAK the other web restarted"},
		{Time: time.Date(2026, 10, 5, 11, 29, 0, 0, time.UTC), Kind: "node-ready", TargetKind: "node", TargetID: "n1", Cluster: "cl-1", Name: "LEAK node"},
		{Time: time.Date(2026, 10, 5, 11, 29, 0, 0, time.UTC), Kind: "cluster-ok", TargetKind: "cluster", TargetID: "cl-1", Cluster: "cl-1", Name: "LEAK cluster"},
	}
	ex := &liveApps{topoExtras: &topoExtras{view: appTokenView(), events: events}, groups: appTokenGroups()}
	d.a.extras = ex
	return d, st, ex
}

func noLeak(t *testing.T, what string, r resp) {
	t.Helper()
	if r.Code != 200 {
		t.Fatalf("%s: %d %s", what, r.Code, r.Body.String())
	}
	if b := r.Body.String(); strings.Contains(b, leak) {
		t.Errorf("%s gave the token what is not its applications': %s", what, b)
	}
}

func TestATokenIsLimitedOnlyToApplicationsThatExist(t *testing.T) {
	d, _, _ := appTokenRig(t)
	var many []string
	for i := 0; i <= fusionapi.MaxTokenApplications; i++ {
		many = append(many, fmt.Sprintf("app-%d", i))
	}
	for name, apps := range map[string][]string{
		"unknown":      {"app-shop", "app-nope"},
		"by name":      {"Shop"},
		"blank":        {" ", ""},
		"too many":     many,
		"bad id chars": {"app\x00shop"},
	} {
		if r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "x", "applications": apps}, withCookie(d.admin)); r.Code != 400 {
			t.Errorf("%s: %d %s", name, r.Code, r.Body.String())
		}
	}
	r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "shop", "applications": []string{"app-shop", " app-shop ", "app-search"}}, withCookie(d.admin))
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	apps := r.json(t)["details"].(map[string]any)["applications"].([]any)
	if len(apps) != 2 || apps[0].(map[string]any)["id"] != "app-shop" || apps[0].(map[string]any)["name"] != "Shop" || apps[1].(map[string]any)["name"] != "Search" {
		t.Fatalf("applications = %v", apps)
	}
	rows, _ := d.st.AuditSince(d.ctx, 0, 500)
	var detail string
	for _, e := range rows {
		if e.Action == "fusion-token-created" {
			detail = e.Detail
		}
	}
	if !strings.Contains(detail, "applications Shop (app-shop), Search (app-search)") {
		t.Fatalf("audit detail = %q", detail)
	}
	// A token not limited to applications says so with an empty list, and its audit row is what it was.
	d.mint(t, map[string]any{"name": "open"})
	list := d.do("GET", "/api/v1/fusion/tokens", nil, withCookie(d.admin)).jsonArray(t)
	for _, tok := range list {
		got, _ := tok["applications"].([]any)
		switch tok["name"] {
		case "open":
			if got == nil || len(got) != 0 {
				t.Fatalf("an unlimited token lists applications %v", tok["applications"])
			}
		case "shop":
			if len(got) != 2 {
				t.Fatalf("listed applications %v", got)
			}
		}
	}
	rows, _ = d.st.AuditSince(d.ctx, 0, 500)
	if last := rows[len(rows)-1]; last.Action != "fusion-token-created" || strings.Contains(last.Detail, "applications") {
		t.Fatalf("an unlimited token's audit detail = %q", last.Detail)
	}
	// An application deleted since is listed by its id, marked gone; one renamed by its name now.
	groups := appTokenGroups()
	groups[0].Name = "Shop renamed"
	d.a.extras.(*liveApps).set(groups[:1], false)
	list = d.do("GET", "/api/v1/fusion/tokens", nil, withCookie(d.admin)).jsonArray(t)
	for _, tok := range list {
		if tok["name"] == "shop" {
			got := tok["applications"].([]any)
			a, b := got[0].(map[string]any), got[1].(map[string]any)
			if a["name"] != "Shop renamed" || a["gone"] != nil || b["id"] != "app-search" || b["gone"] != true || b["name"] != nil {
				t.Fatalf("listed applications %v", got)
			}
		}
	}
	// Applications that cannot be read do not stop the list; each is given by its id alone.
	d.a.extras.(*liveApps).set(nil, true)
	list = d.do("GET", "/api/v1/fusion/tokens", nil, withCookie(d.admin)).jsonArray(t)
	if len(list) != 2 {
		t.Fatalf("list = %v", list)
	}
	// ... but a token cannot be limited to them while they cannot be checked.
	if r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "x", "applications": []string{"app-shop"}}, withCookie(d.admin)); r.Code != 503 {
		t.Fatalf("made while the applications cannot be read: %d %s", r.Code, r.Body.String())
	}
}

// Every route class: an application token sees its members' telemetry where they run, and nothing near it.
func TestAnApplicationTokenSeesOnlyItsMembersWhereTheyRun(t *testing.T) {
	d, st, _ := appTokenRig(t)
	secret, _ := d.mint(t, map[string]any{"name": "shop", "applications": []string{"app-shop"}})
	tok := bearer(secret)

	// One trace, across the application, another service of its namespace, a cart elsewhere and one that says nowhere.
	r := d.get("/api/v1/fusion/traces/"+testTraceID, tok)
	noLeak(t, "the trace", r)
	if spans := r.json(t)["spans"].([]any); len(spans) != 2 {
		t.Fatalf("spans = %v", spans)
	}
	// The search: only the hit of a member where it runs.
	n := st.count()
	r = d.get("/api/v1/fusion/traces", tok)
	noLeak(t, "the search", r)
	if hits := r.json(t)["traces"].([]any); len(hits) != 1 || hits[0].(map[string]any)["traceId"] != appTraceA {
		t.Fatalf("hits = %v", hits)
	}
	if q := st.since(n); !strings.Contains(q, `resource.k8s.namespace.name = "shop" && resource.ikhnos.cluster.id = "cl-1" && resource.service.name =~ "^(?:cart|web)$"`) {
		t.Fatalf("the search asked %s", q)
	}
	// The lines.
	n = st.count()
	r = d.get("/api/v1/fusion/logs", tok)
	noLeak(t, "the logs", r)
	if e := r.json(t)["entries"].([]any); len(e) != 2 {
		t.Fatalf("entries = %v", e)
	}
	if q := st.since(n); !strings.Contains(q, `(k8s_namespace_name="shop" and ikhnos_cluster_id="cl-1" and service_name=~"cart|web")`) {
		t.Fatalf("the logs asked %s", q)
	}
	// The metrics: names (asked of each place), series and a range.
	n = st.count()
	noLeak(t, "the metric names", d.get("/api/v1/fusion/metrics/names", tok))
	if q := st.since(n); !strings.Contains(q, `k8s_namespace_name="shop",ikhnos_cluster_id="cl-1",service_name=~"cart|web"`) ||
		!strings.Contains(q, `k8s_namespace_name="shop",ikhnos_cluster_id="cl-1",k8s_deployment_name=~"cart|web"`) {
		t.Fatalf("the names asked %s", q)
	}
	r = d.get("/api/v1/fusion/metrics/series", tok)
	noLeak(t, "the series", r)
	if s := r.json(t)["series"].([]any); len(s) != 2 {
		t.Fatalf("series = %v", s)
	}
	r = d.get("/api/v1/fusion/metrics/range?metric=up", tok)
	noLeak(t, "the range", r)
	if s := r.json(t)["series"].([]any); len(s) != 2 {
		t.Fatalf("range = %v", s)
	}
	// System telemetry alone is empty, and the answer says why.
	r = d.get("/api/v1/fusion/metrics/range?name=node_load1", tok)
	noLeak(t, "a node's metric", r)
	if !strings.Contains(r.Body.String(), "carries no service") {
		t.Fatalf("no word on system telemetry: %s", r.Body.String())
	}

	// The services, and where each runs: the application's own places only.
	r = d.get("/api/v1/fusion/services", tok)
	noLeak(t, "the services", r)
	for _, sv := range r.json(t)["services"].([]any) {
		s := sv.(map[string]any)
		if s["name"] != "cart" && s["name"] != "web" {
			t.Fatalf("service %v", s)
		}
		for _, p := range s["runsIn"].([]any) {
			if p.(map[string]any)["namespace"] != "shop" {
				t.Fatalf("%s runs in %v", s["name"], s["runsIn"])
			}
		}
	}
	// The applications: its own, and no other.
	r = d.get("/api/v1/fusion/applications", tok)
	noLeak(t, "the applications", r)
	if apps := r.json(t)["applications"].([]any); len(apps) != 1 || apps[0].(map[string]any)["id"] != "app-shop" {
		t.Fatalf("applications = %v", apps)
	}
	noLeak(t, "the application", d.get("/api/v1/fusion/applications/Shop", tok))
	for _, other := range []string{"app-search", "Search", "app-other"} {
		for _, path := range []string{"/api/v1/fusion/applications/" + other, "/api/v1/fusion/applications/" + other + "/topology", "/api/v1/fusion/logs?application=" + other} {
			if r := d.get(path, tok); r.Code != 404 {
				t.Errorf("%s: %d %s", path, r.Code, r.Body.String())
			}
		}
	}
	noLeak(t, "logs of its application", d.get("/api/v1/fusion/logs?application=app-shop", tok))
	// One service at a glance: its own; one of the same name elsewhere is not there.
	noLeak(t, "a service", d.get("/api/v1/fusion/services/cart", tok))
	if r := d.get("/api/v1/fusion/services/cart?namespace=other", tok); r.Code != 404 {
		t.Errorf("a cart of namespace other: %d %s", r.Code, r.Body.String())
	}
	if r := d.get("/api/v1/fusion/services/search", tok); r.Code != 404 {
		t.Errorf("another service of the namespace: %d %s", r.Code, r.Body.String())
	}

	// A fused read with every include.
	n = st.count()
	r = d.get("/api/v1/fusion/traces/"+testTraceID+"?include=logs,context_logs,system_logs,metrics,topology,changes&metric_scope=app,pod,node", tok)
	noLeak(t, "the fused read", r)
	j := r.json(t)
	src := j["sources"].(map[string]any)
	if src["systemLogs"] != "not allowed" || src["logs"] != "ok" || src["metrics"] != "ok" || src["topology"] != "ok" || src["changes"] != "ok" {
		t.Fatalf("sources = %v", src)
	}
	if ch, _ := j["changes"].([]any); len(ch) != 1 {
		t.Fatalf("changes = %v", ch)
	}
	if asked := st.since(n); strings.Contains(asked, "kube-system") {
		t.Fatalf("the system lines were read: %s", asked)
	}
	body := r.Body.String()
	for _, id := range []string{"s-search", "s-cart2", "x-1"} {
		if strings.Contains(body, id) {
			t.Errorf("the fused read names %s, which the token may not see", id)
		}
	}
	// The same as a batch.
	r = d.post("/api/v1/fusion/traces/batch", map[string]any{"ids": []string{testTraceID}, "include": "logs,metrics,topology,changes,system_logs"}, tok)
	noLeak(t, "the batch", r)
	// The overview of the status: what it is limited to, and why it may send no query of its own.
	r = d.get("/api/v1/fusion/status", tok)
	noLeak(t, "the status", r)
	acc := r.json(t)["access"].(map[string]any)
	if acc["promql"] != false || acc["rawQueries"] != false || !strings.Contains(fmt.Sprint(acc["notes"]), "PromQL, LogQL and TraceQL") {
		t.Fatalf("access = %v", acc)
	}
	if a := acc["applications"].([]any); len(a) != 1 || a[0].(map[string]any)["name"] != "Shop" {
		t.Fatalf("access applications = %v", acc["applications"])
	}
}

func TestAnApplicationTokenSendsNoQueryOfItsOwn(t *testing.T) {
	d, st, _ := appTokenRig(t)
	secret, _ := d.mint(t, map[string]any{"name": "shop", "applications": []string{"app-shop"}})
	tok := bearer(secret)
	n := st.count()
	for _, path := range []string{
		"/api/v1/fusion/metrics/query?query=up",
		"/api/v1/fusion/metrics/query_range?query=up&start=1&end=2&step=1",
		"/api/v1/fusion/logs?query=" + url.QueryEscape(`{service_name="cart"}`),
		"/api/v1/fusion/traces?q=" + url.QueryEscape(`{ true }`),
		"/api/v1/fusion/traces/" + testTraceID + "?promql=" + url.QueryEscape("cpu=rate(x[1m])"),
		"/api/v1/fusion/prometheus/api/v1/query?query=up",
		"/api/v1/fusion/prometheus/api/v1/series?match[]=up",
		"/api/v1/fusion/loki/loki/api/v1/labels",
		"/api/v1/fusion/tempo/api/search",
		"/api/v1/fusion/links",
	} {
		r := d.get(path, tok)
		if r.Code != 403 || !strings.Contains(r.Body.String(), "applications") {
			t.Errorf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
	r := d.post("/api/v1/fusion/traces/batch", map[string]any{"ids": []string{testTraceID}, "promql": map[string]string{"cpu": "rate(x[1m])"}}, tok)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"status":403`) {
		t.Errorf("batch with promql: %d %s", r.Code, r.Body.String())
	}
	if asked := st.since(n); asked != "" {
		t.Fatalf("a refused query reached a store:\n%s", asked)
	}
}

// The topology of its application: its services, the links between them, and no neighbour or address it may not see; and
// only the changes of those.
func TestAnApplicationTokenSeesOnlyItsApplicationsTopology(t *testing.T) {
	d, _, ex := appTokenRig(t)
	secret, _ := d.mint(t, map[string]any{"name": "shop", "applications": []string{"app-shop"}})
	ex.events = append(ex.events, fusionapi.ChangeEvent{Time: time.Date(2026, 10, 5, 11, 59, 0, 0, time.UTC), Kind: "dependency-seen", TargetKind: "dependency", TargetID: "d-web-cart", Cluster: "cl-1", Name: "web calls cart"})
	r := d.get("/api/v1/fusion/applications/app-shop/topology", bearer(secret))
	noLeak(t, "the topology", r)
	body := r.Body.String()
	for _, id := range []string{"s-search", "s-cart2", "x-1", "d-cart-search", "d-cart-ext"} {
		if strings.Contains(body, id) {
			t.Errorf("the topology names %s: %s", id, body)
		}
	}
	j := r.json(t)
	if len(j["services"].([]any)) != 2 || len(j["links"].([]any)) != 1 {
		t.Fatalf("topology = %s", body)
	}
	for _, id := range ex.ids {
		if id == "app-shop" || id == "s-search" || id == "d-cart-search" {
			t.Errorf("asked for the events of %s", id)
		}
	}
}

// The applications are resolved at every request: a member added shows once the applications are read again (appGroupTTL),
// a member removed goes, and a deleted application leaves the token seeing nothing and /status saying why. Read through the
// server's own reuse of the applications, from the saved workspace.
func TestAnEditedApplicationChangesWhatTheTokenSeesAfterTheTTL(t *testing.T) {
	d := newDataRig(t)
	st := newAppStores(t)
	d.a.Fusion.Data = &fusionapi.Client{Prometheus: st.prom.URL, Loki: st.loki.URL, Tempo: st.tempo.URL}
	rev := int64(0)
	save := func(services string, apps string) {
		t.Helper()
		ws := `{"schemaVersion":4,"applications":[` + apps + `],"services":[` + services + `]}`
		if _, err := d.a.C.ForOrg("org-1").Landscape.SaveWorkspace(d.ctx, "root", rev, []byte(ws)); err != nil {
			t.Fatal(err)
		}
		rev++
	}
	const shop = `{"id":"app-shop","name":"Shop"}`
	cart := `{"id":"sv-cart","source":"manual","applicationId":"app-shop","name":"cart","namespace":"shop","clusterId":"cl-1"}`
	web := `{"id":"sv-web","source":"manual","applicationId":"app-shop","name":"web","namespace":"shop","clusterId":"cl-1"}`
	save(cart+","+web, shop)
	secret, _ := d.mint(t, map[string]any{"name": "shop", "applications": []string{"app-shop"}})
	tok := bearer(secret)
	asks := func(want string) bool {
		t.Helper()
		n := st.count()
		if r := d.get("/api/v1/fusion/logs", tok); r.Code != 200 {
			t.Fatalf("%d %s", r.Code, r.Body.String())
		}
		return strings.Contains(st.since(n), want)
	}
	if !asks(`service_name=~"cart|web")`) {
		t.Fatalf("not read as cart and web: %s", st.since(0))
	}
	if e := d.get("/api/v1/fusion/logs", tok).json(t)["entries"].([]any); len(e) != 2 {
		t.Fatalf("entries = %v", e)
	}

	// web leaves the application: within the TTL the token is as it was, after it web is gone from it.
	save(cart, shop)
	if !asks(`service_name=~"cart|web")`) {
		t.Fatal("the edit was seen before the applications were read again")
	}
	d.advance(t, appGroupTTL+time.Second)
	if !asks(`service_name=~"cart")`) {
		t.Fatalf("the member removed is still read: %s", st.since(0))
	}
	if e := d.get("/api/v1/fusion/logs", tok).json(t)["entries"].([]any); len(e) != 1 {
		t.Fatalf("entries = %v", e)
	}
	// web comes back: a member the token was made with gives its rights again; a new one (search) never does.
	search := `{"id":"sv-search","source":"manual","applicationId":"app-shop","name":"search","namespace":"shop","clusterId":"cl-1"}`
	save(cart+","+web+","+search, shop)
	d.advance(t, appGroupTTL+time.Second)
	if !asks(`service_name=~"cart|web")`) || asks("search") {
		t.Fatalf("after the edit: %s", st.since(0))
	}
	// The application is deleted: nothing is read, every read says why, and the status names what is gone.
	save("", `{"id":"app-other","name":"Other"}`)
	d.advance(t, appGroupTTL+time.Second)
	n := st.count()
	for _, path := range []string{"/api/v1/fusion/logs", "/api/v1/fusion/traces", "/api/v1/fusion/metrics/series", "/api/v1/fusion/traces/" + testTraceID} {
		if r := d.get(path, tok); r.Code != 403 || !strings.Contains(r.Body.String(), "sees nothing") {
			t.Errorf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
	if r := d.get("/api/v1/fusion/applications", tok); r.Code != 200 || len(r.json(t)["applications"].([]any)) != 0 {
		t.Errorf("applications: %d %s", r.Code, r.Body.String())
	}
	if asked := st.since(n); asked != "" {
		t.Fatalf("a token that sees nothing asked the stores:\n%s", asked)
	}
	acc := d.get("/api/v1/fusion/status", tok).json(t)["access"].(map[string]any)
	apps := acc["applications"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["gone"] != true || !strings.Contains(fmt.Sprint(acc["notes"]), "app-shop no longer exists") ||
		!strings.Contains(fmt.Sprint(acc["notes"]), "sees nothing") {
		t.Fatalf("access = %v", acc)
	}
}

// Applications that cannot be read refuse the request as unavailable, never fall back on the token's namespaces and
// clusters, and are not a failed sign-in.
func TestAnApplicationTokenWhoseApplicationsCannotBeReadIsRefused(t *testing.T) {
	d, st, ex := appTokenRig(t)
	secret, _ := d.mint(t, map[string]any{"name": "shop", "namespaces": []string{"shop"}, "applications": []string{"app-shop"}})
	open, _ := d.mint(t, map[string]any{"name": "open", "namespaces": []string{"shop"}})
	ex.set(nil, true)
	n := st.count()
	for _, path := range []string{"/api/v1/fusion/status", "/api/v1/fusion/logs", "/api/v1/fusion/traces/" + testTraceID, "/api/v1/fusion/metrics/series"} {
		r := d.get(path, bearer(secret))
		if r.Code != 503 || r.Header().Get("Retry-After") == "" {
			t.Errorf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
	if st.since(n) != "" {
		t.Fatalf("asked the stores: %s", st.since(n))
	}
	// A token not limited to applications does not depend on them.
	if r := d.get("/api/v1/fusion/logs", bearer(open)); r.Code != 200 {
		t.Fatalf("an unlimited token: %d %s", r.Code, r.Body.String())
	}
	// And the server with no applications at all refuses an application token too.
	d.a.extras = nil
	d.a.P = nil
	if r := d.get("/api/v1/fusion/logs", bearer(secret)); r.Code != 503 {
		t.Fatalf("no applications on this server: %d %s", r.Code, r.Body.String())
	}
}

// The rights are the applications' places within the token's namespace limit: a token limited to namespace other sees the
// cart of other only, even when the cart of shop is put in its application later; and one whose application has nothing in
// its namespaces cannot be made.
func TestApplicationRightsAreWithinTheTokensNamespaces(t *testing.T) {
	d, st, ex := appTokenRig(t)
	if r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "carts", "namespaces": []string{"other"}, "applications": []string{"app-shop", "app-other"}}, withCookie(d.admin)); r.Code != 400 {
		t.Fatalf("an application with nothing in the token's namespaces: %d %s", r.Code, r.Body.String())
	}
	secret, _ := d.mint(t, map[string]any{"name": "carts", "namespaces": []string{"other"}, "applications": []string{"app-other"}})
	groups := appTokenGroups()
	groups[2].Members = append(groups[2].Members, groups[0].Members[0]) // the cart of shop joins Other cart
	ex.set(groups, false)
	n := st.count()
	r := d.get("/api/v1/fusion/logs", bearer(secret))
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	e := r.json(t)["entries"].([]any)
	if len(e) != 1 || e[0].(map[string]any)["line"] != "LEAK other cart line" { // (the one line of this token's place)
		t.Fatalf("entries = %v", e)
	}
	if q := st.since(n); !strings.Contains(q, `(k8s_namespace_name="other" and ikhnos_cluster_id="cl-1" and service_name=~"cart")`) || strings.Contains(q, `"shop"`) {
		t.Fatalf("asked %s", q)
	}
	apps := d.get("/api/v1/fusion/applications", bearer(secret)).json(t)["applications"].([]any)
	if len(apps) != 1 || apps[0].(map[string]any)["id"] != "app-other" || len(apps[0].(map[string]any)["services"].([]any)) != 1 {
		t.Fatalf("applications = %v", apps)
	}
}

// Applications in more places than a focus names one by one: an application token is still read place by place, its lists
// of names too, and the answers are each member where it runs.
func TestAnApplicationTokenInManyPlacesIsStillReadPlaceByPlace(t *testing.T) {
	d, st, ex := appTokenRig(t)
	groups := appTokenGroups()
	for i := 0; i < 2; i++ {
		g := fusionapi.AppGroup{ID: fmt.Sprintf("app-big%d", i), Name: fmt.Sprintf("Big %d", i)}
		for j := 0; j < 11; j++ {
			g.Members = append(g.Members, fusionapi.AppMember{ID: fmt.Sprintf("s%d-%d", i, j), Name: fmt.Sprintf("svc%d-%d", i, j), Namespace: fmt.Sprintf("ns%d-%d", i, j), Cluster: "cl-1"})
		}
		groups = append(groups, g)
	}
	// cart in shop, and the big ones' 22 places: other and search are in the union of names and namespaces, never a place.
	groups[0].Members = groups[0].Members[:1]
	ex.set(groups, false)
	secret, _ := d.mint(t, map[string]any{"name": "big", "applications": []string{"app-shop", "app-big0", "app-big1"}})
	n := st.count()
	r := d.get("/api/v1/fusion/logs", bearer(secret))
	noLeak(t, "the logs", r)
	if e := r.json(t)["entries"].([]any); len(e) != 1 || e[0].(map[string]any)["line"] != "cart line" {
		t.Fatalf("entries = %v", e)
	}
	if q := st.since(n); !strings.Contains(q, `(k8s_namespace_name="shop" and ikhnos_cluster_id="cl-1" and service_name=~"cart")`) ||
		!strings.Contains(q, `(k8s_namespace_name="ns1-10" and ikhnos_cluster_id="cl-1" and service_name=~"svc1-10")`) {
		t.Fatalf("the places were not named one by one: %s", q)
	}
	n = st.count()
	noLeak(t, "the metric names", d.get("/api/v1/fusion/metrics/names", bearer(secret)))
	if q := st.since(n); !strings.Contains(q, `k8s_namespace_name="ns1-10",ikhnos_cluster_id="cl-1",service_name=~"svc1-10"}`) {
		t.Fatalf("the names were not asked place by place: %s", q)
	}
	noLeak(t, "the services", d.get("/api/v1/fusion/services", bearer(secret)))
	noLeak(t, "a service", d.get("/api/v1/fusion/services/cart", bearer(secret)))
	noLeak(t, "the series", d.get("/api/v1/fusion/metrics/series", bearer(secret)))
	noLeak(t, "the range", d.get("/api/v1/fusion/metrics/range?metric=up", bearer(secret)))
	noLeak(t, "the search", d.get("/api/v1/fusion/traces", bearer(secret)))
	noLeak(t, "the trace", d.get("/api/v1/fusion/traces/"+testTraceID, bearer(secret)))
}

// must is the value of a call that the test expects to work.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// A token is pinned to the members its applications had when it was made, as the administrator approved them: a service
// that joins afterwards (by its labels: a Helm release of the application's name, or put there by a person) never widens it,
// one that leaves takes its rights with it, and one that moves is visible neither where it was nor where it is.
func TestATokenIsPinnedToTheMembersItWasMadeWith(t *testing.T) {
	d, st, ex := appTokenRig(t)
	groups := appTokenGroups()
	ex.set(groups, false)
	// The approval is of what was shown: a service no longer there is refused, and one not approved is not pinned.
	if r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "x", "applications": []string{"app-shop"}, "members": []string{"s-cart", "s-gone"}}, withCookie(d.admin)); r.Code != 409 {
		t.Fatalf("an approved service that is gone: %d %s", r.Code, r.Body.String())
	}
	if r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "x", "applications": []string{"app-shop"}, "members": []string{}}, withCookie(d.admin)); r.Code != 400 {
		t.Fatalf("nothing approved: %d %s", r.Code, r.Body.String())
	}
	r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "shop", "applications": []string{"app-shop"}, "members": []string{"s-cart", "s-web"}}, withCookie(d.admin))
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	j := r.json(t)
	secret := j["token"].(string)
	members := j["details"].(map[string]any)["applicationMembers"].([]any)
	if len(members) != 2 || fmt.Sprint(members[0]) != "map[application:app-shop cluster:cl-1 id:s-cart kind:Deployment name:cart namespace:shop]" {
		t.Fatalf("pinned members = %v", members)
	}
	rows, _ := d.st.AuditSince(d.ctx, 0, 500)
	if !strings.Contains(rows[len(rows)-1].Detail, "applications Shop (app-shop) with 2 services in shop/cl-1") {
		t.Fatalf("audit = %q", rows[len(rows)-1].Detail)
	}
	reads := func() string {
		t.Helper()
		n := st.count()
		noLeak(t, "the logs", d.get("/api/v1/fusion/logs", bearer(secret)))
		return st.since(n)
	}
	// Joined since: a cart of namespace other (by a person) and search of shop (by its labels). Neither is the token's.
	groups[0].Members = append(groups[0].Members,
		fusionapi.AppMember{ID: "s-cart2", Name: "cart", Namespace: "other", Cluster: "cl-1", Kind: "Deployment"},
		fusionapi.AppMember{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment"})
	ex.set(groups, false)
	if q := reads(); strings.Contains(q, `"other"`) || strings.Contains(q, "search") || !strings.Contains(q, `service_name=~"cart|web")`) {
		t.Fatalf("asked %s", q)
	}
	if apps := d.get("/api/v1/fusion/applications", bearer(secret)).json(t)["applications"].([]any); len(apps[0].(map[string]any)["services"].([]any)) != 2 {
		t.Fatalf("applications = %v", apps)
	}
	// web's label is removed and ikhnos drops it from the application: it is gone from the token.
	groups[0].Members = []fusionapi.AppMember{groups[0].Members[0]}
	ex.set(groups, false)
	if q := reads(); strings.Contains(q, "web") {
		t.Fatalf("a member that left is still read: %s", q)
	}
	// cart moves to namespace other: visible neither in shop nor in other, and the token sees nothing.
	groups[0].Members[0].Namespace = "other"
	ex.set(groups, false)
	if r := d.get("/api/v1/fusion/logs", bearer(secret)); r.Code != 403 || !strings.Contains(r.Body.String(), "sees nothing") {
		t.Fatalf("after the move: %d %s", r.Code, r.Body.String())
	}
	list := d.do("GET", "/api/v1/fusion/tokens", nil, withCookie(d.admin)).jsonArray(t)
	if m := list[0]["applicationMembers"].([]any); len(m) != 2 {
		t.Fatalf("listed pinned members = %v", m)
	}
	// Too many places for one token.
	var big []fusionapi.AppMember
	for i := 0; i <= fusionapi.MaxTokenPlaces; i++ {
		big = append(big, fusionapi.AppMember{ID: fmt.Sprintf("s%d", i), Name: "svc", Namespace: fmt.Sprintf("ns%d", i), Cluster: "cl-1"})
	}
	ex.set(append(groups, fusionapi.AppGroup{ID: "app-big", Name: "Big", Members: big}), false)
	if r := d.do("POST", "/api/v1/fusion/tokens", map[string]any{"name": "big", "applications": []string{"app-big"}}, withCookie(d.admin)); r.Code != 400 || !strings.Contains(r.Body.String(), "places") {
		t.Fatalf("too many places: %d %s", r.Code, r.Body.String())
	}
}

// An application ikhnos discovered and a person accepted, whose services are in it by their labels alone (accepting it
// writes no member of its own), can be used for a token: its services as the applications are read from the workspace and
// the topology's hints.
func TestATokenCanBeLimitedToAnAcceptedDiscoveredApplication(t *testing.T) {
	d, st, ex := appTokenRig(t)
	doc := landscape.StateDoc{Topology: model.Topology{Services: []model.Service{
		{ID: "sv-cart", Name: "cart", Namespace: "shop", ClusterID: "cl-1", Kind: "Deployment", ApplicationHint: "app-disc"},
		{ID: "sv-web", Name: "web", Namespace: "shop", ClusterID: "cl-1", Kind: "Deployment", ApplicationHint: "app-disc"},
	}}}
	ws := []byte(`{"schemaVersion":4,"applications":[{"id":"app-disc","name":"Shop","origin":"discovered","confidence":"high"}],
		"refs":{"sv-cart":{"kind":"service","overrides":{"applicationId":"app-disc"}}}}`)
	groups, err := appGroups(ws, doc)
	if err != nil || len(groups) != 1 || len(groups[0].Members) != 2 {
		t.Fatalf("groups = %+v (%v)", groups, err)
	}
	ex.set(groups, false)
	secret, _ := d.mint(t, map[string]any{"name": "disc", "applications": []string{"app-disc"}})
	n := st.count()
	r := d.get("/api/v1/fusion/logs", bearer(secret))
	noLeak(t, "the logs", r)
	if q := st.since(n); !strings.Contains(q, `(k8s_namespace_name="shop" and ikhnos_cluster_id="cl-1" and service_name=~"cart|web")`) {
		t.Fatalf("asked %s", q)
	}
}

// A saved workspace that cannot be read is not "no applications": an application token is refused as unavailable, never
// told that its applications are gone.
func TestAWorkspaceThatCannotBeReadRefusesAnApplicationToken(t *testing.T) {
	d := newDataRig(t)
	st := newAppStores(t)
	d.a.Fusion.Data = &fusionapi.Client{Prometheus: st.prom.URL, Loki: st.loki.URL, Tempo: st.tempo.URL}
	ws := `{"schemaVersion":4,"applications":[{"id":"app-shop","name":"Shop"}],"services":[{"id":"sv-cart","source":"manual","applicationId":"app-shop","name":"cart","namespace":"shop","clusterId":"cl-1"}]}`
	if _, err := d.a.C.ForOrg("org-1").Landscape.SaveWorkspace(d.ctx, "root", 0, []byte(ws)); err != nil {
		t.Fatal(err)
	}
	secret, _ := d.mint(t, map[string]any{"name": "shop", "applications": []string{"app-shop"}})
	if _, err := d.st.PutWorkspace(d.ctx, "org-1", 1, []byte("{not json"), "root", *d.now); err != nil {
		t.Fatal(err)
	}
	d.advance(t, appGroupTTL+time.Second)
	for _, path := range []string{"/api/v1/fusion/status", "/api/v1/fusion/logs"} {
		if r := d.get(path, bearer(secret)); r.Code != 503 || strings.Contains(r.Body.String(), "no longer exists") {
			t.Errorf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
}

// A service outside the token's applications, asked for at a place, is said to be a service that is not there (not an
// application).
func TestAServiceOutsideTheRightsIsNotFoundAsAService(t *testing.T) {
	d, _, _ := appTokenRig(t)
	secret, _ := d.mint(t, map[string]any{"name": "shop", "applications": []string{"app-shop"}})
	for _, path := range []string{"/api/v1/fusion/services/cart?namespace=other", "/api/v1/fusion/services/search"} {
		r := d.get(path, bearer(secret))
		if r.Code != 404 || !strings.Contains(r.Body.String(), `no service \"`) || strings.Contains(r.Body.String(), "application") {
			t.Errorf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
}
