package server

import (
	"context"
	"errors"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/landscape"
	"ikhnos/internal/store"
)

// fusionExtras gives a fused read what ikhnos itself knows (fusionapi.Extras): the observed topology of the FUSION
// organisation and the events recorded about it. Both come from the server's own state, not from the three stores.
type fusionExtras struct{ a *Admin }

// extras is nil on a server with no platform behind it (it can then only be asked for what the stores hold).
func (a *Admin) fusionExtras() fusionapi.Extras {
	if a.extras != nil {
		return a.extras
	}
	if a.P == nil {
		return nil
	}
	return fusionExtras{a}
}

func (e fusionExtras) tenant(ctx context.Context) (*Tenant, error) {
	t, err := e.a.P.Tenant(ctx, e.a.fusionOrg())
	if err != nil || t == nil || t.Hub == nil {
		return nil, &fusionapi.Error{Status: 503, Msg: "the organisation's topology is not available"}
	}
	return t, nil
}

// state is the organisation's state document, which the topology, the applications and the clusters' names are made from.
func (e fusionExtras) state(ctx context.Context) (*Tenant, landscape.StateDoc, error) {
	t, err := e.tenant(ctx)
	if err != nil {
		return nil, landscape.StateDoc{}, err
	}
	doc, err := t.Hub.State(ctx)
	if err != nil {
		return nil, landscape.StateDoc{}, &fusionapi.Error{Status: 503, Msg: "the organisation's topology is not available"}
	}
	return t, doc, nil
}

// topologyTTL is how long the topology is reused. It is short: the topology is joined "as of now", and a few seconds is
// enough for the reads a dashboard or a script makes at once (each fused read, each list of services) to share one.
const topologyTTL = 3 * time.Second

// Topology is the organisation's topology, reused for topologyTTL (sharedValue) and never given stale. Every caller gets
// the same view: a view is only read once it is built (as one batch of traces already shares one, see FuseMany), and
// nothing that is given one changes it.
func (e fusionExtras) Topology(ctx context.Context) (*fusionapi.TopologyView, error) {
	return e.a.topoCache.get(ctx, e.a.C.Now, topologyTTL, 0, e.topology)
}

func (e fusionExtras) topology(ctx context.Context) (*fusionapi.TopologyView, error) {
	_, doc, err := e.state(ctx)
	if err != nil {
		return nil, err
	}
	v := topologyView(doc)
	// Say which ikhnos applications each service is in, from the applications as last worked out (reused for appGroupTTL,
	// so reading many traces does not read and parse the workspace for each). The topology is still worth returning when
	// they cannot be had, so a failure here leaves the services unannotated.
	groups, _ := e.Applications(ctx)
	annotateApplications(v, groups)
	return v, nil
}

// ClusterNames are the clusters of the organisation, by id and name, as the topology last had them.
func (e fusionExtras) ClusterNames(ctx context.Context) ([]fusionapi.ClusterName, error) {
	_, doc, err := e.state(ctx)
	if err != nil {
		return nil, err
	}
	var out []fusionapi.ClusterName
	for _, c := range doc.Topology.Clusters {
		out = append(out, fusionapi.ClusterName{ID: c.ID, Name: c.Name})
	}
	return out, nil
}

// OrgName is FUSION's organisation: its id, which the telemetry carries, and the name it was given in ikhnos.
func (e fusionExtras) OrgName(ctx context.Context) (fusionapi.OrgName, error) {
	t, err := e.tenant(ctx)
	if err != nil {
		return fusionapi.OrgName{}, err
	}
	o, err := t.C.Store.GetOrg(ctx, t.C.OrgID)
	if err != nil {
		return fusionapi.OrgName{}, &fusionapi.Error{Status: 503, Msg: "the organisation is not available"}
	}
	return fusionapi.OrgName{ID: o.ID, Name: o.Name}, nil
}

// namesStale is how long past appGroupTTL the names may still be given while they are read again: they are a help to a
// reader, not what decides what a caller may see, so a request should not wait on the state document for them.
const namesStale = time.Minute

// Names are the names of the organisation and its clusters (fusionapi.Namer), reused for appGroupTTL like the applications
// (sharedValue), so that a script reading traces many times a second does not build the state document and read the
// organisation each time: a cluster renamed in ikhnos shows in the API within that, or by the next request after it. The
// kept map is never changed, so it is shared, not copied. A read that cannot have the clusters' names fails; one that
// cannot have the organisation's gives the clusters' alone.
func (e fusionExtras) Names(ctx context.Context) (fusionapi.Names, error) {
	return e.a.nameCache.get(ctx, e.a.C.Now, appGroupTTL, namesStale, e.names)
}

func (e fusionExtras) names(ctx context.Context) (fusionapi.Names, error) {
	cs, err := e.ClusterNames(ctx)
	if err != nil {
		return fusionapi.Names{}, err
	}
	n := fusionapi.Names{Clusters: make(map[string]string, len(cs))}
	for _, cl := range cs {
		if cl.ID != "" && cl.Name != "" {
			n.Clusters[cl.ID] = cl.Name
		}
	}
	if o, err := e.OrgName(ctx); err == nil {
		n.Org = &o
	}
	return n, nil
}

func (e fusionExtras) Changes(ctx context.Context, since, until time.Time, clusters, targetIDs []string, limit int) ([]fusionapi.ChangeEvent, error) {
	// A nil list of targets asks for the events of every target; an empty one asks for none. The store reads an empty list as no filter,
	// so it must not be passed on: a caller who may see none of an application's services would be handed the whole organisation's events.
	if targetIDs != nil && len(targetIDs) == 0 {
		return []fusionapi.ChangeEvent{}, nil
	}
	if len(targetIDs) > 0 {
		// The targets already say what is asked about. A cluster filter on top would drop the events about an application (they carry
		// no cluster) and a dependency seen from the other side (they carry the cluster of the caller only).
		clusters = nil
	}
	t, err := e.tenant(ctx)
	if err != nil {
		return nil, err
	}
	// The clusters are part of the question, not a filter on its answer: with the limit applied first, other clusters' events
	// would fill the window and a token's own events would vanish from it.
	q := store.EventQuery{Since: since, Until: until, TargetIDs: targetIDs, ClusterIDs: clusters, Limit: limit}
	evs, err := t.C.Store.ListEvents(ctx, t.C.OrgID, q)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, &fusionapi.Error{Status: 503, Msg: "the recorded events are not available"}
	}
	want := map[string]bool{}
	for _, c := range clusters {
		want[c] = true
	}
	out := make([]fusionapi.ChangeEvent, 0, len(evs))
	for _, ev := range evs {
		if len(want) > 0 && !want[ev.ClusterID] {
			continue
		}
		out = append(out, fusionapi.ChangeEvent{Time: ev.At.UTC(), Kind: ev.Kind, TargetKind: string(ev.TargetKind), TargetID: ev.TargetID, Name: ev.Name,
			Cluster: ev.ClusterID, ClusterName: ev.ClusterName, Detail: ev.Detail, Cause: ev.Cause, Severity: string(ev.Severity)})
	}
	return out, nil
}
