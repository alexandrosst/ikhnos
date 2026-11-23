package fusionapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
)

// twoClusterTrace is tempoTrace with its audit span moved to namespace shop of a second cluster, cl-2: a limited caller
// (limited, of cl-1) may see its namespace but not its cluster.
func twoClusterTrace() map[string]any {
	tr := tempoTrace()
	rs := tr["trace"].(map[string]any)["resourceSpans"].([]any)
	attrs := []any{kv("service.name", "audit"), kv("k8s.namespace.name", "shop"), kv("ikhnos.cluster.id", "cl-2")}
	rs[2].(map[string]any)["resource"] = map[string]any{"attributes": attrs}
	return tr
}

var testNames = &Names{Org: &OrgName{ID: "default", Name: "Edge lab"},
	Clusters: map[string]string{"cl-1": "edge-paris", "cl-2": "secret-dc"}}

func TestAFusedReadNamesItsClustersTheOrganisationAndTheApplicationsBesideTheirIds(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, twoClusterTrace()) }
	groups := []AppGroup{
		{ID: "app-2", Name: "Shop", Members: []AppMember{{Name: "cart", Namespace: "shop", Cluster: "cl-1"}}},
		{ID: "app-1", Name: "Billing", Members: []AppMember{{Name: "billing", Namespace: "pay", Cluster: "cl-1"}}},
	}
	opts := FuseOptions{Applications: groups, Names: testNames}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, opts)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, r := range got.Resources {
		names[r.Service] = r.ClusterName
	}
	if names["cart"] != "edge-paris" || names["billing"] != "edge-paris" || names["audit"] != "secret-dc" {
		t.Fatalf("cluster names = %v", names)
	}
	fc := got.Context
	if fc == nil || fc.Org == nil || fc.Org.Name != "Edge lab" || fc.Org.ID != "default" {
		t.Fatalf("context = %+v", fc)
	}
	if want := []ClusterName{{ID: "cl-1", Name: "edge-paris"}, {ID: "cl-2", Name: "secret-dc"}}; !slices.Equal(fc.Clusters, want) {
		t.Fatalf("clusters = %+v", fc.Clusters)
	}
	if want := []AppRef{{ID: "app-1", Name: "Billing"}, {ID: "app-2", Name: "Shop"}}; !slices.Equal(fc.Applications, want) {
		t.Fatalf("applications = %+v", fc.Applications)
	}
	if len(got.Warnings) != 0 {
		t.Fatalf("warnings = %v", got.Warnings)
	}
}

func TestALimitedCallerIsNeverToldTheNameOfAClusterOutsideItsRights(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, twoClusterTrace()) }
	got, err := f.client().FuseTrace(context.Background(), limited, traceHex, FuseOptions{Names: testNames})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	if strings.Contains(string(b), "secret-dc") || strings.Contains(string(b), "cl-2") {
		t.Fatalf("the answer names a cluster the caller may not see: %s", b)
	}
	if got.Context == nil || !slices.Equal(got.Context.Clusters, []ClusterName{{ID: "cl-1", Name: "edge-paris"}}) {
		t.Fatalf("context = %+v", got.Context)
	}
	// The second line of defence: a resource of another cluster that reached the trace would still not be named.
	tr := &Trace{Resources: []*Resource{{Key: "r1", Cluster: "cl-2"}, {Key: "r2", Cluster: "cl-1"}}}
	tr.NameClusters(limited, testNames)
	if tr.Resources[0].ClusterName != "" || tr.Resources[1].ClusterName != "edge-paris" {
		t.Fatalf("resources = %+v %+v", tr.Resources[0], tr.Resources[1])
	}
	lines := []LogEntry{{Cluster: "cl-1"}, {Cluster: "cl-2"}, {Cluster: "cl-1"}, {}}
	if got := ClustersOf(limited, lines, testNames); !slices.Equal(got, []ClusterName{{ID: "cl-1", Name: "edge-paris"}}) {
		t.Fatalf("dictionary = %+v", got)
	}
}

func TestNamesThatCannotBeReadAreOneWarningOfAFusedReadAndNothingElse(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{Names: &Names{Unavailable: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Warnings, []string{namesUnavailable}) {
		t.Fatalf("warnings = %v", got.Warnings)
	}
	for _, r := range got.Resources {
		if r.ClusterName != "" {
			t.Fatalf("resource %+v", r)
		}
	}
	// The clusters are still listed by id: what the trace ran on does not depend on knowing their names.
	if got.Context == nil || !slices.Equal(got.Context.Clusters, []ClusterName{{ID: "cl-1"}}) || got.Context.Org != nil {
		t.Fatalf("context = %+v", got.Context)
	}
	if d := ClustersOf(AllSignals(), []LogEntry{{Cluster: "cl-1"}}, &Names{Unavailable: true}); d != nil {
		t.Fatalf("a dictionary without names: %+v", d)
	}
	// A server that knows no names gives none, and says nothing about it.
	got, _ = f.client().FuseTrace(context.Background(), AllSignals(), traceHex, FuseOptions{})
	if len(got.Warnings) != 0 || got.Context == nil || got.Context.Org != nil {
		t.Fatalf("warnings %v, context %+v", got.Warnings, got.Context)
	}
}

func TestJoinedBySaysForEverySourceInJoinsHowItWasJoinedInAWordOfTheList(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	c := f.client()
	includes := []string{IncludeLogs, IncludeContextLogs, IncludeSystemLogs, IncludeMetrics, IncludeTopology, IncludeChanges}
	promql := [][]string{nil, {`a=up{service_name="${service}"}`}, {"b=up"}, {`a=up{service_name="${service}"}`, "b=up"}}
	want := map[string]string{SignalLogs: JoinTraceID, SourceContextLogs: JoinResourceWindow, SourceSystemLogs: JoinNodeWindow,
		SignalMetrics: JoinResourceWindow, SourceTopology: JoinTopology, SourceChanges: JoinChangesWindow}
	wantPromQL := []string{"", JoinResourceWindow, JoinWindow, JoinResourceOrWindow}
	for mask := 0; mask < 1<<len(includes); mask++ {
		for pi, qs := range promql {
			var in []string
			for i, n := range includes {
				if mask&(1<<i) != 0 {
					in = append(in, n)
				}
			}
			if len(in) == 0 {
				in = []string{IncludeNone}
			}
			q := url.Values{ParamInclude: {strings.Join(in, ",")}, ParamPromQL: qs}
			opts, err := ParseFuseOptions(q)
			if err != nil {
				t.Fatalf("%v: %v", q, err)
			}
			got, err := c.FuseTrace(context.Background(), AllSignals(), traceHex, opts)
			if err != nil {
				t.Fatalf("%v: %v", q, err)
			}
			joins, by := keysOf(got.Joins), keysOf(got.JoinedBy)
			if !slices.Equal(joins, by) {
				t.Fatalf("%v: joins %v, joinedBy %v", q, joins, by)
			}
			for k, v := range got.JoinedBy {
				if !slices.Contains(JoinWords, v) {
					t.Fatalf("%v: %s is joined by %q, which is not a word of the list", q, k, v)
				}
				if w, ok := want[k]; ok && v != w {
					t.Fatalf("%v: %s is joined by %q, want %q", q, k, v, w)
				}
			}
			if pi > 0 && got.JoinedBy[SourcePromQL] != wantPromQL[pi] {
				t.Fatalf("%v: promql is joined by %q, want %q", q, got.JoinedBy[SourcePromQL], wantPromQL[pi])
			}
		}
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestTheCompactShapeGivesWhereASpanRanOnceOnItsResource(t *testing.T) {
	f := newFake(t)
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, tempoTrace()) }
	opts, err := ParseFuseOptions(url.Values{ParamInclude: {IncludeNone}, ParamShape: {ShapeCompact}})
	if err != nil || !opts.Compact {
		t.Fatalf("%+v %v", opts, err)
	}
	got, err := f.client().FuseTrace(context.Background(), AllSignals(), traceHex, opts)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(got)
	var doc struct {
		Spans     []map[string]any `json:"spans"`
		Resources []map[string]any `json:"resources"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, r := range doc.Resources {
		keys[fmt.Sprint(r["key"])] = r["service"] != nil && r["cluster"] != nil
	}
	for _, sp := range doc.Spans {
		for _, k := range []string{"service", "namespace", "pod", "node", "cluster"} {
			if _, ok := sp[k]; ok {
				t.Fatalf("a compact span carries %s: %v", k, sp)
			}
		}
		if !keys[fmt.Sprint(sp["resource"])] {
			t.Fatalf("the span's resource %v does not say where it ran: %v", sp["resource"], doc.Resources)
		}
	}
	// The default is the full shape, as before.
	opts, _ = ParseFuseOptions(url.Values{ParamInclude: {IncludeNone}})
	got, _ = f.client().FuseTrace(context.Background(), AllSignals(), traceHex, opts)
	if opts.Compact || got.Spans[0].Service == "" || got.Spans[0].Cluster == "" {
		t.Fatalf("the full shape: %+v", got.Spans[0])
	}
	for _, v := range []string{"tiny", "COMPACT", "full,compact"} {
		if _, err := ParseFuseOptions(url.Values{ParamShape: {v}}); err == nil || !strings.Contains(err.Error(), "shape") {
			t.Fatalf("shape=%s: %v", v, err)
		}
	}
	if o, err := ParseFuseOptions(url.Values{ParamShape: {ShapeFull}}); err != nil || o.Compact {
		t.Fatalf("shape=full: %+v %v", o, err)
	}
}

func TestAnApplicationInMorePlacesThanAQueryCanNameIsSaidToBeMatchedByTheUnion(t *testing.T) {
	inPlaces := func(n int) *AppGroup {
		g := &AppGroup{ID: "big", Name: "Big"}
		for i := 0; i < n; i++ {
			m := AppMember{Name: fmt.Sprintf("svc-%d", i), Namespace: fmt.Sprintf("ns-%d", i), Cluster: "cl-1"}
			g.Members = append(g.Members, m)
		}
		return g
	}
	if note := inPlaces(maxFocusPlaces).UnionFocusNote(); note != "" {
		t.Fatalf("at the bound: %q", note)
	}
	s, err := AllSignals().FocusOn(inPlaces(maxFocusPlaces))
	if err != nil || len(s.Focus) == 0 {
		t.Fatalf("at the bound the focus is exact: %+v %v", s, err)
	}
	g := inPlaces(maxFocusPlaces + 1)
	note := g.UnionFocusNote()
	if !strings.Contains(note, "application Big runs in 21 places, more than the 20") || !strings.Contains(note, "together") {
		t.Fatalf("past the bound: %q", note)
	}
	if s, err := AllSignals().FocusOn(g); err != nil || len(s.Focus) != 0 {
		t.Fatalf("past the bound the union applies, as the note says: %+v %v", s, err)
	}
}

// The list of applications says so too, of the application it matched by the union.
func TestTheApplicationsListSaysWhichApplicationIsMatchedByTheUnion(t *testing.T) {
	big := AppGroup{ID: "big", Name: "Big"}
	for i := 0; i <= maxFocusPlaces; i++ {
		big.Members = append(big.Members, AppMember{Name: fmt.Sprintf("svc-%d", i), Namespace: fmt.Sprintf("ns-%d", i),
			Cluster: "cl-1"})
	}
	small := AppGroup{ID: "small", Name: "Small", Members: []AppMember{{Name: "api", Namespace: "web", Cluster: "cl-1"}}}
	f := newFake(t)
	f.prom = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"status": "success", "data": []string{}}) }
	f.loki = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"status": "success", "data": []string{}}) }
	f.tempo = func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"traces": []any{}}) }
	_, _, warnings, err := f.client().GroupSignals(context.Background(), AllSignals(), []AppGroup{big, small}, rangeAll)
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(warnings, "\n")
	if !strings.Contains(all, "application Big runs in 21 places") || strings.Contains(all, "application Small") {
		t.Fatalf("warnings: %v", warnings)
	}
}
