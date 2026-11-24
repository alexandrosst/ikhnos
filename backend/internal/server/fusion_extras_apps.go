package server

import (
	"context"
	"errors"
	"slices"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/landscape"
	"ikhnos/internal/model"
	"ikhnos/internal/workspace"
)

// appGroupTTL is how long the applications are reused: an application edited in ikhnos shows in the API within this.
const appGroupTTL = 10 * time.Second

// Applications are the ikhnos applications of the organisation, each with the services in it as the topology knows them.
// A failure is not kept; a result is, for appGroupTTL (sharedValue), and callers get their own copy of the slice header. A
// stale result is never given: which services an application has decides what a caller narrowed to it reads.
func (e fusionExtras) Applications(ctx context.Context) ([]fusionapi.AppGroup, error) {
	groups, err := e.a.appCache.get(ctx, e.a.C.Now, appGroupTTL, 0, e.applications)
	if err != nil {
		return nil, err
	}
	return slices.Clone(groups), nil
}

// fusionApplications are the applications of the organisation FUSION belongs to, read as the data API reads them (through the
// extras, and so through appCache): what a FUSION token limited to applications is checked against when it is made and
// resolved by at every request.
func (a *Admin) fusionApplications(ctx context.Context) ([]fusionapi.AppGroup, error) {
	ex := a.fusionExtras()
	if ex == nil {
		return nil, &fusionapi.Error{Status: 503, Msg: "this server does not know the applications"}
	}
	return ex.Applications(ctx)
}

func (e fusionExtras) applications(ctx context.Context) ([]fusionapi.AppGroup, error) {
	t, doc, err := e.state(ctx)
	if err != nil {
		return nil, err
	}
	ws, err := t.C.Store.GetWorkspace(ctx, t.C.OrgID)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return nil, &fusionapi.Error{Status: 503, Msg: "the saved applications are not available"}
	}
	groups, err := appGroups(ws.Data, doc)
	if err != nil {
		// (Not "no applications": a FUSION token limited to applications would read that as its applications being gone.)
		return nil, &fusionapi.Error{Status: 503, Msg: "the saved applications cannot be read"}
	}
	return groups, nil
}

// appGroups turns the applications of a saved workspace (workspace.Applications, the definition the graph uses too) into
// groups of services by the identity telemetry carries. A member is a topology service the workspace names by id; one the
// topology does not (or no longer) have is left out, since there is nothing to match telemetry with. The aliases are the
// app labels a service's telemetry often takes as its service.name instead of the workload name.
//
// A workspace that cannot be parsed is an error, not an organisation with no applications: what reads the applications must be
// able to tell the two apart (a token limited to applications is refused, not told they are gone).
func appGroups(data []byte, doc landscape.StateDoc) ([]fusionapi.AppGroup, error) {
	if len(data) == 0 {
		return nil, nil
	}
	d, err := workspace.Parse(data)
	if err != nil {
		return nil, err
	}
	byID := map[string]int{}
	for i, s := range doc.Topology.Services {
		byID[s.ID] = i
	}
	// A service a person wrote into ikhnos by hand is not in the interpreted topology (that is what the agents saw), but the
	// record itself says what it is called and where it runs, which is all telemetry can be matched with.
	declared := map[string]map[string]any{}
	for _, r := range d.Records["service"] {
		if _, gone := r["deletedAt"]; !gone {
			if id, _ := r["id"].(string); id != "" {
				declared[id] = r
			}
		}
	}
	str := func(r map[string]any, k string) string { v, _ := r[k].(string); return v }
	apps := workspace.Applications(d, landscape.HintsOf(doc))
	out := make([]fusionapi.AppGroup, 0, len(apps))
	for _, a := range apps {
		g := fusionapi.AppGroup{ID: a.ID, Name: a.Name, Description: a.Description, Origin: a.Origin, Confidence: a.Confidence, Members: []fusionapi.AppMember{}}
		if g.Name == "" {
			g.Name = a.ID
		}
		for _, id := range a.ServiceIDs {
			var m fusionapi.AppMember
			if i, ok := byID[id]; ok {
				s := doc.Topology.Services[i]
				m = fusionapi.AppMember{ID: id, Name: s.Name, Namespace: s.Namespace, Cluster: s.ClusterID, Kind: string(s.Kind)}
				for _, k := range []string{"app", "app.kubernetes.io/name"} {
					if v := s.Labels[k]; v != "" && v != s.Name && !slices.Contains(m.Aliases, v) {
						m.Aliases = append(m.Aliases, v)
					}
				}
			} else if r, ok := declared[id]; ok && str(r, "name") != "" {
				m = fusionapi.AppMember{ID: id, Name: str(r, "name"), Namespace: str(r, "namespace"), Cluster: str(r, "clusterId"), Kind: str(r, "kind")}
			} else {
				// A member ikhnos has an id for but cannot tie to a running service: a cluster that is not connected, or a
				// service that has gone. Counted, so a caller can be told why an application looks empty.
				g.Unresolved++
				continue
			}
			g.Members = append(g.Members, m)
		}
		out = append(out, g)
	}
	return out, nil
}

// annotateApplications names, on each service of the view, the applications it is in: those with a member of the
// service's key.
func annotateApplications(v *fusionapi.TopologyView, groups []fusionapi.AppGroup) {
	in := map[model.ServiceKey][]fusionapi.AppGroup{}
	for _, g := range groups {
		for _, m := range g.Members {
			if k := m.Key(); !slices.ContainsFunc(in[k], func(o fusionapi.AppGroup) bool { return o.ID == g.ID }) {
				in[k] = append(in[k], g)
			}
		}
	}
	for i := range v.Services {
		for _, g := range in[v.Services[i].Key()] {
			v.Services[i].Applications = append(v.Services[i].Applications, g.Name)
			v.Services[i].AppIDs = append(v.Services[i].AppIDs, g.ID)
		}
	}
}
