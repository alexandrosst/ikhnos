package fusionhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ikhnos/internal/fusionapi"
)

func TestAMirrorRefusesThisAPIsOwnFilters(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	w := httptest.NewRecorder()
	g.native("prometheus", "/api/v1/query", nil)(w, httptest.NewRequest("GET", "/x?query=up&application=shop", nil), &fusionapi.Client{}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "structured routes") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestSystemTelemetryWithAnApplicationIsRefused(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	for _, q := range []string{"application=Shop&category=system", "application=Shop&name=kepler_container_joules_total"} {
		_, _, err := g.Scope(httptest.NewRequest("GET", "/x?"+q, nil), Caller{Scope: fusionapi.AllSignals()})
		if err == nil || !strings.Contains(err.Error(), "application") {
			t.Errorf("%s: %v", q, err)
		}
	}
}

func TestServicesOfAnApplicationRunWhereItsMembersDo(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Extras = func() fusionapi.Extras {
		return extrasWith{
			groups: []fusionapi.AppGroup{{ID: "app", Name: "Shop", Members: []fusionapi.AppMember{{Name: "web", Namespace: "shop", Cluster: "cl-1"}}}},
			topo: &fusionapi.TopologyView{Services: []fusionapi.TopoService{
				{ID: "sv-1", Name: "web", Namespace: "shop", Cluster: "cl-1"}, {ID: "sv-2", Name: "web", Namespace: "blog", Cluster: "cl-1"}}},
		}
	}
	places := g.servicePlaces(context.Background(), Caller{Scope: fusionapi.AllSignals()})
	grp := &fusionapi.AppGroup{Members: []fusionapi.AppMember{{Name: "web", Namespace: "shop", Cluster: "cl-1"}}}
	var kept []string
	for _, p := range places["web"] {
		if memberAt(grp, "web", p) {
			kept = append(kept, p.ID)
		}
	}
	if strings.Join(kept, ",") != "sv-1" {
		t.Fatalf("kept %v of %+v", kept, places["web"])
	}
}

func TestTheGraphFormTakesNoWindow(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	r := httptest.NewRequest("GET", "/x?format=nodelink&window=6h", nil)
	r.SetPathValue("name", "Shop")
	w := httptest.NewRecorder()
	g.AppTopology(w, r, &fusionapi.Client{}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "nodelink") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
