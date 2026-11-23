package server

import (
	"context"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/fusionhttp"
	"ikhnos/internal/landscape"
	"ikhnos/internal/model"
	"ikhnos/internal/store"
)

func TestYourPromQLIsExpandedPerResourceAndReachesPrometheus(t *testing.T) {
	d := newDataRig(t)
	q := `errs=sum(rate(http_errors_total{service_name="${service}",k8s_namespace_name="${namespace}"}[1m]))`
	r := d.get("/api/v1/fusion/traces/"+testTraceID+"?promql="+urlEscape(q)+"&promql_spans=true", withCookie(d.admin))
	if r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	j := r.json(t)
	src := j["sources"].(map[string]any)
	if src["promql"] != "ok" || src["logs"] != "not requested" || src["metrics"] != "not requested" {
		t.Fatalf("sources %v", src)
	}
	if !d.stores.askedAbout(urlEscape(`http_errors_total{service_name="cart",k8s_namespace_name="shop"}`)) ||
		!d.stores.askedAbout(urlEscape(`http_errors_total{service_name="audit",k8s_namespace_name="secret"}`)) {
		t.Fatalf("expanded queries were not sent: %v", d.stores.asked)
	}
	res := j["resources"].([]any)[0].(map[string]any)
	qs := res["queries"].([]any)
	if len(qs) != 1 || qs[0].(map[string]any)["name"] != "errs" || !strings.Contains(qs[0].(map[string]any)["query"].(string), `service_name="`) {
		t.Fatalf("resource queries: %v", res)
	}

	// The batch body takes an object of name to expression, and a list.
	before := d.stores.count()
	for _, body := range []map[string]any{
		{"ids": []string{testTraceID}, "promql": map[string]string{"errs": `up{service_name="${service}"}`}, "include": []string{"none"}},
		{"ids": []string{testTraceID}, "promql": []string{`errs=up{service_name="${service}"}`, `all=sum(up)`}},
	} {
		r := d.post("/api/v1/fusion/traces/batch", body, withCookie(d.admin))
		if r.Code != 200 {
			t.Fatalf("%v: %d %s", body, r.Code, r.Body.String())
		}
		tr := r.json(t)["results"].([]any)[0].(map[string]any)["trace"].(map[string]any)
		if tr["sources"].(map[string]any)["promql"] != "ok" {
			t.Fatalf("batch sources %v", tr["sources"])
		}
	}
	if d.stores.count() == before {
		t.Fatal("batch read nothing")
	}
	// A query with a variable that does not exist is the caller's mistake, said before any read.
	before = d.stores.count()
	for _, bad := range []string{`/api/v1/fusion/traces/` + testTraceID + `?promql=` + urlEscape(`x=up{a="${nope}"}`), `/api/v1/fusion/traces/` + testTraceID + `?promql=up`} {
		if r := d.get(bad, withCookie(d.admin)); r.Code != 400 {
			t.Errorf("%s: %d", bad, r.Code)
		}
	}
	if r := d.post("/api/v1/fusion/traces/batch", map[string]any{"ids": []string{testTraceID}, "promql": 5}, withCookie(d.admin)); r.Code != 400 {
		t.Errorf("a number as promql: %d", r.Code)
	}
	if d.stores.count() != before {
		t.Fatal("a refused request reached a store")
	}
}

// A limited token's own PromQL is read with its namespaces on every selector; an unlimited one's as written.
func TestYourPromQLIsReadWithinTheTokensLimits(t *testing.T) {
	d := newDataRig(t)
	limited, _ := d.mint(t, map[string]any{"name": "shop", "namespaces": []string{"shop"}})
	unlimited, _ := d.mint(t, map[string]any{"name": "all"})
	path := "/api/v1/fusion/traces/" + testTraceID + "?promql=" + urlEscape(`n=up{service_name="${service}"}`)

	before := d.stores.count()
	j := d.get(path, bearer(limited)).json(t)
	if j["sources"].(map[string]any)["promql"] != "ok" {
		t.Fatalf("%v", j["sources"])
	}
	if !d.stores.askedAbout(urlEscape(`k8s_namespace_name=~"shop"`)) {
		t.Fatalf("a limited token's query must carry its namespaces: %v", d.stores.asked[before:])
	}
	if j := d.get(path, bearer(unlimited)).json(t); j["sources"].(map[string]any)["promql"] != "ok" {
		t.Fatalf("%v", j["sources"])
	}
}

func TestTopologyAndChangesAreOfferedAndReportedHonestly(t *testing.T) {
	d := newDataRig(t)
	j := d.get("/api/v1/fusion/traces/"+testTraceID+"?include=topology,changes", withCookie(d.admin)).json(t)
	src := j["sources"].(map[string]any)
	if src["topology"] != "ok" || src["changes"] != "ok" || src["logs"] != "not requested" {
		t.Fatalf("sources %v warnings %v", src, j["warnings"])
	}
	// No service of the fixture is in the (empty) topology: said, not silent.
	if !strings.Contains(strings.Join(anyStrings(j["warnings"]), " "), "topology") || j["joins"].(map[string]any)["topology"] == nil {
		t.Fatalf("%v", j)
	}
}

func anyStrings(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, e := range l {
		out = append(out, e.(string))
	}
	return out
}

func TestTheTopologyViewCarriesWhatTheFusedReadJoins(t *testing.T) {
	doc := landscape.StateDoc{GeneratedAt: "2026-10-05T12:00:00Z", Topology: model.Topology{
		Services: []model.Service{{ID: "s1", Name: "cart", Namespace: "shop", ClusterID: "cl-1", Kind: "Deployment", Replicas: 3, ReadyReplicas: 2, Restarts: 5, Labels: map[string]string{"app": "cart"},
			NodeIDs: []string{"n1"}, Pods: []model.Pod{{Name: "cart-1", NodeID: "n1"}, {Name: "cart-2", NodeID: "n1"}, {Name: "cart-3"}}}},
		Nodes:    []model.Node{{ID: "n1", Name: "node-a", ClusterID: "cl-1", Role: "worker", Zone: "z1"}},
		Clusters: []model.Cluster{{ID: "cl-1", Name: "athens-edge", Tier: "edge"}},
		Dependencies: []model.Dependency{{ID: "d1", From: "s1", FromKind: "service", To: "x1", ToKind: "external", Protocol: "tcp", Port: 443, Confidence: "high", Noise: ""},
			{From: "s1", FromKind: "service", To: "s9", ToKind: "service", Noise: "dns"}},
		ExternalEndpoints: []model.ExternalEndpoint{{ID: "x1", Host: "198.51.100.7", Port: 443}, {ID: "x2", Host: "h", Name: "GitHub"}},
	}}
	v := topologyView(doc)
	if len(v.Services) != 1 || v.Services[0].Replicas != 3 || v.Services[0].Ready != 2 || v.Services[0].Restarts != 5 || v.Services[0].Cluster != "cl-1" || v.Services[0].Labels["app"] != "cart" {
		t.Fatalf("%+v", v.Services)
	}
	if len(v.Nodes) != 1 || v.Nodes[0].ID != "n1" || v.Nodes[0].Name != "node-a" || v.Nodes[0].Cluster != "cl-1" || v.Links[0].ID != "d1" {
		t.Fatalf("%+v %+v", v.Nodes, v.Links)
	}
	if sv := v.Services[0]; !slices.Equal(sv.NodeIDs, []string{"n1"}) || sv.PodsOnNode["n1"] != 2 || len(sv.PodsOnNode) != 1 {
		t.Fatalf("where the service runs: %+v", sv)
	}
	if v.Nodes[0].Role != "worker" || v.Nodes[0].Zone != "z1" || len(v.Clusters) != 1 || v.Clusters[0] != (fusionapi.TopoCluster{ID: "cl-1", Name: "athens-edge", Tier: "edge"}) {
		t.Fatalf("%+v %+v", v.Nodes, v.Clusters)
	}
	if len(v.Links) != 2 || v.Links[0].Port != 443 || v.Links[1].Noise != "dns" || v.Externals["x1"] != "198.51.100.7:443" || v.Externals["x2"] != "GitHub" || v.At.IsZero() {
		t.Fatalf("%+v %v", v.Links, v.Externals)
	}
}

func TestChangesComeFromTheEventStoreNarrowedToTheClusters(t *testing.T) {
	a := newAdminRig(t)
	at := time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)
	if err := a.st.AddEvents(a.ctx, "org-1", []store.Event{
		{At: at, Kind: "service-scaled", TargetKind: "service", TargetID: "s1", Name: "cart", ClusterID: "cl-1", ClusterName: "prod", Detail: "2 to 3", Cause: "autoscaler", Severity: "info"},
		{At: at.Add(time.Minute), Kind: "node-status", TargetKind: "node", TargetID: "n1", Name: "n1", ClusterID: "cl-2"},
		{At: at.Add(-3 * time.Hour), Kind: "old", TargetKind: "node", TargetID: "n1", ClusterID: "cl-1"},
	}); err != nil {
		t.Fatal(err)
	}
	ex := a.a.fusionExtras()
	if ex == nil {
		t.Fatal("a platform has extras")
	}
	got, err := ex.Changes(context.Background(), at.Add(-time.Hour), at.Add(time.Hour), []string{"cl-1"}, nil, 100)
	if err != nil || len(got) != 1 || got[0].Kind != "service-scaled" || got[0].Cluster != "cl-1" || got[0].Cause != "autoscaler" || got[0].Detail != "2 to 3" {
		t.Fatalf("%+v %v", got, err)
	}
	if all, _ := ex.Changes(context.Background(), at.Add(-time.Hour), at.Add(time.Hour), nil, nil, 100); len(all) != 2 {
		t.Fatalf("%+v", all)
	}
	// Asked for the events about some targets, the store reads those and no others.
	if some, _ := ex.Changes(context.Background(), at.Add(-time.Hour), at.Add(time.Hour), nil, []string{"n1", "nothing"}, 100); len(some) != 1 || some[0].TargetID != "n1" {
		t.Fatalf("%+v", some)
	}
	if (&Admin{}).fusionExtras() != nil {
		t.Fatal("a server with no platform has no extras")
	}
	var _ fusionapi.Extras = fusionExtras{}
}

func urlEscape(s string) string { return url.QueryEscape(s) }

func TestTheBatchBodySchemaNamesEveryFusedOption(t *testing.T) {
	spec := fusionhttp.OpenAPI(fusionOps(nil))
	props := spec["components"].(obj)["schemas"].(obj)["BatchRequest"].(map[string]any)["properties"].(map[string]any)
	for _, n := range fusionapi.FuseParamNames {
		if n == fusionapi.ParamFused {
			continue // a batch is always fused
		}
		if _, ok := props[n]; !ok {
			t.Errorf("BatchRequest does not describe %q", n)
		}
	}
}

// The applications are worked out from the topology and the saved workspace, which is not cheap, and a dashboard asks on every
// refresh: the answer is reused for appGroupTTL and worked out again after it; a failure is never kept.
func TestTheApplicationsAreReusedForAShortWhile(t *testing.T) {
	d := newDataRig(t)
	if r := d.get("/api/v1/fusion/applications", withCookie(d.admin)); r.Code != 200 {
		t.Fatalf("%d %s", r.Code, r.Body.String())
	}
	c := &d.a.appCache
	c.mu.Lock()
	if c.at.IsZero() {
		c.mu.Unlock()
		t.Fatal("the first answer was not kept")
	}
	c.val = []fusionapi.AppGroup{{ID: "from-cache", Name: "from-cache", Members: []fusionapi.AppMember{{Name: "cached"}}}}
	c.mu.Unlock()

	r := d.get("/api/v1/fusion/applications", withCookie(d.admin))
	if !strings.Contains(r.Body.String(), "from-cache") {
		t.Fatalf("a second read within the TTL did not use the kept answer: %s", r.Body.String())
	}
	*d.now = d.now.Add(appGroupTTL + time.Second)
	r = d.get("/api/v1/fusion/applications", withCookie(d.admin))
	if r.Code != 200 || strings.Contains(r.Body.String(), "from-cache") {
		t.Fatalf("after the TTL the answer was not worked out again: %d %s", r.Code, r.Body.String())
	}
}

// A fused read with include=topology takes the applications from the reused answer, so reading many traces does not read and
// parse the saved workspace once each.
func TestTheTopologyUsesTheReusedApplications(t *testing.T) {
	d := newDataRig(t)
	ex := fusionExtras{d.a}
	if _, err := ex.Topology(context.Background()); err != nil {
		t.Fatal(err)
	}
	c := &d.a.appCache
	c.mu.Lock()
	if c.at.IsZero() {
		c.mu.Unlock()
		t.Fatal("the topology did not go through the kept applications")
	}
	kept := c.at.Add(-time.Second) // (the rig's clock stands still; this tells a reuse from a new reading)
	c.at = kept
	c.mu.Unlock()
	if _, err := ex.Topology(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.at.Equal(kept) {
		t.Fatal("a second topology within the TTL worked the applications out again")
	}
}

// An empty list of targets asks for the events of none; the store reads it as no filter, which would hand a caller who may see none
// of an application's services every event of the organisation. And a cluster filter must not hide the events about an application,
// which carry no cluster, from a question that names the application.
func TestAnEmptyListOfTargetsIsNoEventsAndTheTargetsNeedNoClusterFilter(t *testing.T) {
	a := newAdminRig(t)
	at := time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)
	if err := a.st.AddEvents(a.ctx, "org-1", []store.Event{
		{At: at, Kind: "application-membership", TargetKind: "application", TargetID: "app-1", Name: "Shop"},
		{At: at, Kind: "service-scaled", TargetKind: "service", TargetID: "sv-1", Name: "web", ClusterID: "cl-1"},
		{At: at, Kind: "service-scaled", TargetKind: "service", TargetID: "sv-2", Name: "payroll", ClusterID: "cl-2"},
	}); err != nil {
		t.Fatal(err)
	}
	ex := a.a.fusionExtras()
	since, until := at.Add(-time.Hour), at.Add(time.Hour)
	if got, err := ex.Changes(context.Background(), since, until, nil, []string{}, 100); err != nil || len(got) != 0 {
		t.Fatalf("an empty list of targets returned %+v, %v", got, err)
	}
	if got, _ := ex.Changes(context.Background(), since, until, nil, nil, 100); len(got) != 3 {
		t.Fatalf("a nil list of targets is every target: %+v", got)
	}
	got, err := ex.Changes(context.Background(), since, until, []string{"cl-1"}, []string{"sv-1", "app-1"}, 100)
	if err != nil || len(got) != 2 {
		t.Fatalf("the service and the application it is in, asked with the cluster of the trace: %+v, %v", got, err)
	}
}

// Every trace and log read carries the names of the organisation and its clusters, and reading them builds the state document
// and reads the organisation's record: they are reused for appGroupTTL, like the applications, and read again after it.
func TestTheNamesOfTheOrganisationAndItsClustersAreReusedForAShortWhile(t *testing.T) {
	d := newDataRig(t)
	ex := fusionExtras{d.a}
	var _ fusionapi.Namer = ex
	n, err := ex.Names(context.Background())
	if err != nil || n.Org == nil || n.Org.ID == "" || n.Clusters == nil {
		t.Fatalf("%+v %v", n, err)
	}
	c := &d.a.nameCache
	c.mu.Lock()
	if c.at.IsZero() {
		c.mu.Unlock()
		t.Fatal("the first answer was not kept")
	}
	c.val = fusionapi.Names{Clusters: map[string]string{"cl-kept": "from-cache"}}
	c.mu.Unlock()
	if n, _ := ex.Names(context.Background()); n.Clusters["cl-kept"] != "from-cache" {
		t.Fatalf("a second read within the TTL did not use the kept answer: %+v", n)
	}
	// Past the TTL the kept names are still given, at once, while they are read again for the next caller.
	*d.now = d.now.Add(appGroupTTL + time.Second)
	if n, _ := ex.Names(context.Background()); n.Clusters["cl-kept"] != "from-cache" {
		t.Fatalf("a stale answer was not given while the names were read again: %+v", n)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		n, _ := ex.Names(context.Background())
		if n.Clusters["cl-kept"] == "" && n.Org != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("after the TTL the names were not read again: %+v", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Past what may be given stale, a caller waits for the names to be read.
	c.mu.Lock()
	c.val = fusionapi.Names{Clusters: map[string]string{"cl-kept": "from-cache"}}
	c.mu.Unlock()
	*d.now = d.now.Add(appGroupTTL + namesStale + time.Second)
	if n, _ := ex.Names(context.Background()); n.Clusters["cl-kept"] != "" || n.Org == nil {
		t.Fatalf("names too old to give were given: %+v", n)
	}
}
