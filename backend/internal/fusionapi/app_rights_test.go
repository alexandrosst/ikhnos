package fusionapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The applications of the tests: Shop is cart (a Deployment, with its app label cart-app) and web (a StatefulSet) in shop on
// cl-1, both put there by a person; Pay is pay in pay on cl-2; Loose has a member ikhnos knows no namespace for. Next to them
// run what is no member: a cart of namespace other, a search of namespace shop, telemetry that says no namespace.
func rightsGroups() []AppGroup {
	return []AppGroup{
		{ID: "app-shop", Name: "Shop", Members: []AppMember{
			{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment", Aliases: []string{"cart-app"}},
			{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Kind: "StatefulSet"},
		}},
		{ID: "app-pay", Name: "Pay", Members: []AppMember{{ID: "s-pay", Name: "pay", Namespace: "pay", Cluster: "cl-2", Kind: "Deployment"}}},
		{ID: "app-loose", Name: "Loose", Members: []AppMember{{ID: "s-worker", Name: "worker"}}},
	}
}

// limit is s limited to the applications, pinned to the members they have now (as a token made now is).
func limit(s Scope, groups []AppGroup, ids ...string) (Scope, []string) {
	return s.LimitToApplications(groups, ids, s.ApplicationMembers(groups, ids))
}

func shopOnly(t *testing.T) Scope {
	t.Helper()
	s, notes := limit(Scope{Signals: Signals}, rightsGroups(), "app-shop")
	if len(notes) != 0 || !s.AppLimited || len(s.RightsPlaces) != 1 {
		t.Fatalf("scope = %+v, notes %v", s, notes)
	}
	return s
}

func TestApplicationRightsAreTheMembersPlacesWithinTheTokensLimits(t *testing.T) {
	s, notes := limit(Scope{Signals: Signals}, rightsGroups(), "app-shop", "app-pay", "app-gone", "app-loose", "app-shop")
	want := []FocusPlace{
		{Namespace: "shop", Cluster: "cl-1", Names: []string{"cart", "web"}, Workloads: map[string][]string{"k8s_deployment_name": {"cart"}, "k8s_statefulset_name": {"web"}}},
		{Namespace: "pay", Cluster: "cl-2", Names: []string{"pay"}, Workloads: map[string][]string{"k8s_deployment_name": {"pay"}}}}
	if !reflect.DeepEqual(s.RightsPlaces, want) {
		t.Fatalf("places = %+v", s.RightsPlaces)
	}
	if got := []string{s.RightsApps[0].ID, s.RightsApps[1].ID, s.RightsApps[2].ID}; len(s.RightsApps) != 3 || !reflect.DeepEqual(got, []string{"app-shop", "app-pay", "app-loose"}) {
		t.Fatalf("apps = %+v", s.RightsApps)
	}
	all := strings.Join(notes, "\n")
	if !strings.Contains(all, "app-gone no longer exists") || !strings.Contains(all, "Loose (app-loose) has none of the services this token was made with") || len(notes) != 2 {
		t.Fatalf("notes = %v", notes)
	}
	if s.Unrestricted() {
		t.Fatal("a scope limited to applications is unrestricted")
	}
	// Within a namespace limit: only the places inside it.
	ns, _ := limit(Scope{Signals: Signals, Namespaces: []string{"pay"}}, rightsGroups(), "app-shop", "app-pay")
	if len(ns.RightsPlaces) != 1 || ns.RightsPlaces[0].Namespace != "pay" {
		t.Fatalf("within the namespace limit: %+v", ns.RightsPlaces)
	}
	// Nothing left: it sees nothing, says why, and every read is refused before a store is asked.
	none, notes := limit(Scope{Signals: Signals, Clusters: []string{"cl-9"}}, rightsGroups(), "app-shop")
	if len(none.RightsPlaces) != 0 || !strings.Contains(strings.Join(notes, " "), "sees nothing") {
		t.Fatalf("outside every limit: %+v %v", none, notes)
	}
	var e *Error
	for _, sig := range Signals {
		if err := none.needSignal(sig); !errors.As(err, &e) || e.Status != http.StatusForbidden || !strings.Contains(e.Msg, "sees nothing") {
			t.Fatalf("%s: %v", sig, err)
		}
	}
	gone, _ := limit(Scope{Signals: Signals}, rightsGroups(), "app-gone")
	if gone.needSignal(SignalLogs) == nil {
		t.Fatal("a token whose only application is gone may read")
	}
}

// A scope that may read nothing renders queries that match nothing, never an open one, and holds nothing.
func TestAScopeThatMayReadNothingBuildsNoOpenQuery(t *testing.T) {
	none := Scope{Signals: Signals, AppLimited: true}
	for _, p := range []placeSet{none.rights(), none.places()} {
		if !p.none || p.contains("shop", "cl-1", "cart") || p.containsNamed("shop", "cl-1", "cart") {
			t.Fatalf("nowhere holds something: %+v", p)
		}
	}
	q, _ := LogFilter{}.logQL(none)
	tq, _ := TraceFilter{}.traceQL(none)
	sels, _ := MetricFilter{Name: "up"}.selectors(none)
	for _, got := range append([]string{q, tq}, sels...) {
		if !strings.Contains(got, noPlace) {
			t.Fatalf("a query of nowhere does not say so: %s", got)
		}
	}
	if none.NamespaceVisible("shop") || none.ClusterVisible("cl-1") || none.visibleAt("shop", "cl-1") {
		t.Fatal("nowhere sees a namespace or a cluster")
	}
	// Places a Scope was given that lie outside its own limits, or name no namespace or cluster, are not rights either.
	odd := Scope{Signals: Signals, AppLimited: true, Namespaces: []string{"pay"},
		RightsPlaces: []FocusPlace{{Namespace: "shop", Cluster: "cl-1", Names: []string{"cart"}}, {Cluster: "cl-1", Names: []string{"web"}}}}
	if !odd.rights().none {
		t.Fatalf("rights = %+v", odd.rights())
	}
}

func TestEveryQueryOfAnApplicationTokenReadsEachMemberWhereItRuns(t *testing.T) {
	s := shopOnly(t)
	q, err := LogFilter{}.logQL(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`service_name=~"cart|web"`, `k8s_namespace_name=~"shop"`, `| ikhnos_cluster_id=~"cl-1"`,
		`| (k8s_namespace_name="shop" and ikhnos_cluster_id="cl-1" and service_name=~"cart|web")`} {
		if !strings.Contains(q, want) {
			t.Fatalf("logql %s lacks %s", q, want)
		}
	}
	tq, _ := TraceFilter{}.traceQL(s)
	if !strings.Contains(tq, `(resource.k8s.namespace.name = "shop" && resource.ikhnos.cluster.id = "cl-1" && resource.service.name =~ "^(?:cart|web)$")`) {
		t.Fatalf("traceql = %s", tq)
	}
	sels, _ := MetricFilter{Name: "up"}.selectors(s)
	all := strings.Join(sels, "\n")
	for _, want := range []string{`k8s_namespace_name="shop",ikhnos_cluster_id="cl-1",service_name=~"cart|web"`,
		`k8s_namespace_name="shop",ikhnos_cluster_id="cl-1",k8s_deployment_name=~"cart"}`, `k8s_namespace_name="shop",ikhnos_cluster_id="cl-1",k8s_statefulset_name=~"web"}`} {
		if !strings.Contains(all, want) {
			t.Fatalf("selectors lack %s:\n%s", want, all)
		}
	}
	// A member's name is a workload's only under its own kind's label, and the app labels are not matched at all.
	if len(sels) != 3 || strings.Contains(all, "k8s_daemonset_name") || strings.Contains(all, "cart-app") {
		t.Fatalf("selectors:\n%s", all)
	}
	// A filter of the caller's own is ANDed with the places, not put in their stead.
	sels, _ = MetricFilter{Name: "up", Service: "search", Namespace: "shop"}.selectors(s)
	for _, sel := range sels {
		if !strings.Contains(sel, `service_name="search"`) || !strings.Contains(sel, `service_name=~"cart|web"`) {
			t.Fatalf("selector %s", sel)
		}
	}
	// Existing scopes are rendered exactly as before.
	if got := (Scope{Signals: Signals, Namespaces: []string{"shop"}}).places(); got.names != nil || got.exact != nil || got.none {
		t.Fatalf("a namespace-limited scope's places = %+v", got)
	}
}

// What the second checks hold a span's resource, a series and a line of an application token to.
func TestTheSecondChecksHoldEveryAnswerToTheExactPlaces(t *testing.T) {
	s := shopOnly(t)
	res := func(kvs ...string) map[string]any {
		m := map[string]any{}
		for i := 0; i < len(kvs); i += 2 {
			m[kvs[i]] = kvs[i+1]
		}
		return m
	}
	for _, c := range []struct {
		attrs map[string]any
		want  bool
		why   string
	}{
		{res(attrService, "cart", attrNamespace, "shop", attrCluster, "cl-1"), true, "a member where it runs"},
		{res(attrService, "cart-app", attrNamespace, "shop", attrCluster, "cl-1"), false, "a member's app label, which its workload sets for itself"},
		{res(attrService, "cart-7f9", attrNamespace, "shop", attrCluster, "cl-1", "k8s.deployment.name", "cart"), true, "a member by its workload"},
		{res(attrService, "x", attrNamespace, "shop", attrCluster, "cl-1", "k8s.statefulset.name", "cart"), false, "a member's name as another kind's workload"},
		{res(attrService, "x", attrNamespace, "shop", attrCluster, "cl-1", "k8s.statefulset.name", "web"), true, "a StatefulSet member by its workload"},
		{res(attrService, "x", attrNamespace, "shop", attrCluster, "cl-1", "k8s.daemonset.name", "web"), false, "a StatefulSet member's name as a DaemonSet"},
		{res(attrService, "cart", attrNamespace, "other", attrCluster, "cl-1"), false, "a member's name in another namespace"},
		{res(attrService, "cart", attrNamespace, "shop", attrCluster, "cl-2"), false, "a member's name in another cluster"},
		{res(attrService, "search", attrNamespace, "shop", attrCluster, "cl-1"), false, "another service of the namespace"},
		{res(attrService, "cart", attrCluster, "cl-1"), false, "no namespace"},
		{res(attrService, "cart", attrNamespace, "shop"), false, "no cluster"},
		{res(attrNamespace, "shop", attrCluster, "cl-1"), false, "no name"},
	} {
		if got := s.attrsVisible(c.attrs); got != c.want {
			t.Errorf("resource, %s: %v", c.why, got)
		}
	}
	for _, c := range []struct {
		labels map[string]string
		want   bool
	}{
		{map[string]string{"service_name": "cart", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, true},
		{map[string]string{"k8s_statefulset_name": "web", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, true},
		{map[string]string{"k8s_deployment_name": "web", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, false}, // not its kind
		{map[string]string{"k8s_deployment_name": "cart", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, true},
		{map[string]string{"service_name": "cart-app", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, false}, // an app label
		{map[string]string{"service_name": "cart", "k8s_namespace_name": "other", "ikhnos_cluster_id": "cl-1"}, false},
		{map[string]string{"service_name": "search", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, false},
		{map[string]string{"service_name": "cart", "ikhnos_cluster_id": "cl-1"}, false},
		{map[string]string{"k8s_node_name": "n1", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}, false}, // a node's own
	} {
		if got := s.seriesVisible(c.labels); got != c.want {
			t.Errorf("series %v: %v", c.labels, got)
		}
	}
	if !s.lineVisible(LogEntry{Service: "web", Namespace: "shop", Cluster: "cl-1"}) || s.lineVisible(LogEntry{Service: "web", Namespace: "kube-system", Cluster: "cl-1"}) ||
		s.lineVisible(LogEntry{Service: "search", Namespace: "shop", Cluster: "cl-1"}) || s.lineVisible(LogEntry{Service: "web", Namespace: "shop"}) {
		t.Error("lines")
	}
	// The topology's services by their id: one that merely has a member's name and place is not the member.
	if !s.serviceVisible(&TopoService{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1"}) ||
		s.serviceVisible(&TopoService{ID: "s-web2", Name: "web", Namespace: "shop", Cluster: "cl-1"}) ||
		s.serviceVisible(&TopoService{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1"}) ||
		s.serviceVisible(&TopoService{ID: "s-web", Name: "web", Namespace: "other", Cluster: "cl-1"}) || s.serviceVisible(nil) {
		t.Error("topology services")
	}
	// Telemetry that names no service is no application's.
	if s.visibleAt("shop", "cl-1") {
		t.Error("a place with no name")
	}
}

func appTrace() map[string]any {
	span := func(id, parent, name string) map[string]any {
		return map[string]any{"traceId": traceHex, "spanId": id, "parentSpanId": parent, "name": name,
			"startTimeUnixNano": "1791200000000000000", "endTimeUnixNano": "1791200000050000000", "status": map[string]any{}}
	}
	rs := func(span map[string]any, kvs ...string) any {
		var attrs []any
		for i := 0; i < len(kvs); i += 2 {
			attrs = append(attrs, kv(kvs[i], kvs[i+1]))
		}
		return map[string]any{"resource": map[string]any{"attributes": attrs}, "scopeSpans": []any{map[string]any{"spans": []any{span}}}}
	}
	return map[string]any{"trace": map[string]any{"resourceSpans": []any{
		rs(span("0000000000000001", "", "GET /"), "service.name", "gateway", "k8s.namespace.name", "edge", "ikhnos.cluster.id", "cl-1"),
		rs(span("0000000000000002", "0000000000000001", "GET /cart"), "service.name", "cart", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
		rs(span("0000000000000003", "0000000000000002", "search"), "service.name", "search", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
		rs(span("0000000000000004", "0000000000000002", "other cart"), "service.name", "cart", "k8s.namespace.name", "other", "ikhnos.cluster.id", "cl-1"),
		rs(span("0000000000000005", "0000000000000002", "unplaced"), "service.name", "cart"),
		rs(span("0000000000000006", "0000000000000002", "render"), "service.name", "web-7f9", "k8s.statefulset.name", "web", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
		rs(span("0000000000000007", "0000000000000002", "wrong kind"), "service.name", "web-x", "k8s.deployment.name", "web", "k8s.namespace.name", "shop", "ikhnos.cluster.id", "cl-1"),
	}}}
}

// A trace that crosses the application and other services of its namespace: only the application's spans, each in its own
// place, and the parent outside it is not named.
func TestATraceReadByAnApplicationTokenHoldsOnlyItsMembersSpans(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, appTrace()) }
	tr, err := f.client().Trace(context.Background(), shopOnly(t), traceHex)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sp := range tr.Spans {
		names = append(names, sp.Name)
		if sp.Name == "GET /cart" && sp.ParentSpanID != "" {
			t.Fatalf("the gateway's span id was given as the parent: %+v", sp)
		}
	}
	slices.Sort(names)
	if !reflect.DeepEqual(names, []string{"GET /cart", "render"}) || !reflect.DeepEqual(tr.Services, []string{"cart", "web-7f9"}) {
		t.Fatalf("spans %v, services %v", names, tr.Services)
	}
	// A token of another application reads the trace as not found, as a trace that does not exist.
	pay, _ := limit(Scope{Signals: Signals}, rightsGroups(), "app-pay")
	var e *Error
	if _, err := f.client().Trace(context.Background(), pay, traceHex); !errors.As(err, &e) || e.Status != http.StatusNotFound {
		t.Fatalf("another application's token: %v", err)
	}
}

// Many places: an application token is still read place by place (never by the union of its names, namespaces and
// clusters, which a list of names could not be checked against afterwards), and every answer is held to the exact places.
func TestPastMaxFocusPlacesAnApplicationTokenIsStillReadPlaceByPlace(t *testing.T) {
	var groups []AppGroup
	var ids []string
	for i := 0; i <= maxFocusPlaces; i++ {
		id := fmt.Sprintf("app-%d", i)
		groups = append(groups, AppGroup{ID: id, Name: id, Members: []AppMember{{ID: "s" + id, Name: fmt.Sprintf("svc%d", i), Namespace: fmt.Sprintf("ns%d", i), Cluster: "cl-1", Kind: "Deployment"}}})
		ids = append(ids, id)
	}
	s, _ := limit(Scope{Signals: Signals}, groups, ids...)
	if len(s.rights().exact) != maxFocusPlaces+1 || len(s.places().exact) != maxFocusPlaces+1 {
		t.Fatalf("rights %d places, query places %d", len(s.rights().exact), len(s.places().exact))
	}
	sels, _ := MetricFilter{Name: "up"}.selectors(s)
	if len(sels) != 2*(maxFocusPlaces+1) {
		t.Fatalf("%d selectors", len(sels))
	}
	place := regexp.MustCompile(`k8s_namespace_name="ns(\d+)",ikhnos_cluster_id="cl-1",(service_name|k8s_deployment_name)=~"svc(\d+)"}$`)
	for _, sel := range sels {
		if m := place.FindStringSubmatch(sel); m == nil || m[1] != m[3] {
			t.Fatalf("a selector that is not one place: %s", sel)
		}
	}
	q, _ := LogFilter{}.logQL(s)
	tq, _ := TraceFilter{}.traceQL(s)
	if !strings.Contains(q, `(k8s_namespace_name="ns20" and ikhnos_cluster_id="cl-1" and service_name=~"svc20")`) ||
		!strings.Contains(tq, `(resource.k8s.namespace.name = "ns20" && resource.ikhnos.cluster.id = "cl-1" && resource.service.name = "svc20")`) {
		t.Fatalf("logql %s\ntraceql %s", q, tq)
	}
	// svc0 belongs in ns0: in ns1 it is in the union of names and namespaces, and not the token's.
	right := map[string]string{"service_name": "svc0", "k8s_namespace_name": "ns0", "ikhnos_cluster_id": "cl-1"}
	wrong := map[string]string{"service_name": "svc0", "k8s_namespace_name": "ns1", "ikhnos_cluster_id": "cl-1"}
	f := newFake(t)
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/series":
			writeJSON(w, map[string]any{"status": "success", "data": []any{right, wrong}})
		default:
			writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{
				map[string]any{"metric": right, "values": []any{[]any{1791200000.0, "1"}}},
				map[string]any{"metric": wrong, "values": []any{[]any{1791200000.0, "2"}}},
			}}})
		}
	}
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lokiResult(
			[]any{map[string]string{"service_name": "svc0", "k8s_namespace_name": "ns0", "ikhnos_cluster_id": "cl-1"}, []any{"1791200000000000000", "mine"}},
			[]any{map[string]string{"service_name": "svc0", "k8s_namespace_name": "ns1", "ikhnos_cluster_id": "cl-1"}, []any{"1791200001000000000", "not mine"}},
		))
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) {
		hit := func(id, ns string) any {
			return map[string]any{"traceID": id, "startTimeUnixNano": "1791200000000000000", "durationMs": 5, "spanSet": map[string]any{"matched": 1, "spans": []any{
				map[string]any{"startTimeUnixNano": "1791200000000000000", "durationNanos": "1000", "attributes": []any{kv("service.name", "svc0"), kv("k8s.namespace.name", ns), kv("ikhnos.cluster.id", "cl-1")}}}}}
		}
		writeJSON(w, map[string]any{"traces": []any{hit("01", "ns0"), hit("02", "ns1")}})
	}
	c, ctx := f.client(), context.Background()
	series, _, err := c.Series(ctx, s, MetricFilter{Name: "up"}, rangeAll, 10)
	if err != nil || len(series) != 1 || series[0]["k8s_namespace_name"] != "ns0" {
		t.Fatalf("series = %v %v", series, err)
	}
	ms, _, err := c.MetricRange(ctx, s, MetricFilter{Name: "up"}, rangeAll, 60e9, 10)
	if err != nil || len(ms) != 1 || ms[0].Labels["k8s_namespace_name"] != "ns0" {
		t.Fatalf("range = %+v %v", ms, err)
	}
	lines, _, err := c.Logs(ctx, s, LogFilter{}, rangeAll, 10)
	if err != nil || len(lines) != 1 || lines[0].Line != "mine" {
		t.Fatalf("lines = %+v %v", lines, err)
	}
	hits, _, err := c.SearchTraces(ctx, s, TraceFilter{}, rangeAll, 10)
	if err != nil || len(hits) != 1 || !strings.HasSuffix(hits[0].TraceID, "01") {
		t.Fatalf("hits = %+v %v", hits, err)
	}
}

// Queries written by hand, the native APIs, the link figures and a fused read's own PromQL are refused, before any store is
// asked.
func TestAnApplicationTokenSendsNoQueryOfItsOwn(t *testing.T) {
	f := newFake(t)
	c, ctx, s := f.client(), context.Background(), shopOnly(t)
	var e *Error
	forbidden := func(what string, err error) {
		t.Helper()
		if !errors.As(err, &e) || e.Status != http.StatusForbidden || !strings.Contains(e.Msg, "limited to ikhnos applications") {
			t.Errorf("%s: %v", what, err)
		}
	}
	_, err := c.RawMetricAnswer(ctx, s, "query", url.Values{"query": {"up"}})
	forbidden("PromQL", err)
	_, err = c.RawMetricAnswer(ctx, s, "query_range", url.Values{"query": {"up"}, "start": {"1"}, "end": {"2"}, "step": {"1"}})
	forbidden("PromQL range", err)
	_, err = restrictPromQL(`sum(rate(x[1m]))`, s)
	forbidden("restrictPromQL", err)
	_, _, err = c.RawLogQuery(ctx, s, `{service_name="cart"}`, false, rangeAll, 10)
	forbidden("LogQL", err)
	_, _, err = c.RawTraceSearch(ctx, s, `{ true }`, rangeAll, 10)
	forbidden("TraceQL", err)
	for _, st := range []string{MirrorPrometheus, MirrorLoki, MirrorTempo} {
		_, err = c.Mirror(ctx, s, st, "/api/v1/labels", httptest.NewRequest("GET", "/x", nil))
		forbidden("native "+st, err)
	}
	forbidden("links", s.NeedLinks())
	opts, err := ParseFuseOptions(url.Values{"promql": {"cpu=rate(x[1m])"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.FuseTrace(ctx, s, traceHex, opts)
	forbidden("fused promql", err)
	items := c.FuseMany(ctx, s, []string{traceHex}, opts, nil)
	if len(items) != 1 || items[0].Status != http.StatusForbidden {
		t.Errorf("batch promql: %+v", items)
	}
	if len(f.reqs) != 0 {
		t.Fatalf("a refused query reached a store: %v", f.paths())
	}
}

func TestAnApplicationTokenKnowsOfItsOwnApplicationsOnly(t *testing.T) {
	groups := append(rightsGroups(), AppGroup{ID: "app-shop2", Name: "Shop too", Members: []AppMember{{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1"}}})
	s, _ := limit(Scope{Signals: Signals}, groups, "app-shop", "app-loose")
	vis := VisibleGroups(groups, s)
	if len(vis) != 1 || vis[0].ID != "app-shop" || len(vis[0].Members) != 2 {
		t.Fatalf("visible = %+v", vis)
	}
	// A member added since the rights were worked out is not one until the rights are worked out again.
	grown := rightsGroups()
	grown[0].Members = append(grown[0].Members, AppMember{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1"})
	if vis := VisibleGroups(grown, s); len(vis[0].Members) != 2 {
		t.Fatalf("visible = %+v", vis)
	}
	// application= is one of its own, inside the rights; any other reads as not found.
	if _, err := FindGroup(vis, "app-pay"); err == nil {
		t.Fatal("found another application")
	}
	fs, err := s.FocusOn(&vis[0])
	if err != nil || !reflect.DeepEqual(fs.Focus, s.RightsPlaces) || !fs.AppLimited {
		t.Fatalf("focus = %+v %v", fs, err)
	}
	// A focus reaching past the rights (a service named with no place, as /services/{name} does) is cut to them.
	fs, err = s.FocusOn(&AppGroup{Name: "cart", Members: []AppMember{{Name: "cart"}}})
	if err != nil || !reflect.DeepEqual(fs.Focus, []FocusPlace{{Namespace: "shop", Cluster: "cl-1", Names: []string{"cart"}, Workloads: map[string][]string{"k8s_deployment_name": {"cart"}}}}) {
		t.Fatalf("focus = %+v %v", fs.Focus, err)
	}
	var e *Error
	if _, err := s.FocusOn(&AppGroup{Name: "cart", Members: []AppMember{{Name: "cart", Namespace: "other", Cluster: "cl-1"}}}); !errors.As(err, &e) || e.Status != 404 {
		t.Fatalf("a focus outside the rights: %v", err)
	}
}

func TestAnApplicationTokenSeesOnlyTheChangesOfItsServicesAndTheirLinks(t *testing.T) {
	s := shopOnly(t)
	byID := map[string]*TopoService{"s-cart": {ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1"},
		"s-search": {ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1"}, "s-web": {ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1"}}
	link := map[string]TopoLink{"l-1": {ID: "l-1", From: "s-web", To: "s-cart"}, "l-2": {ID: "l-2", From: "s-cart", To: "s-search"}}
	for _, c := range []struct {
		e    ChangeEvent
		want bool
	}{
		{ChangeEvent{TargetKind: "service", TargetID: "s-cart", Cluster: "cl-1"}, true},
		{ChangeEvent{TargetKind: "service", TargetID: "s-search", Cluster: "cl-1"}, false},
		{ChangeEvent{TargetKind: "dependency", TargetID: "l-1"}, true},
		{ChangeEvent{TargetKind: "dependency", TargetID: "l-2"}, false},
		{ChangeEvent{TargetKind: "node", TargetID: "n1", Cluster: "cl-1"}, false},
		{ChangeEvent{TargetKind: "cluster", TargetID: "cl-1", Cluster: "cl-1"}, false},
		{ChangeEvent{TargetKind: "application", TargetID: "app-shop"}, false},
	} {
		if got := changeVisible(s, c.e, byID, link); got != c.want {
			t.Errorf("%+v: %v", c.e, got)
		}
	}
}

// The fused read of an application token: the trace's spans are its own, its lines are read within its places, the system
// lines are not read at all, and the node view is said to be empty.
func TestAFusedReadOfAnApplicationTokenJoinsOnlyWhatItMaySee(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, appTrace()) }
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lokiResult(
			[]any{map[string]string{"service_name": "cart", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"},
				[]any{"1791200000000000000", "mine", map[string]any{"structuredMetadata": map[string]string{"trace_id": traceHex, "span_id": "0000000000000002"}}}},
			[]any{map[string]string{"service_name": "cart", "k8s_namespace_name": "other", "ikhnos_cluster_id": "cl-1"},
				[]any{"1791200000000000000", "the other cart", map[string]any{"structuredMetadata": map[string]string{"trace_id": traceHex, "span_id": "0000000000000004"}}}},
		))
	}
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{}}})
	}
	opts, err := ParseFuseOptions(url.Values{"include": {"logs,system_logs,metrics"}, "metric_scope": {"app,node"}})
	if err != nil {
		t.Fatal(err)
	}
	fz, err := f.client().FuseTrace(context.Background(), shopOnly(t), traceHex, opts)
	if err != nil {
		t.Fatal(err)
	}
	if fz.Logs.Total != 1 || fz.Logs.Matched != 1 || fz.Sources[SourceSystemLogs] != SourceNotAllowed || fz.SystemLogs != nil {
		t.Fatalf("logs %+v, sources %v", fz.Logs, fz.Sources)
	}
	all := strings.Join(fz.Warnings, "\n")
	if !strings.Contains(all, "system_logs: a token limited to ikhnos applications") || !strings.Contains(all, "node view") {
		t.Fatalf("warnings = %v", fz.Warnings)
	}
	q := f.last("/loki/api/v1/query_range").Get("query")
	if !strings.Contains(q, `(k8s_namespace_name="shop" and ikhnos_cluster_id="cl-1" and service_name=~"cart|web")`) || strings.Contains(q, "kube-system") {
		t.Fatalf("the trace's lines were read with %s", q)
	}
	for _, r := range f.reqs {
		if strings.Contains(r.Form.Get("query"), "kube-system") {
			t.Fatalf("the system lines were read: %s", r.Form.Get("query"))
		}
	}
}

// A token is pinned to the members its applications had when it was made, whatever brought them there: a member that joins
// afterwards (by its labels, or put there by a person) gives it nothing, one that leaves takes its rights with it, and one
// that moves, is renamed or changes kind is visible neither where it was nor where it is. Its app labels never count.
func TestATokensRightsAreOnlyTheMembersItWasMadeWith(t *testing.T) {
	groups := rightsGroups()
	pinned := Scope{}.ApplicationMembers(groups, []string{"app-shop"})
	if !reflect.DeepEqual(pinned, []PinnedMember{
		{Application: "app-shop", ID: "s-cart", Name: "cart", Kind: "Deployment", Namespace: "shop", Cluster: "cl-1"},
		{Application: "app-shop", ID: "s-web", Name: "web", Kind: "StatefulSet", Namespace: "shop", Cluster: "cl-1"}}) {
		t.Fatalf("pinned = %+v", pinned)
	}
	now := func(edit func(g *AppGroup)) Scope {
		g := rightsGroups()
		edit(&g[0])
		s, _ := Scope{Signals: Signals}.LimitToApplications(g, []string{"app-shop"}, pinned)
		return s
	}
	line := func(svc, ns string) LogEntry { return LogEntry{Service: svc, Namespace: ns, Cluster: "cl-1"} }
	// Joined since, by its labels (a Helm release of the application's name) in another namespace, and in the same one.
	s := now(func(g *AppGroup) {
		g.Members = append(g.Members, AppMember{ID: "s-redis-b", Name: "redis", Namespace: "team-b", Cluster: "cl-1", Kind: "StatefulSet"},
			AppMember{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment"})
	})
	if s.lineVisible(line("redis", "team-b")) || s.lineVisible(line("search", "shop")) || slices.Contains(s.RightsServiceIDs, "s-search") ||
		!s.lineVisible(line("cart", "shop")) || s.lineVisible(line("cart-app", "shop")) {
		t.Fatalf("after joins: %+v %v", s.RightsPlaces, s.RightsServiceIDs)
	}
	if vis := VisibleGroups(append(rightsGroups()[:0:0], AppGroup{ID: "app-shop", Members: append(rightsGroups()[0].Members, AppMember{ID: "s-search", Name: "search", Namespace: "shop", Cluster: "cl-1"})}), s); len(vis[0].Members) != 2 {
		t.Fatalf("visible = %+v", vis)
	}
	// Left (its label removed, so ikhnos dropped it from the application).
	s = now(func(g *AppGroup) { g.Members = g.Members[1:] })
	if s.lineVisible(line("cart", "shop")) || !s.lineVisible(line("web", "shop")) {
		t.Fatal("a member that left is still visible")
	}
	// Moved to another namespace: neither there nor where it was.
	s = now(func(g *AppGroup) { g.Members[0].Namespace = "team-b" })
	if s.lineVisible(line("cart", "shop")) || s.lineVisible(line("cart", "team-b")) {
		t.Fatal("a member that moved is visible")
	}
	// Renamed, or another kind under the same id.
	s = now(func(g *AppGroup) { g.Members[0].Name = "basket" })
	if s.lineVisible(line("basket", "shop")) || s.lineVisible(line("cart", "shop")) {
		t.Fatal("a member that was renamed is visible")
	}
	s = now(func(g *AppGroup) { g.Members[0].Kind = "DaemonSet" })
	if s.seriesVisible(map[string]string{"k8s_daemonset_name": "cart", "k8s_namespace_name": "shop", "ikhnos_cluster_id": "cl-1"}) || s.lineVisible(line("cart", "shop")) {
		t.Fatal("a member of another kind is visible")
	}
	// Pinned under another application: not this one's.
	other := []PinnedMember{pinned[0]}
	other[0].Application = "app-pay"
	if s, _ := (Scope{Signals: Signals}).LimitToApplications(rightsGroups(), []string{"app-shop"}, other); !s.rights().none {
		t.Fatalf("a member pinned for another application: %+v", s.RightsPlaces)
	}
	// No pinned member: nothing at all.
	if none, _ := (Scope{Signals: Signals}).LimitToApplications(rightsGroups(), []string{"app-shop"}, nil); !none.rights().none {
		t.Fatalf("no pin: %+v", none.RightsPlaces)
	}
	// What a token would be pinned to stays within its limits, and leaves out a member ikhnos knows no place for.
	if got := (Scope{Namespaces: []string{"pay"}}).ApplicationMembers(rightsGroups(), []string{"app-shop", "app-pay", "app-loose"}); len(got) != 1 || got[0].ID != "s-pay" {
		t.Fatalf("pinned within the limits = %+v", got)
	}
}

// The topology a fused read joins to a resource names only the token's own applications among the service's, and takes a
// service, or a neighbour, as a member by its id only.
func TestTheJoinedTopologyNamesOnlyTheTokensOwnApplications(t *testing.T) {
	s := shopOnly(t)
	v := &TopologyView{
		Services: []TopoService{
			{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1", Applications: []string{"Shop", "Secret project"}, AppIDs: []string{"app-shop", "app-secret"}},
			{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Applications: []string{"Secret project", "Shop"}, AppIDs: []string{"app-secret", "app-shop"}},
			{ID: "s-web2", Name: "web", Namespace: "shop", Cluster: "cl-1"}, // the name of a member, not the member
		},
		Links: []TopoLink{{ID: "d1", From: "s-cart", To: "s-web", FromKind: "service", ToKind: "service"}, {ID: "d2", From: "s-cart", To: "s-web2", FromKind: "service", ToKind: "service"}},
	}
	rt := newTopoIndex(v).topologyFor(s, &Resource{Service: "cart", Namespace: "shop", Cluster: "cl-1"})
	if rt == nil || !reflect.DeepEqual(rt.Service.Applications, []string{"Shop"}) || !reflect.DeepEqual(rt.Service.AppIDs, []string{"app-shop"}) {
		t.Fatalf("topology = %+v", rt)
	}
	if len(rt.Calls) != 1 || rt.Calls[0].ID != "s-web" {
		t.Fatalf("calls = %+v", rt.Calls)
	}
	if v.Services[0].Applications[1] != "Secret project" {
		t.Fatal("the shared view was changed")
	}
	// The application's own topology: the neighbour names only the token's applications.
	pay := rightsGroups()[0]
	pay.Members = pay.Members[:1]
	at := BuildAppTopology(&pay, v, s, false)
	if len(at.Neighbours) != 1 || at.Neighbours[0].ID != "s-web" || !reflect.DeepEqual(at.Neighbours[0].Applications, []string{"Shop"}) {
		t.Fatalf("neighbours = %+v", at.Neighbours)
	}
	// A member the topology has under another id is not taken for the service of its key.
	moved := rightsGroups()[0]
	moved.Members = []AppMember{{ID: "s-cart-old", Name: "cart", Namespace: "shop", Cluster: "cl-1"}}
	s2, _ := limit(Scope{Signals: Signals}, []AppGroup{moved}, "app-shop")
	at = BuildAppTopology(&moved, v, s2, false)
	if at.Services[0].ID != "s-cart-old" || len(at.Links) != 0 || slices.Contains(at.ids, "s-cart") {
		t.Fatalf("topology = %+v", at)
	}
}

// Past maxFocusPlaces the lists of names alone are still read place by place: the service names of the metrics are asked with
// one selector per place, and those of the logs from the lines themselves (checked against the places), not by the union.
func TestPastMaxFocusPlacesTheListsOfNamesAreExact(t *testing.T) {
	var groups []AppGroup
	var ids []string
	for i := 0; i <= maxFocusPlaces; i++ {
		id := fmt.Sprintf("app-%d", i)
		groups = append(groups, AppGroup{ID: id, Name: id, Members: []AppMember{{ID: "s" + id, Name: fmt.Sprintf("svc%d", i), Namespace: fmt.Sprintf("ns%d", i), Cluster: "cl-1"}}})
		ids = append(ids, id)
	}
	s, _ := limit(Scope{Signals: Signals}, groups, ids...)
	f := newFake(t)
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "success", "data": []string{"svc0"}})
	}
	f.loki = func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, lokiResult(
			[]any{map[string]string{"service_name": "svc0", "k8s_namespace_name": "ns0", "ikhnos_cluster_id": "cl-1"}, []any{"1791200000000000000", "mine"}},
			[]any{map[string]string{"service_name": "svc0", "k8s_namespace_name": "ns1", "ikhnos_cluster_id": "cl-1"}, []any{"1791200001000000000", "LEAK"}},
			[]any{map[string]string{"service_name": "svc2", "k8s_namespace_name": "ns1", "ikhnos_cluster_id": "cl-1"}, []any{"1791200002000000000", "LEAK"}},
		))
	}
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"traces": []any{}}) }
	apps, _, _, err := f.client().Applications(context.Background(), s, rangeAll)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Name != "svc0" || !reflect.DeepEqual(apps[0].Signals, []Signal{SignalMetrics, SignalLogs}) {
		t.Fatalf("services = %+v", apps)
	}
	// The label-values call is GET only: its selectors are asked a batch at a time, each within the bound, and all are asked.
	asked := func(path string) (sels []string, calls int) {
		for _, r := range f.reqs {
			if r.URL.Path == path {
				calls++
				if r.Method != http.MethodGet || len(r.URL.RawQuery) > maxGetQuery {
					t.Fatalf("%s %s: %d bytes", r.Method, path, len(r.URL.RawQuery))
				}
				sels = append(sels, r.URL.Query()["match[]"]...)
			}
		}
		return sels, calls
	}
	if sels, calls := asked("/api/v1/label/" + lblService + "/values"); len(sels) != maxFocusPlaces+1 || len(slices.Compact(slices.Sorted(slices.Values(sels)))) != maxFocusPlaces+1 || calls < 2 {
		t.Fatalf("the metric services were asked with %d selectors in %d calls", len(sels), calls)
	}
	if q := f.last("/loki/api/v1/query_range").Get("query"); !strings.Contains(q, `(k8s_namespace_name="ns20" and ikhnos_cluster_id="cl-1" and service_name=~"svc20")`) {
		t.Fatalf("the log services were read with %s", q)
	}
	for _, r := range f.reqs {
		if strings.HasPrefix(r.URL.Path, "/loki/api/v1/label/") {
			t.Fatalf("past the bound the log services were read by label values: %v", r.URL)
		}
	}
	names, _, err := f.client().MetricNames(context.Background(), s, MetricFilter{}, rangeAll, 10)
	if err != nil || len(names) != 1 {
		t.Fatalf("names = %v %v", names, err)
	}
	if sels, _ := asked("/api/v1/label/__name__/values"); len(sels) != maxFocusPlaces+1 { // (members of no workload kind: service_name alone)
		t.Fatalf("the metric names were asked with %d selectors", len(sels))
	}
}

// Batches of the label-values call are united, and a limit is the cut of the sorted union: the same answer one call with
// every selector would give.
func TestTheBatchesOfALabelValuesCallAreOneAnswer(t *testing.T) {
	f := newFake(t)
	f.prom = func(w http.ResponseWriter, r *http.Request) {
		// Each batch answers the values of its own selectors: the n of svcN, sorted, at most limit of them.
		var vals []string
		for _, m := range r.URL.Query()["match[]"] {
			vals = append(vals, "v"+strings.TrimSuffix(m[strings.LastIndex(m, "svc")+3:], `"}`))
		}
		slices.Sort(vals)
		if l, _ := strconv.Atoi(r.URL.Query().Get("limit")); l > 0 && len(vals) > l {
			vals = vals[:l]
		}
		writeJSON(w, map[string]any{"status": "success", "data": vals})
	}
	var sels []string
	for i := 0; i < 200; i++ {
		sels = append(sels, fmt.Sprintf(`{k8s_namespace_name="ns%03d",service_name=~"svc%03d"}`, i, i))
	}
	got, err := f.client().promLabelValues(context.Background(), "__name__", url.Values{"match[]": sels, "limit": {"6"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.reqs) < 2 {
		t.Fatalf("one call of %d selectors", len(sels))
	}
	got, truncated := cutAt(got, 5)
	if !reflect.DeepEqual(got, []string{"v000", "v001", "v002", "v003", "v004"}) || !truncated {
		t.Fatalf("got %v %v", got, truncated)
	}
}
