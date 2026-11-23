package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/history"
	"ikhnos/internal/landscape"
	"ikhnos/internal/model"
	"ikhnos/internal/store"
)

// TopologyAt is the landscape as the graph remembered it at a moment, and the services that were in the application then: the topology
// of the newest recording at or before the moment, and the application's members as of the moment itself (so a service that
// joined after that recording is a member the topology does not have). Only a store with a graph
// database remembers membership, so without one this says history is not enabled.
func (e fusionExtras) TopologyAt(ctx context.Context, appID string, at time.Time) (*fusionapi.TopologyView, []fusionapi.AppMember, error) {
	t, err := e.tenant(ctx)
	if err != nil {
		return nil, nil, err
	}
	return topologyAt(ctx, t.C, appID, at)
}

func topologyAt(ctx context.Context, c *Core, appID string, at time.Time) (*fusionapi.TopologyView, []fusionapi.AppMember, error) {
	g, ok := c.Store.(landscape.GraphAPI)
	if !ok {
		return nil, nil, &fusionapi.Error{Status: 404, Msg: "history is not enabled on this server: it needs the graph database"}
	}
	p, data, err := c.Store.GetHistory(ctx, c.OrgID, at)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil, &fusionapi.Error{Status: 404, Msg: "nothing was recorded at or before that time"}
	}
	topo, derr := history.Decode(data)
	if err != nil || derr != nil {
		return nil, nil, &fusionapi.Error{Status: 503, Msg: "the recorded topology is not available"}
	}
	_, reached, err := g.Dependencies(ctx, c.OrgID, at, "application", appID, 1) // what it CONTAINS, at the very moment: it changes when a person saves, not when the landscape is recorded
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, nil, &fusionapi.Error{Status: 503, Msg: "the recorded applications are not available"}
	}
	members := []fusionapi.AppMember{}
	for _, r := range reached {
		var s model.Service
		if r.Kind == "service" && json.Unmarshal(r.Doc, &s) == nil {
			members = append(members, fusionapi.AppMember{ID: r.ID, Name: s.Name, Namespace: s.Namespace, Cluster: s.ClusterID, Kind: string(s.Kind)})
		}
	}
	return topologyView(landscape.StateDoc{GeneratedAt: p.At.UTC().Format(time.RFC3339), Topology: topo}), members, nil
}
