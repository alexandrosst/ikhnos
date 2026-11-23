package fusionapi

import (
	"strings"
	"testing"
)

// Two members in two places: api in shop on c1, redis in data on c2. A query made of the union of their names,
// namespaces and clusters would also take a redis of shop on c1, or an api of data on c2, for part of the application.
func twoPlaceFocus(t *testing.T) Scope {
	t.Helper()
	g := &AppGroup{ID: "app", Name: "Shop", Members: []AppMember{
		{Name: "api", Namespace: "shop", Cluster: "c1", Aliases: []string{"api-v2"}},
		{Name: "redis", Namespace: "data", Cluster: "c2"},
	}}
	s, err := AllSignals().FocusOn(g)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Focus) != 2 {
		t.Fatalf("focus = %+v", s.Focus)
	}
	return s
}

func TestAnApplicationsMetricsAreEachMemberWhereItRuns(t *testing.T) {
	sels, err := MetricFilter{Name: "up"}.selectors(twoPlaceFocus(t))
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(sels, "\n")
	for _, want := range []string{
		`k8s_namespace_name="shop",ikhnos_cluster_id="c1",service_name=~"api|api-v2"`,
		`k8s_namespace_name="data",ikhnos_cluster_id="c2",service_name=~"redis"`,
		`k8s_namespace_name="shop",ikhnos_cluster_id="c1",k8s_deployment_name=~"api|api-v2"`, // a pod's own series name its workload
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("no selector with %s:\n%s", want, all)
		}
	}
	for _, sel := range sels {
		if strings.Contains(sel, `"shop"`) && strings.Contains(sel, "redis") || strings.Contains(sel, `"data"`) && strings.Contains(sel, "api") {
			t.Fatalf("a member's name in another member's place: %s", sel)
		}
	}
	// A service the caller names itself is still only looked for where a member of that name runs.
	sels, _ = MetricFilter{Name: "up", Service: "redis"}.selectors(twoPlaceFocus(t))
	if len(sels) != 2 || strings.Contains(strings.Join(sels, ""), "k8s_deployment_name") {
		t.Fatalf("with a service named, one selector per place and no workload ones: %v", sels)
	}
}

func TestAnApplicationsLogsAndTracesAreEachMemberWhereItRuns(t *testing.T) {
	s := twoPlaceFocus(t)
	q, err := LogFilter{}.logQL(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q, `| (k8s_namespace_name="shop" and ikhnos_cluster_id="c1" and service_name=~"api|api-v2") or (k8s_namespace_name="data" and ikhnos_cluster_id="c2" and service_name=~"redis")`) {
		t.Fatalf("logql = %s", q)
	}
	tq, err := TraceFilter{}.traceQL(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tq, `((resource.k8s.namespace.name = "shop" && resource.ikhnos.cluster.id = "c1" && (resource.service.name = "api" || resource.service.name = "api-v2")) || (resource.k8s.namespace.name = "data" && resource.ikhnos.cluster.id = "c2" && (resource.service.name = "redis")))`) {
		t.Fatalf("traceql = %s", tq)
	}
}

// A member ikhnos knows no namespace for opens the namespace for its own names only, not for the whole application.
func TestAMemberWithNoNamespaceDoesNotOpenTheWholeApplication(t *testing.T) {
	g := &AppGroup{ID: "app", Name: "Shop", Members: []AppMember{{Name: "api", Namespace: "shop", Cluster: "c1"}, {Name: "worker"}}}
	s, err := AllSignals().FocusOn(g)
	if err != nil {
		t.Fatal(err)
	}
	sels, err := MetricFilter{Name: "up"}.selectors(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, sel := range sels {
		if strings.Contains(sel, "api") && !strings.Contains(sel, `k8s_namespace_name="shop"`) {
			t.Fatalf("api is read outside its namespace: %s", sel)
		}
	}
	found := false
	for _, sel := range sels {
		found = found || (strings.HasSuffix(sel, `,service_name=~"worker"}`) && !strings.Contains(sel, "k8s_namespace_name"))
	}
	if !found {
		t.Fatalf("worker, known nowhere in particular, is read by its name: %v", sels)
	}
}

func TestAnApplicationInTooManyPlacesKeepsTheUnionAlone(t *testing.T) {
	g := &AppGroup{ID: "app", Name: "Big"}
	for i := 0; i <= maxFocusPlaces; i++ {
		g.Members = append(g.Members, AppMember{Name: "s", Namespace: "ns" + itoa(int64(i)), Cluster: "c"})
	}
	s, err := AllSignals().FocusOn(g)
	if err != nil || s.Focus != nil || len(s.FocusNamespaces) != maxFocusPlaces+1 {
		t.Fatalf("err %v, focus %d places, namespaces %d", err, len(s.Focus), len(s.FocusNamespaces))
	}
}
