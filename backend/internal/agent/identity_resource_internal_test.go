package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/relay"
)

type idWrites struct {
	mu   sync.Mutex
	ids  []string
	fail error
}

func (w *idWrites) report(_ context.Context, ident relay.Identity) error {
	id := ident.ClusterID + "/" + ident.OrgID
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fail != nil {
		return w.fail
	}
	w.ids = append(w.ids, id)
	return nil
}

func (w *idWrites) written() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.ids...)
}

func identityRunner(w *idWrites) *runner {
	r := &runner{log: slog.New(slog.NewTextHandler(io.Discard, nil)), ident: newIdentityState()}
	if w != nil {
		r.cfg = Config{ReportIdentity: w.report, IdentityRetry: 20 * time.Millisecond}
	}
	return r
}

// The cluster id the server names is written once, again only when it changes, and never while it is empty.
func TestTheClusterIdTheServerNamesIsWrittenOnceAndAgainOnlyWhenItChanges(t *testing.T) {
	w := &idWrites{}
	r := identityRunner(w)
	ctx := context.Background()

	r.noteIdentity(&ikhnosv1.Config{}) // (the server does not know the cluster yet)
	r.identityStep(ctx)
	if got := w.written(); len(got) != 0 {
		t.Fatalf("written before it was named: %v", got)
	}
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-1"})
	r.identityStep(ctx)
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-1"})
	r.noteIdentity(&ikhnosv1.Config{}) // an empty one leaves what is known
	r.identityStep(ctx)
	r.identityStep(ctx)
	if got := w.written(); strings.Join(got, ",") != "cl-1/" {
		t.Fatalf("written %v, want cl-1 once", got)
	}
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-2"})
	r.identityStep(ctx)
	if got := w.written(); strings.Join(got, ",") != "cl-1/,cl-2/" {
		t.Fatalf("written %v", got)
	}
}

// A write that failed is tried again later, not at every pass, and a new id does not wait for the old one's turn.
func TestAFailedWriteOfTheClusterIdIsTriedAgainLaterAndANewIdAtOnce(t *testing.T) {
	w := &idWrites{fail: errors.New("forbidden")}
	r := identityRunner(w)
	ctx := context.Background()
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-1"})
	r.identityStep(ctx)
	w.mu.Lock()
	w.fail = nil
	w.mu.Unlock()
	r.identityStep(ctx) // inside the wait after the failure
	if got := w.written(); len(got) != 0 {
		t.Fatalf("tried again inside the wait: %v", got)
	}
	time.Sleep(40 * time.Millisecond)
	r.identityStep(ctx)
	if got := w.written(); strings.Join(got, ",") != "cl-1/" {
		t.Fatalf("written %v", got)
	}
	// A failure and then a new id: the new one is not made to wait.
	w.mu.Lock()
	w.fail = errors.New("boom")
	w.mu.Unlock()
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-2"})
	r.identityStep(ctx)
	w.mu.Lock()
	w.fail = nil
	w.mu.Unlock()
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-3"})
	r.identityStep(ctx)
	if got := w.written(); strings.Join(got, ",") != "cl-1/,cl-3/" {
		t.Fatalf("written %v", got)
	}
}

// An agent that nothing keeps has nowhere to write, and says nothing.
func TestAnAgentNothingKeepsDoesNotTryToWriteTheClusterId(t *testing.T) {
	r := identityRunner(nil)
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-1"})
	r.identityStep(context.Background())
	if r.ident.want != (relay.Identity{}) {
		t.Errorf("remembered %q", r.ident.want)
	}
}

// The write is one merge patch of the status of the agent's own resource, checked against what the API server kept.
func TestTheClusterIdIsOnePatchOfTheStatusAndAnOlderDefinitionIsSaid(t *testing.T) {
	var mu sync.Mutex
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		method, path, body = r.Method, r.URL.Path, string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"identity":{"clusterId":"cl-a287110206"}}}`))
	}))
	defer srv.Close()
	kube, err := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := ReportResourceIdentity(kube, "ikhnos-system", "this-cluster")(context.Background(), relay.Identity{ClusterID: "cl-a287110206"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if method != "PATCH" || path != "/apis/ikhnos.io/v1alpha1/namespaces/ikhnos-system/ikhnosagents/this-cluster/status" || body != `{"status":{"identity":{"clusterId":"cl-a287110206"}}}` {
		t.Errorf("%s %s %s", method, path, body)
	}
	mu.Unlock()

	// An id that is not a plain word is not written at all.
	for _, bad := range []relay.Identity{{ClusterID: `a"b`}, {ClusterID: "cl-1", OrgID: "o r g"}, {}} {
		if err := ReportResourceIdentity(kube, "ikhnos-system", "this-cluster")(context.Background(), bad); err == nil {
			t.Errorf("%+v was written", bad)
		}
	}
	// A definition from before status.identity drops it and answers success.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":{"phase":"Ready"}}`))
	}))
	defer old.Close()
	kubeOld, _ := kubernetes.NewForConfig(&rest.Config{Host: old.URL})
	err = ReportResourceIdentity(kubeOld, "ikhnos-system", "this-cluster")(context.Background(), relay.Identity{ClusterID: "cl-1"})
	if err == nil || !strings.Contains(err.Error(), "older than the controller") {
		t.Errorf("a field the API server dropped was taken for success: %v", err)
	}
}

// The organisation is told along with the cluster, each when the server names it; naming only one leaves the other as it was.
func TestTheOrganisationIsToldWithTheClusterAndNeitherForgetsTheOther(t *testing.T) {
	w := &idWrites{}
	r := identityRunner(w)
	ctx := context.Background()
	r.noteIdentity(&ikhnosv1.Config{OrgId: "org-1"}) // an older server names no cluster; the organisation alone is still told
	r.identityStep(ctx)
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-1", OrgId: "org-1"})
	r.identityStep(ctx)
	r.noteIdentity(&ikhnosv1.Config{ClusterId: "cl-1"}) // (the organisation is not named in this one: it stays)
	r.identityStep(ctx)
	if got := strings.Join(w.written(), ","); got != "/org-1,cl-1/org-1" {
		t.Errorf("written %q", got)
	}
}

// The patch carries what is known and nothing else, and what the API server kept is checked for each.
func TestTheIdentityPatchCarriesBothIdsAndChecksBothWereKept(t *testing.T) {
	var mu sync.Mutex
	var body string
	answer := `{"status":{"identity":{"clusterId":"cl-1","orgId":"org-1"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		a := answer
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(a))
	}))
	defer srv.Close()
	kube, _ := kubernetes.NewForConfig(&rest.Config{Host: srv.URL})
	report := ReportResourceIdentity(kube, "ns", "res")
	if err := report(context.Background(), relay.Identity{ClusterID: "cl-1", OrgID: "org-1"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if body != `{"status":{"identity":{"clusterId":"cl-1","orgId":"org-1"}}}` {
		t.Errorf("body %s", body)
	}
	// A definition that knows clusterId but not orgId (it cannot: both came together) is still told, should it ever happen.
	answer = `{"status":{"identity":{"clusterId":"cl-1"}}}`
	mu.Unlock()
	if err := report(context.Background(), relay.Identity{ClusterID: "cl-1", OrgID: "org-1"}); err == nil || !strings.Contains(err.Error(), "older than the controller") {
		t.Errorf("a dropped organisation was taken for success: %v", err)
	}
}

func problemCoded(d *ikhnosv1.Diagnostics, code string) *ikhnosv1.Problem {
	for _, p := range d.Problems {
		if p.Code == code {
			return p
		}
	}
	return nil
}

// Telemetry that carries no cluster id while the server has named the cluster is a problem, but only once the server has named it for a while
// (the controller needs a few minutes to stamp it), only when the install sends telemetry, and never when the install already stamps the id.
func TestTelemetryWithoutTheClusterIdIsAProblemOnceTheGraceHasPassed(t *testing.T) {
	build := func(signals []string, stamped string, namedFor time.Duration, named string) *ikhnosv1.Problem {
		r := newTestRunner()
		r.ident = newIdentityState()
		r.cfg.TelemetrySignals = signals
		r.cfg.TelemetryClusterID = stamped
		if named != "" {
			r.noteClusterSeen(named)
			r.ident.seenSince = time.Now().Add(-namedFor)
		}
		return problemCoded(r.diagnostics(), CodeTelemetryNoClusterID)
	}
	sig := []string{"resource_usage"}

	p := build(sig, "", time.Hour, "cl-1")
	if p == nil || p.Severity != ikhnosv1.Problem_WARN || !strings.Contains(p.Message, "cl-1") || !strings.Contains(p.Message, "DefinitionCurrent") {
		t.Fatalf("an hour without the id: %+v", p)
	}
	if build(sig, "", time.Minute, "cl-1") != nil {
		t.Fatal("the problem is raised before the controller has had time to stamp the id")
	}
	if build(sig, "cl-1", time.Hour, "cl-1") != nil {
		t.Fatal("the problem is raised though the telemetry carries the id")
	}
	if build(nil, "", time.Hour, "cl-1") != nil {
		t.Fatal("the problem is raised though the install sends no telemetry")
	}
	if build(sig, "", time.Hour, "") != nil {
		t.Fatal("the problem is raised though the server has not named the cluster")
	}

	// the message does not change from one build to the next, so it stays one problem with one `since`
	r := newTestRunner()
	r.ident = newIdentityState()
	r.cfg.TelemetrySignals = sig
	r.noteClusterSeen("cl-1")
	r.ident.seenSince = time.Now().Add(-time.Hour)
	a, b := problemCoded(r.diagnostics(), CodeTelemetryNoClusterID), problemCoded(r.diagnostics(), CodeTelemetryNoClusterID)
	if a == nil || b == nil || a.Message != b.Message {
		t.Fatalf("the message changes between builds: %+v / %+v", a, b)
	}

	// the count starts again for a new id, and a repeat of the same id does not restart it
	r.noteClusterSeen("cl-1")
	if _, since := r.clusterSeen(); time.Since(since) < 59*time.Minute {
		t.Fatal("hearing the same id again restarted the count")
	}
	r.noteClusterSeen("cl-2")
	if id, since := r.clusterSeen(); id != "cl-2" || time.Since(since) > time.Minute {
		t.Fatalf("a new id: %q since %v", id, since)
	}

	// a runner that cannot keep the id (no state) does not fail
	var bare runner
	bare.noteClusterSeen("x")
	if id, _ := bare.clusterSeen(); id != "" {
		t.Fatalf("a bare runner knows %q", id)
	}
}
