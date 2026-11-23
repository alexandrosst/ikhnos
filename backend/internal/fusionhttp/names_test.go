package fusionhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"ikhnos/internal/fusionapi"
)

// extrasWithNames knows the names of the organisation and of the clusters cl-1 and cl-2; with fail set it cannot read them.
type extrasWithNames struct {
	extrasWith
	fail bool
}

func (e extrasWithNames) Names(context.Context) (fusionapi.Names, error) {
	if e.fail {
		return fusionapi.Names{}, errors.New("the state is not available")
	}
	return fusionapi.Names{Org: &fusionapi.OrgName{ID: "default", Name: "Edge lab"},
		Clusters: map[string]string{"cl-1": "edge-paris", "cl-2": "secret-dc"}}, nil
}

// shopAndHR are the applications of tempoWithOneTrace's two services.
var shopAndHR = []fusionapi.AppGroup{
	{ID: "app-1", Name: "Shop", Members: []fusionapi.AppMember{{Name: "web", Namespace: "a", Cluster: "cl-1"}}},
	{ID: "app-2", Name: "HR", Members: []fusionapi.AppMember{{Name: "payroll", Namespace: "secret", Cluster: "cl-1"}}},
}

func readTraceAs(t *testing.T, query string, scope fusionapi.Scope, ex fusionapi.Extras) *httptest.ResponseRecorder {
	t.Helper()
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Extras = func() fusionapi.Extras { return ex }
	r := httptest.NewRequest("GET", "/traces/x?"+query, nil)
	r.SetPathValue("id", "0af7651916cd43dd8448eb211c80319c")
	w := httptest.NewRecorder()
	g.Trace(w, r, tempoWithOneTrace(t), Caller{Kind: "token", ID: "t1", Scope: scope})
	return w
}

func TestAFusedReadAndAPlainOneNameTheirClustersBesideTheIds(t *testing.T) {
	ex := extrasWithNames{extrasWith: extrasWith{groups: shopAndHR}}
	for _, q := range []string{"", "include=none"} {
		w := readTraceAs(t, q, fusionapi.AllSignals(), ex)
		var f fusionapi.Fused
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || len(f.Resources) != 2 {
			t.Fatalf("%q: %d %s", q, w.Code, w.Body.String())
		}
		for _, r := range f.Resources {
			if r.Cluster != "cl-1" || r.ClusterName != "edge-paris" {
				t.Fatalf("%q: resource %+v", q, r)
			}
		}
		if q == "" {
			if f.Context != nil || f.JoinedBy != nil {
				t.Fatalf("a plain read is the trace alone: %s", w.Body.String())
			}
			continue
		}
		fc := f.Context
		wantClusters := []fusionapi.ClusterName{{ID: "cl-1", Name: "edge-paris"}}
		wantApps := []fusionapi.AppRef{{ID: "app-2", Name: "HR"}, {ID: "app-1", Name: "Shop"}}
		if fc == nil || fc.Org == nil || fc.Org.Name != "Edge lab" || !slices.Equal(fc.Clusters, wantClusters) ||
			!slices.Equal(fc.Applications, wantApps) {
			t.Fatalf("context = %+v", fc)
		}
	}
}

func TestAFusedReadOfALimitedCallerNamesOnlyTheApplicationsAndClustersItMaySee(t *testing.T) {
	ex := extrasWithNames{extrasWith: extrasWith{groups: shopAndHR}}
	limited := fusionapi.Scope{Signals: fusionapi.Signals, Namespaces: []string{"a"}, Clusters: []string{"cl-1"}}
	w := readTraceAs(t, "include=none", limited, ex)
	var f fusionapi.Fused
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if f.Context == nil || !slices.Equal(f.Context.Applications, []fusionapi.AppRef{{ID: "app-1", Name: "Shop"}}) {
		t.Fatalf("context = %+v", f.Context)
	}
	for _, hidden := range []string{"HR", "payroll", "secret-dc", "cl-2"} {
		if strings.Contains(w.Body.String(), hidden) {
			t.Fatalf("the answer names %s, which the caller may not see: %s", hidden, w.Body.String())
		}
	}
}

func TestNamesThatCannotBeReadFailNoReadAndAreOneWarningOfAFusedOne(t *testing.T) {
	ex := extrasWithNames{extrasWith: extrasWith{groups: shopAndHR}, fail: true}
	w := readTraceAs(t, "", fusionapi.AllSignals(), ex)
	if w.Code != 200 || strings.Contains(w.Body.String(), "clusterName") || strings.Contains(w.Body.String(), "warning") {
		t.Fatalf("plain: %d %s", w.Code, w.Body.String())
	}
	w = readTraceAs(t, "include=none", fusionapi.AllSignals(), ex)
	var f fusionapi.Fused
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || len(f.Warnings) != 1 ||
		!strings.Contains(f.Warnings[0], "names") {
		t.Fatalf("fused: %d %s", w.Code, w.Body.String())
	}
}

func TestTheShapeOfAFusedReadIsCheckedAndCompactLeavesWhereASpanRanToItsResource(t *testing.T) {
	ex := extrasWith{groups: shopAndHR}
	if w := readTraceAs(t, "include=none&shape=small", fusionapi.AllSignals(), ex); w.Code != http.StatusBadRequest ||
		!strings.Contains(w.Body.String(), "shape takes full or compact") {
		t.Fatalf("an unknown shape: %d %s", w.Code, w.Body.String())
	}
	if w := readTraceAs(t, "shape=compact", fusionapi.AllSignals(), ex); w.Code != http.StatusBadRequest {
		t.Fatalf("shape without a fused read: %d %s", w.Code, w.Body.String())
	}
	w := readTraceAs(t, "include=none&shape=compact", fusionapi.AllSignals(), ex)
	var doc struct {
		Spans []map[string]any `json:"spans"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &doc) != nil || len(doc.Spans) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, sp := range doc.Spans {
		if sp["service"] != nil || sp["namespace"] != nil || sp["cluster"] != nil || sp["resource"] == nil {
			t.Fatalf("span %v", sp)
		}
	}
}

// lokiAndProm answers every Loki read with one line from each of the clusters cl-1 and cl-2, and every Prometheus read with
// nothing. The requests it saw are recorded under a lock: reads run concurrently.
type lokiAndProm struct {
	mu   sync.Mutex
	seen []string
}

func (s *lokiAndProm) client(t *testing.T) *fusionapi.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, r.URL.Path)
		s.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/loki/") {
			line := func(cl, ts string) map[string]any {
				labels := map[string]string{"service_name": "web", "k8s_namespace_name": "a", "ikhnos_cluster_id": cl}
				return map[string]any{"stream": labels, "values": []any{[]any{ts, "hello"}}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "streams",
				"result": []any{line("cl-1", "1791200000000000000"), line("cl-2", "1791200001000000000")}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": []any{}})
	}))
	t.Cleanup(srv.Close)
	return &fusionapi.Client{Loki: srv.URL, Prometheus: srv.URL}
}

func TestTheLogListNamesTheClustersOfItsLinesOnceBesideThem(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Now = func() time.Time { return time.Unix(1791200100, 0) }
	g.Extras = func() fusionapi.Extras { return extrasWithNames{extrasWith: extrasWith{groups: shopAndHR}} }
	stores := &lokiAndProm{}
	c := stores.client(t)
	read := func(scope fusionapi.Scope) logsDoc {
		w := httptest.NewRecorder()
		g.Logs(w, httptest.NewRequest("GET", "/logs?service=web", nil), c, Caller{Kind: "token", ID: "t1", Scope: scope})
		var d logsDoc
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "clusterName") {
			t.Fatalf("a line carries a name: %s", w.Body.String())
		}
		return d
	}
	both := []fusionapi.ClusterName{{ID: "cl-1", Name: "edge-paris"}, {ID: "cl-2", Name: "secret-dc"}}
	if d := read(fusionapi.AllSignals()); !slices.Equal(d.Clusters, both) {
		t.Fatalf("clusters = %+v", d.Clusters)
	}
	limited := fusionapi.Scope{Signals: fusionapi.Signals, Clusters: []string{"cl-1"}}
	if d := read(limited); slices.ContainsFunc(d.Clusters, func(c fusionapi.ClusterName) bool { return c.ID == "cl-2" }) {
		t.Fatalf("a limited caller is told of cl-2: %+v", d.Clusters)
	}
}

func TestAReadNarrowedToAnApplicationInTooManyPlacesSaysItsNamesAreMatchedTogether(t *testing.T) {
	inPlaces := func(id string, n int) fusionapi.AppGroup {
		g := fusionapi.AppGroup{ID: id, Name: id}
		for i := 0; i < n; i++ {
			m := fusionapi.AppMember{Name: fmt.Sprintf("svc-%d", i), Namespace: fmt.Sprintf("ns-%d", i), Cluster: "cl-1"}
			g.Members = append(g.Members, m)
		}
		return g
	}
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Now = func() time.Time { return time.Unix(1791200100, 0) }
	g.Extras = func() fusionapi.Extras {
		return extrasWith{groups: []fusionapi.AppGroup{inPlaces("Big", 21), inPlaces("Small", 20)}}
	}
	c := (&lokiAndProm{}).client(t)
	who := Caller{Kind: "token", ID: "t1", Scope: fusionapi.AllSignals()}
	for _, rd := range []struct {
		path string
		h    Handler
	}{{"/logs", g.Logs}, {"/metrics/names", g.MetricNames}, {"/metrics/series", g.MetricSeries}} {
		for _, app := range []string{"Big", "Small", ""} {
			url := rd.path + "?application=" + app
			if app == "" {
				url = rd.path
			}
			w := httptest.NewRecorder()
			rd.h(w, httptest.NewRequest("GET", url, nil), c, who)
			var d struct {
				Warnings []string `json:"warnings"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &d) != nil {
				t.Fatalf("%s: %d %s", url, w.Code, w.Body.String())
			}
			said := len(d.Warnings) == 1 && strings.Contains(d.Warnings[0], "application Big runs in 21 places, more than the 20")
			if (app == "Big") != said || (app != "Big" && len(d.Warnings) > 0) {
				t.Fatalf("%s: warnings %q", url, d.Warnings)
			}
		}
	}
}
