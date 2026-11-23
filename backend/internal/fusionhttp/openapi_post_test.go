package fusionhttp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ikhnos/internal/fusionapi"
)

// A native route served as a form POST too is described as one, so a client generated from the description can send it.
func TestAFormPostIsDescribedBesideItsGet(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	spec := OpenAPI(g.Ops())
	b, _ := json.Marshal(spec)
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string         `json:"operationId"`
			FormOf      string         `json:"x-form-of"`
			RequestBody map[string]any `json:"requestBody"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	found := 0
	for path, methods := range doc.Paths {
		post, ok := methods["post"]
		if !ok || post.FormOf == "" {
			continue
		}
		found++
		if _, ok := methods["get"]; !ok || post.OperationID == methods["get"].OperationID || post.RequestBody == nil {
			t.Errorf("%s: post %+v beside get %+v", path, post, methods["get"])
		}
	}
	if found == 0 {
		t.Fatal("no form POST described")
	}
	if !strings.Contains(string(b), "600 reads a minute") {
		t.Fatal("the limits are not stated from the code's own numbers")
	}
}

// PromQL as written is answered in Prometheus' envelope, with what Prometheus said about it.
func TestAWrittenQueryKeepsPrometheusWarnings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[]},"infos":["PromQL info: metric might not be a counter"]}`))
	}))
	defer srv.Close()
	g := newGuard(fakeAccounts{}, fakeTokens{})
	w := httptest.NewRecorder()
	g.MetricRaw("query")(w, httptest.NewRequest("GET", "/metrics/query?query=rate(x[1m])", nil), &fusionapi.Client{Prometheus: srv.URL}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"infos":["PromQL info: metric might not be a counter"]`) || !strings.Contains(w.Body.String(), `"resultType":"vector"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
