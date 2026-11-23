package fusionhttp

import (
	"context"
	"testing"

	"ikhnos/internal/fusionapi"
)

// /services says where ikhnos has services of each name, by their name or the app label telemetry uses, within what the caller
// may see.
func TestServicePlacesAreWhereIkhnosHasTheServiceAndTheCallerMaySee(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Extras = func() fusionapi.Extras {
		return extrasWith{topo: &fusionapi.TopologyView{Services: []fusionapi.TopoService{
			{ID: "sv-1", Name: "web", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment"},
			{ID: "sv-2", Name: "web-api", Namespace: "blog", Cluster: "cl-2", Kind: "Deployment", Labels: map[string]string{"app": "web"}},
			{ID: "sv-3", Name: "web", Namespace: "secret", Cluster: "cl-1"},
		}}}
	}
	all := g.servicePlaces(context.Background(), Caller{Scope: fusionapi.AllSignals()})
	if len(all["web"]) != 3 || all["web"][0].Cluster != "cl-1" || len(all["web-api"]) != 1 {
		t.Fatalf("places = %+v", all)
	}
	some := g.servicePlaces(context.Background(), Caller{Scope: fusionapi.Scope{Signals: fusionapi.Signals, Namespaces: []string{"shop", "blog"}}})
	for _, p := range some["web"] {
		if p.Namespace == "secret" {
			t.Fatalf("a place the caller may not see: %+v", some["web"])
		}
	}
	if len(some["web"]) != 2 {
		t.Fatalf("places = %+v", some["web"])
	}
}
