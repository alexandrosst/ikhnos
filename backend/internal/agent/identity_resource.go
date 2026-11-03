package agent

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/relay"
)

// Telling the controller who the cluster is. The server names every cluster it knows, and the organisation it belongs to, and says both to
// the agent in its Config (cluster_id, org_id). Telemetry has to carry them (ikhnos.cluster.id, ikhnos.org.id) for the stores and the
// dashboards to keep clusters apart, and a cluster installed with Helm gets them in the command the server writes. One kept by a controller is
// installed from a resource that does not have them, so the agent writes them under status.identity of that resource, and the controller
// stamps them on the telemetry unless the resource says them itself. It is one merge patch of the status subresource, the one thing besides
// status.revoked and status.requested the agent writes, and it needs no right to read the resource, so it works with the telemetry relay off.

// ClusterIdentity is what the agent tells the controller of the cluster (the relay package's Identity, which the controller reads), so that
// whatever starts the agent need not import the relay for it.
type ClusterIdentity = relay.Identity

// identityEvery is how often an identity that is not written yet is tried, and identityRetry how long to wait after a try that failed (a
// definition of the resource from before status.identity drops it without saying so; waiting stops that being told every few seconds).
const (
	identityEvery = 30 * time.Second
	identityRetry = 5 * time.Minute
)

// ReportResourceIdentity is Config.ReportIdentity for an agent that a controller keeps: it writes status.identity on the IkhnosAgent called
// name in namespace. The patch is checked against what the API server answers with, as MarkResourceRevoked's is: an older definition drops the
// field, and an agent that took that for success would never write it again. An id that is empty is left out of the patch, so what is
// there stays.
func ReportResourceIdentity(kube kubernetes.Interface, namespace, name string) func(ctx context.Context, id relay.Identity) error {
	return func(ctx context.Context, id relay.Identity) error {
		if (id.ClusterID != "" && !relay.ValidID(id.ClusterID)) || (id.OrgID != "" && !relay.ValidID(id.OrgID)) || id == (relay.Identity{}) {
			return fmt.Errorf("the identity %+v cannot be reported", id)
		}
		body, err := json.Marshal(map[string]any{"status": map[string]any{relay.StatusIdentity: id}})
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/apis/%s/namespaces/%s/ikhnosagents/%s/status", resourceAPIVersion, namespace, name)
		raw, err := kube.CoreV1().RESTClient().Patch(types.MergePatchType).AbsPath(path).Body(body).DoRaw(ctx)
		if err != nil {
			return fmt.Errorf("patching the status of the IkhnosAgent %s/%s: %w", namespace, name, err)
		}
		var got struct {
			Status struct {
				Identity relay.Identity `json:"identity"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &got) != nil || (id.ClusterID != "" && got.Status.Identity.ClusterID != id.ClusterID) || (id.OrgID != "" && got.Status.Identity.OrgID != id.OrgID) {
			return fmt.Errorf("the IkhnosAgent %s/%s did not keep status.identity: the definition of the resource in this cluster is older than the controller; apply the one in the controller's chart (kubectl apply -f crds/ikhnosagents.yaml)", namespace, name)
		}
		return nil
	}
}

// identityState is the identity the server last named, and the one last written.
type identityState struct {
	mu       sync.Mutex
	want     relay.Identity
	reported relay.Identity
	retryAt  time.Time
	wake     chan struct{}
}

func newIdentityState() *identityState { return &identityState{wake: make(chan struct{}, 1)} }

// noteIdentity takes the ids of a Config the agent applied. Nothing is done unless the agent has somewhere to write them and they are new; an
// id that is empty (the server does not know it yet) leaves what is known.
func (r *runner) noteIdentity(c *ikhnosv1.Config) {
	if r.cfg.ReportIdentity == nil || (c.GetClusterId() == "" && c.GetOrgId() == "") {
		return
	}
	st := r.ident
	st.mu.Lock()
	defer st.mu.Unlock()
	next := st.want
	if c.GetClusterId() != "" {
		next.ClusterID = c.GetClusterId()
	}
	if c.GetOrgId() != "" {
		next.OrgID = c.GetOrgId()
	}
	if next == st.want {
		return
	}
	st.want = next
	st.retryAt = time.Time{} // a new identity is written at once, whatever became of the last
	select {
	case st.wake <- struct{}{}:
	default:
	}
}

func (r *runner) identityLoop(ctx context.Context) {
	t := time.NewTicker(cmp.Or(r.cfg.IdentityEvery, identityEvery))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-r.ident.wake:
		}
		r.identityStep(ctx)
	}
}

// identityStep is one pass of identityLoop: it writes the identity if it is not written, and tries again after a failure only later.
func (r *runner) identityStep(ctx context.Context) {
	st := r.ident
	st.mu.Lock()
	want, reported, retryAt := st.want, st.reported, st.retryAt
	st.mu.Unlock()
	if want == (relay.Identity{}) || want == reported || r.cfg.ReportIdentity == nil || time.Now().Before(retryAt) {
		return
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := r.cfg.ReportIdentity(c, want)
	st.mu.Lock()
	defer st.mu.Unlock()
	if err != nil {
		if st.want == want { // (a newer identity is written at once, not after this one's wait)
			st.retryAt = time.Now().Add(cmp.Or(r.cfg.IdentityRetry, identityRetry))
		}
		r.log.Warn("could not tell the controller which cluster this is", "cluster", want.ClusterID, "org", want.OrgID, "err", err)
		return
	}
	st.reported = want
	r.log.Info("told the controller which cluster this is", "cluster", want.ClusterID, "org", want.OrgID)
}
