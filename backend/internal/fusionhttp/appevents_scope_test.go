package fusionhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ikhnos/internal/fusionapi"
)

// extrasWith answers Changes the way the real adapter does: only the events whose target is among the ids asked for.
type extrasWith struct {
	groups []fusionapi.AppGroup
	topo   *fusionapi.TopologyView
	evs    []fusionapi.ChangeEvent
}

func (f extrasWith) Topology(context.Context) (*fusionapi.TopologyView, error) { return f.topo, nil }
func (f extrasWith) Applications(context.Context) ([]fusionapi.AppGroup, error) {
	return f.groups, nil
}
func (f extrasWith) Changes(_ context.Context, _, _ time.Time, _, ids []string, _ int) ([]fusionapi.ChangeEvent, error) {
	var out []fusionapi.ChangeEvent
	for _, e := range f.evs {
		for _, id := range ids {
			if e.TargetID == id {
				out = append(out, e)
				break
			}
		}
	}
	return out, nil
}

// appTopologyBody asks for the topology of the application "Shop", which has a service in namespace a (cluster cl-1) and one in
// namespace secret (cluster cl-2), as a caller with scope.
func appTopologyBody(t *testing.T, scope fusionapi.Scope) (string, []fusionapi.ChangeEvent) {
	t.Helper()
	g := newGuard(fakeAccounts{}, fakeTokens{})
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return now }
	ex := extrasWith{
		groups: []fusionapi.AppGroup{{ID: "app-1", Name: "Shop", Members: []fusionapi.AppMember{
			{ID: "sv-visible", Name: "web", Namespace: "a", Cluster: "cl-1"},
			{ID: "sv-hidden-secret", Name: "payroll", Namespace: "secret", Cluster: "cl-2"}}}},
		topo: &fusionapi.TopologyView{At: now, Services: []fusionapi.TopoService{
			{ID: "sv-visible", Name: "web", Namespace: "a", Cluster: "cl-1", Replicas: 1, Ready: 1},
			{ID: "sv-hidden-secret", Name: "payroll", Namespace: "secret", Cluster: "cl-2", Replicas: 1, Ready: 1}}},
		evs: []fusionapi.ChangeEvent{
			{Time: now.Add(-time.Hour), Kind: "application-membership", TargetKind: "application", TargetID: "app-1", Name: "Shop",
				Detail: "1 service(s) added: sv-hidden-secret"},
			{Time: now.Add(-2 * time.Hour), Kind: "scaled", TargetKind: "service", TargetID: "sv-visible", Name: "web", Detail: "replicas 1 to 2"},
		},
	}
	g.Extras = func() fusionapi.Extras { return ex }
	r := httptest.NewRequest("GET", "/x", nil)
	r.SetPathValue("name", "app-1")
	w := httptest.NewRecorder()
	g.AppTopology(w, r, &fusionapi.Client{}, Caller{Kind: "token", ID: "t", Scope: scope})
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var v struct {
		Changes []fusionapi.ChangeEvent `json:"changes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return w.Body.String(), v.Changes
}

// The application's own events say which services it gained and lost, by id. A caller limited to a namespace or a cluster is not
// told about services it may not see, so it gets the events about what it can see and not the application's.
func TestAppTopologyEventsDoNotNameServicesALimitedCallerCannotSee(t *testing.T) {
	for name, scope := range map[string]fusionapi.Scope{
		"a namespace": {Signals: fusionapi.Signals, Namespaces: []string{"a"}},
		"a cluster":   {Signals: fusionapi.Signals, Clusters: []string{"cl-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			body, changes := appTopologyBody(t, scope)
			if strings.Contains(body, "sv-hidden-secret") || strings.Contains(body, "payroll") {
				t.Fatalf("a caller limited to %s was told of a service it cannot see: %v", name, changes)
			}
			if len(changes) != 1 || changes[0].TargetID != "sv-visible" {
				t.Fatalf("the event about the visible service is lost: %v", changes)
			}
		})
	}
}

func TestAppTopologyEventsOfAnUnrestrictedCallerIncludeTheApplicationsOwn(t *testing.T) {
	_, changes := appTopologyBody(t, fusionapi.Scope{Signals: fusionapi.Signals})
	if len(changes) != 2 {
		t.Fatalf("an unrestricted caller should see both events, got %v", changes)
	}
}

// everything answers Changes the way the store does when it is given no filter: with every event. A handler that passes it an empty
// list of targets because the caller may see none of the application's services would hand all of them over.
type everything struct {
	extrasWith
	asked [][]string
}

func (f *everything) Changes(ctx context.Context, since, until time.Time, clusters, ids []string, limit int) ([]fusionapi.ChangeEvent, error) {
	f.asked = append(f.asked, ids)
	if len(ids) == 0 {
		return f.evs, nil
	}
	return f.extrasWith.Changes(ctx, since, until, clusters, ids, limit)
}

func TestACallerWhoMaySeeNoServiceOfTheApplicationIsToldNoEvents(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	g.Now = func() time.Time { return now }
	// the application's visible member is declared but not in the live topology, so the topology holds nothing of it to ask about
	ex := &everything{extrasWith: extrasWith{
		groups: []fusionapi.AppGroup{{ID: "app-1", Name: "Shop", Members: []fusionapi.AppMember{
			{ID: "sv-declared", Name: "web", Namespace: "a", Cluster: "cl-1"}}}},
		topo: &fusionapi.TopologyView{At: now, Services: []fusionapi.TopoService{
			{ID: "sv-payroll", Name: "payroll", Namespace: "secret", Cluster: "cl-2", Replicas: 1, Ready: 1}}},
		evs: []fusionapi.ChangeEvent{{Time: now.Add(-time.Hour), Kind: "service-scaled", TargetKind: "service", TargetID: "sv-payroll",
			Name: "payroll", Cluster: "cl-2", ClusterName: "finance-prod", Detail: "replicas 1 to 9"}},
	}}
	g.Extras = func() fusionapi.Extras { return ex }
	r := httptest.NewRequest("GET", "/x", nil)
	r.SetPathValue("name", "app-1")
	w := httptest.NewRecorder()
	g.AppTopology(w, r, &fusionapi.Client{}, Caller{Kind: "token", ID: "t", Scope: fusionapi.Scope{Signals: fusionapi.Signals, Namespaces: []string{"a"}}})
	if body := w.Body.String(); strings.Contains(body, "payroll") || strings.Contains(body, "finance-prod") {
		t.Fatalf("a token for namespace a was told about namespace secret: %s", body)
	}
	for _, ids := range ex.asked {
		if len(ids) == 0 {
			t.Fatalf("the events were asked for with no targets, which a store reads as every target")
		}
	}
}
