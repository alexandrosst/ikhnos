package agent_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"ikhnos/internal/agent"
	"ikhnos/internal/relay"
)

// mergePatch is RFC 7386, which is what the API server does with application/merge-patch+json: an object merges key by key, null removes the
// key, anything else replaces.
func mergePatch(target, patch map[string]any) map[string]any {
	if target == nil {
		target = map[string]any{}
	}
	for k, v := range patch {
		switch pv := v.(type) {
		case nil:
			delete(target, k)
		case map[string]any:
			cur, _ := target[k].(map[string]any)
			target[k] = mergePatch(cur, pv)
		default:
			target[k] = v
		}
	}
	return target
}

// A tiny stand-in for the status subresource: it keeps a document, merges patches into it as the API server does, and answers with it.
func statusServer(t *testing.T, doc map[string]any) (*httptest.Server, func() map[string]any) {
	t.Helper()
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != "/apis/ikhnos.io/v1alpha1/namespaces/ikhnos-system/ikhnosagents/this-cluster/status" {
			http.NotFound(w, r)
			return
		}
		if r.Method == http.MethodPatch {
			if ct := r.Header.Get("Content-Type"); ct != "application/merge-patch+json" {
				http.Error(w, "content type "+ct, http.StatusUnsupportedMediaType)
				return
			}
			b, _ := io.ReadAll(r.Body)
			var p map[string]any
			if err := json.Unmarshal(b, &p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mergePatch(doc, p)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)
	return srv, func() map[string]any {
		mu.Lock()
		defer mu.Unlock()
		b, _ := json.Marshal(doc)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		return out
	}
}

func relayStoreAt(t *testing.T, url string) agent.RelayStore {
	t.Helper()
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: url})
	if err != nil {
		t.Fatal(err)
	}
	return agent.ResourceRelayStore(kube, "ikhnos-system", "this-cluster")
}

func requestedIn(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	st, _ := doc["status"].(map[string]any)
	req, _ := st["requested"].(map[string]any)
	return req
}

// A request replaces the last one. A merge patch alone would keep what the new request no longer asks for: a signal turned off would stay
// on, and a share of the traces that was dropped would stay.
func TestAnotherRequestReplacesTheLastInTheStatusAndTakingItBackLeavesNothingAsked(t *testing.T) {
	doc := map[string]any{"status": map[string]any{"phase": "Ready", "relay": map[string]any{"revision": 1, "state": "applied"}}}
	srv, get := statusServer(t, doc)
	s := relayStoreAt(t, srv.URL)
	ctx := context.Background()

	ten := 10
	if err := s.Request(ctx, relay.Request{Revision: 1, Signals: &relay.Signals{Traces: true, Energy: true}, TracesSamplingPercentage: &ten}); err != nil {
		t.Fatal(err)
	}
	req := requestedIn(t, get())
	sig, _ := req["signals"].(map[string]any)
	if req["revision"] != float64(1) || sig["traces"] != true || sig["energy"] != true || req["tracesSamplingPercentage"] != float64(10) {
		t.Fatalf("the first request did not stay as asked: %v", req)
	}

	// Energy off, traces on, no share of the traces named.
	if err := s.Request(ctx, relay.Request{Revision: 2, Signals: &relay.Signals{Traces: true}}); err != nil {
		t.Fatal(err)
	}
	req = requestedIn(t, get())
	sig, _ = req["signals"].(map[string]any)
	if req["revision"] != float64(2) || sig["traces"] != true || sig["energy"] != false {
		t.Fatalf("a signal the second request turned off is still on: %v", req)
	}
	if len(sig) != 12 {
		t.Fatalf("every signal is written, the off ones too: %d of 12 in %v", len(sig), sig)
	}
	if _, kept := req["tracesSamplingPercentage"]; kept {
		t.Fatalf("a share of the traces the second request did not name is still there: %v", req)
	}

	// Taken back: only the revision is left.
	if err := s.Request(ctx, relay.Request{Revision: 3}); err != nil {
		t.Fatal(err)
	}
	req = requestedIn(t, get())
	if len(req) != 1 || req["revision"] != float64(3) {
		t.Fatalf("a request taken back left something asked: %v", req)
	}

	// Nothing else of the status was touched: the controller's word stays.
	if st := get()["status"].(map[string]any); st["phase"] != "Ready" || st["relay"] == nil {
		t.Fatalf("the agent wrote more than status.requested: %v", st)
	}
}

func TestWhatTheControllerSaidIsReadFromTheStatusAndNothingIsNil(t *testing.T) {
	doc := map[string]any{"status": map[string]any{"phase": "Ready"}}
	srv, _ := statusServer(t, doc)
	s := relayStoreAt(t, srv.URL)
	if res, err := s.Result(context.Background()); err != nil || res != nil {
		t.Fatalf("nothing said: %+v %v", res, err)
	}
	doc["status"].(map[string]any)["relay"] = map[string]any{"revision": 4, "state": "refused", "reason": "traces are not allowed here"}
	res, err := s.Result(context.Background())
	if err != nil || res == nil || res.Revision != 4 || res.State != relay.Refused || res.Reason != "traces are not allowed here" {
		t.Fatalf("%+v %v", res, err)
	}
}

// A definition of the resource from before status.requested drops it and answers success; that is told, since nothing would ever read it.
func TestARequestTheDefinitionOfTheResourceDroppedIsAnErrorWithTheWayOut(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"phase":"Ready"}}`))
	}))
	defer old.Close()
	err := relayStoreAt(t, old.URL).Request(context.Background(), relay.Request{Revision: 1, Signals: &relay.Signals{Traces: true}})
	if err == nil || !strings.Contains(err.Error(), "older than the controller's") {
		t.Fatalf("a request the API server dropped was taken for kept: %v", err)
	}
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "forbidden", http.StatusForbidden) }))
	defer deny.Close()
	if err := relayStoreAt(t, deny.URL).Request(context.Background(), relay.Request{Revision: 1}); err == nil {
		t.Fatal("a refused patch was not an error")
	}
	if _, err := relayStoreAt(t, deny.URL).Result(context.Background()); err == nil {
		t.Fatal("a refused read was not an error")
	}
}
