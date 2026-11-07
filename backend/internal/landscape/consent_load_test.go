package landscape

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"context"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/agents"
	"ikhnos/internal/store"
	"ikhnos/internal/testkit"
)

// flakyConsent fails the read of agents' overrides while down is set, as a database that is briefly locked would.
type flakyConsent struct {
	store.Store
	down atomic.Bool
}

func (f *flakyConsent) LoadSnapshot(ctx context.Context, key string) ([]byte, time.Time, error) {
	if f.down.Load() && strings.HasSuffix(key, agents.ConsentKey("")) {
		return nil, time.Time{}, errors.New("database is locked")
	}
	return f.Store.LoadSnapshot(ctx, key)
}

func excludedBatchSync() *ikhnosv1.Sync {
	return &ikhnosv1.Sync{Full: true, Cluster: &ikhnosv1.ClusterFacts{Uid: testkit.FP},
		Namespaces: []*ikhnosv1.NamespaceFacts{{Key: "ns/shop", Name: "shop"}, {Key: "ns/batch", Name: "batch"}},
		Workloads:  []*ikhnosv1.WorkloadFacts{{Key: "shop/Deployment/web", Namespace: "shop"}, {Key: "batch/Job/x", Namespace: "batch"}}}
}

// A store that cannot answer is not an agent with no overrides: before, the read failing once made the hub take the
// excluded namespace as allowed and keep believing so until it restarted.
func TestAnUnreadableStoreIsNotTakenForNoOverrides(t *testing.T) {
	r := newHubRig(t)
	id, _, _ := r.approvedAgent(t, testkit.FP)
	if _, err := r.hub.SetConsent(r.ctx, "actor", id, agents.Consent{Excluded: []string{"batch"}}); err != nil {
		t.Fatal(err)
	}
	a, _ := r.st.GetAgent(r.ctx, "org-1", id)
	flaky := &flakyConsent{Store: r.hub.C.Store}
	r.hub.C.Store = flaky
	t.Cleanup(func() { r.hub.C.Store = flaky.Store })

	// The hub has just started: it knows nothing of this agent's overrides yet, and the database is locked.
	r.hub.Mu.Lock()
	r.hub.Views[id] = NewView()
	r.hub.Mu.Unlock()
	flaky.down.Store(true)
	if _, _, err := r.hub.ApplySync(a, excludedBatchSync(), false); !errors.Is(err, errConsentUnavailable) {
		t.Fatalf("a picture was applied without knowing what is left out: %v", err)
	}
	r.hub.Mu.Lock()
	kept := len(r.hub.Views[id].State.Namespaces)
	r.hub.Mu.Unlock()
	if kept != 0 {
		t.Fatalf("%d namespaces were stored from a picture that was refused", kept)
	}

	// It comes back: the very next picture is filtered, because the failure was not remembered.
	flaky.down.Store(false)
	if _, _, err := r.hub.ApplySync(a, excludedBatchSync(), false); err != nil {
		t.Fatal(err)
	}
	st := r.hub.Views[id].State
	if _, ok := st.Namespaces["ns/batch"]; ok {
		t.Fatal("the excluded namespace got in after the store came back")
	}
	if _, ok := st.Namespaces["ns/shop"]; !ok {
		t.Fatal("the namespace that was not excluded is missing")
	}
}

// One entry that no longer passes (here a namespace that is a system one in this release) used to throw away every
// override the administrator had set for the agent.
func TestOneStaleStoredOverrideDoesNotDropTheOthers(t *testing.T) {
	r := newHubRig(t)
	id, _, _ := r.approvedAgent(t, testkit.FP)
	raw := `{"paused":["flow","retired-collector"],"excluded":["batch","kube-system"]}`
	if err := r.st.SaveSnapshot(r.ctx, agents.ConsentKey(id), []byte(raw), time.Now()); err != nil {
		t.Fatal(err)
	}
	r.hub.Mu.Lock()
	r.hub.Views[id] = NewView()
	r.hub.Mu.Unlock()
	got := r.hub.ConsentOf(id)
	if strings.Join(got.Paused, ",") != "flow" || strings.Join(got.Excluded, ",") != "batch" {
		t.Fatalf("the valid entries were not kept, or the invalid ones were: %+v", got)
	}
}
