package agent_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"

	"ikhnos/internal/agent"
	"ikhnos/internal/install"
)

// The mark is one merge patch of one annotation on the agent's own IkhnosAgent, and nothing else.
func TestMarkingTheResourceIsOnePatchOfOneAnnotationOfOneObject(t *testing.T) {
	var mu sync.Mutex
	var method, path, ctype, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		method, path, ctype, body = r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.MarkResourceRevoked(kube, "ikhnos-system", "this-cluster")(context.Background(), "revoked by an administrator"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if method != "PATCH" || path != "/apis/ikhnos.io/v1alpha1/namespaces/ikhnos-system/ikhnosagents/this-cluster" || ctype != "application/merge-patch+json" {
		t.Fatalf("%s %s (%s)", method, path, ctype)
	}
	if want := `{"metadata":{"annotations":{"` + install.RevokedAnnotation + `":"revoked by an administrator"}}}`; body != want {
		t.Fatalf("body %s\nwant %s", body, want)
	}

	// A refusal (the agent was not given the right) is an error to say, not a crash.
	deny := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "forbidden", http.StatusForbidden) }))
	defer deny.Close()
	kube2, _ := kubernetes.NewForConfig(&rest.Config{Host: deny.URL})
	if err := agent.MarkResourceRevoked(kube2, "ikhnos-system", "this-cluster")(context.Background(), "x"); err == nil {
		t.Fatal("a refused patch was not an error")
	}
}

// A revoked agent that restarts says so again until it has been heard, and then stops saying it; one that cannot say it still ends.
func TestARestartedRevokedAgentTellsTheControllerUntilItIsHeard(t *testing.T) {
	ids := agent.FileStore{Dir: filepath.Join(t.TempDir(), "id")}
	if err := ids.Save(context.Background(), &agent.Identity{AgentID: "ag-1", TokenID: "x", Revoked: "revoked by an administrator"}); err != nil {
		t.Fatal(err)
	}
	log, buf := newLog()
	var calls atomic.Int32
	var reasons []string
	var mu sync.Mutex
	cfg := agent.Config{Server: "127.0.0.1:1", CAPin: "00", Kube: fake.NewSimpleClientset(), Identity: ids, Log: log, RevokedHold: 200 * time.Millisecond,
		MarkRevoked: func(_ context.Context, reason string) error {
			mu.Lock()
			reasons = append(reasons, reason)
			mu.Unlock()
			if calls.Add(1) == 1 {
				return errors.New("forbidden")
			}
			return nil
		}}
	if err := agent.Run(context.Background(), cfg); !errors.Is(err, agent.ErrRevoked) {
		t.Fatalf("err = %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Fatalf("the controller was told %d times, want the failed one and the one that was heard", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if reasons[0] != "revoked by an administrator" {
		t.Errorf("reasons %v", reasons)
	}
	if out := buf.String(); !strings.Contains(out, "could not tell the controller") || !strings.Contains(out, "told the controller that this agent was revoked") {
		t.Fatalf("log:\n%s", out)
	}

	// Nobody keeps it: nothing is said and nothing breaks.
	cfg.MarkRevoked = nil
	if err := agent.Run(context.Background(), cfg); !errors.Is(err, agent.ErrRevoked) {
		t.Fatalf("err = %v", err)
	}
}

// The moment the server revokes a running agent, it tells the controller, once, and the reason is the server's.
func TestAnAgentRevokedWhileRunningTellsTheController(t *testing.T) {
	r := newERig(t, nil)
	ids := agent.FileStore{Dir: filepath.Join(t.TempDir(), "id")}
	log, _ := newLog()
	var reason atomic.Value
	var calls atomic.Int32
	cfg := r.cfg(ids, r.token("e2e"), log)
	cfg.MarkRevoked = func(_ context.Context, why string) error { calls.Add(1); reason.Store(why); return nil }
	_, done := r.start(cfg)
	code := codeOf(t, ids)
	waitFor(t, "pending agent", 10*time.Second, func() bool { return len(r.agents()) == 1 })
	id0 := r.agents()[0].ID
	_ = r.core.Agents.Approve(context.Background(), "test", id0, code, 2)
	waitFor(t, "first sync", 15*time.Second, r.synced)
	if err := r.core.Agents.Revoke(context.Background(), "test", id0, "cluster decommissioned"); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, done, "revocation"); !errors.Is(err, agent.ErrRevoked) {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("told %d times", calls.Load())
	}
	if why, _ := reason.Load().(string); !strings.Contains(why, "cluster decommissioned") {
		t.Fatalf("reason %q", why)
	}
}
