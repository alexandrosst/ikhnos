package fusionhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ikhnos/internal/fusionapi"
)

// tempoWithOneTrace answers every trace read with a trace of two services: web (namespace a) calls payroll (namespace secret).
func tempoWithOneTrace(t *testing.T) *fusionapi.Client {
	t.Helper()
	attr := func(k, v string) map[string]any {
		return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
	}
	rs := func(svc, ns, id, parent string) map[string]any {
		return map[string]any{
			"resource": map[string]any{"attributes": []any{attr("service.name", svc), attr("k8s.namespace.name", ns), attr("ikhnos.cluster.id", "cl-1")}},
			"scopeSpans": []any{map[string]any{"spans": []any{map[string]any{"traceId": "0af7651916cd43dd8448eb211c80319c", "spanId": id, "parentSpanId": parent,
				"name": "op", "startTimeUnixNano": "1791200000000000000", "endTimeUnixNano": "1791200000100000000"}}}},
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"trace": map[string]any{"resourceSpans": []any{
			rs("web", "a", "00f067aa0ba902b7", ""), rs("payroll", "secret", "00f067aa0ba902b8", "00f067aa0ba902b7")}}})
	}))
	t.Cleanup(srv.Close)
	return &fusionapi.Client{Tempo: srv.URL}
}

func readTrace(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Extras = func() fusionapi.Extras {
		return extrasWith{groups: []fusionapi.AppGroup{
			{ID: "app-1", Name: "Shop", Members: []fusionapi.AppMember{{Name: "web", Namespace: "a", Cluster: "cl-1"}}},
			{ID: "app-2", Name: "HR", Members: []fusionapi.AppMember{{Name: "payroll", Namespace: "secret", Cluster: "cl-1"}}},
		}}
	}
	r := httptest.NewRequest("GET", "/traces/x?"+query, nil)
	r.SetPathValue("id", "0af7651916cd43dd8448eb211c80319c")
	w := httptest.NewRecorder()
	g.Trace(w, r, tempoWithOneTrace(t), Caller{Kind: "admin", ID: "u1", Scope: fusionapi.AllSignals()})
	return w
}

func TestAFusedTraceNamesTheApplicationsOfItsResourcesAndCanBeNarrowedToOne(t *testing.T) {
	w := readTrace(t, "include=none")
	var f fusionapi.Fused
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	tags := map[string]string{}
	for _, r := range f.Resources {
		if len(r.Applications) == 1 {
			tags[r.Service] = r.Applications[0].Name
		}
	}
	if tags["web"] != "Shop" || tags["payroll"] != "HR" || f.Application != nil {
		t.Fatalf("tags = %v, application = %+v", tags, f.Application)
	}

	w = readTrace(t, "include=none&application=shop") // a name, any case
	f = fusionapi.Fused{}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &f) != nil || f.Application == nil || f.Application.ID != "app-1" || len(f.Spans) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestApplicationOnOneTraceIsRefusedWhereItWouldDoNothingAndWhereItIsUnknown(t *testing.T) {
	if w := readTrace(t, "application=Shop"); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "fused") {
		t.Fatalf("without fused: %d %s", w.Code, w.Body.String())
	}
	if w := readTrace(t, "fused=true&application=Nope"); w.Code != http.StatusNotFound {
		t.Fatalf("an unknown application: %d %s", w.Code, w.Body.String())
	}
}
