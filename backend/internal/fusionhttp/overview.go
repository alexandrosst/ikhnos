package fusionhttp

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"time"

	"ikhnos/internal/fusion"
	"ikhnos/internal/fusionapi"
	"ikhnos/internal/httpkit"
	"ikhnos/internal/kernel"
)

func (g *Guard) Status(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller) {
	access := callerAccessDoc{Kind: who.Kind, Name: who.Name, Signals: fusionapi.SignalsOf(who.Scope.Signals),
		Namespaces: orEmpty(who.Scope.Namespaces), Clusters: orEmpty(who.Scope.Clusters), RawQueries: who.Scope.Unrestricted(), PromQL: who.Scope.Allows(fusionapi.SignalMetrics)}
	if who.ExpiresAt != nil {
		access.ExpiresAt = kernel.RFC(*who.ExpiresAt)
	}
	// A token holder learns whether the stores are up, not how the cluster is set up: the full status carries workload
	// names, the namespace and Kubernetes API error text, which are for administrators. Nor does it read the cluster for
	// them: a token can poll as fast as the rate limit lets it, so it is answered from the last read (TokenStatus).
	fc := g.Control()
	var st fusion.FusionStatus
	if who.Kind == "token" {
		t := fc.TokenStatus(r.Context())
		st = fusion.FusionStatus{Available: t.Available, State: t.State}
	} else {
		st = fc.Status(r.Context())
	}
	doc := statusDoc{Fusion: st, Access: access}
	if on, ok := g.Extras().(fusionapi.OrgNamer); ok {
		if o, err := on.OrgName(r.Context()); err == nil {
			doc.Organisation = &o
		}
	}
	httpkit.WriteJSON(w, 200, doc)
}

// Applications lists the ikhnos applications, each with its services and which signals FUSION has for them in the range.
// An application here is what ikhnos calls one (a group of services); the services themselves, as the telemetry names them, are
// listed by fusionServices.
func (g *Guard) Applications(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller) {
	tr, err := Range(r, g.Now())
	if err != nil {
		g.Err(w, err)
		return
	}
	groups, err := g.Groups(r, who)
	if err != nil {
		g.Err(w, err)
		return
	}
	// Each application's signals are those of its members where they run, read application by application; past
	// MaxGroupSignals applications, by service name across everything the caller may see, and the answer says so. Either
	// way the request is charged the store reads it makes.
	if !g.chargeReads(w, who, fusionapi.GroupSignalsReads(who.Scope, len(groups))) {
		return
	}
	if len(groups) <= fusionapi.MaxGroupSignals {
		perGroup, sources, warnings, err := c.GroupSignals(r.Context(), who.Scope, groups, tr)
		if err != nil {
			g.Err(w, err)
			return
		}
		httpkit.WriteJSON(w, 200, applicationsDoc{Applications: fusionapi.DescribeGroupsIn(groups, perGroup), Sources: sources, Warnings: orEmpty(warnings), From: tr.From, To: tr.To})
		return
	}
	svcs, sources, warnings, err := c.Applications(r.Context(), who.Scope, tr)
	if err != nil {
		g.Err(w, err)
		return
	}
	warnings = append(warnings, fmt.Sprintf("there are more than %d applications, so a member's signals are those of any service of its name, wherever it runs; /applications/{name} reads one application where its members run", fusionapi.MaxGroupSignals))
	httpkit.WriteJSON(w, 200, applicationsDoc{Applications: fusionapi.DescribeGroups(groups, svcs), Sources: sources, Warnings: orEmpty(warnings), From: tr.From, To: tr.To})
}

// Services lists the services that have telemetry (their service.name), each with its signals and the ikhnos
// applications it is in. These are what the telemetry itself calls an application, which is not what ikhnos does.
func (g *Guard) Services(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller) {
	tr, err := Range(r, g.Now())
	if err != nil {
		g.Err(w, err)
		return
	}
	scope, notes, err := g.Scope(r, who)
	if err != nil {
		g.Err(w, err)
		return
	}
	svcs, sources, warnings, err := c.Applications(r.Context(), scope, tr)
	if err != nil {
		g.Err(w, err)
		return
	}
	warnings = append(notes, warnings...)
	in := map[string][]string{}
	if groups, err := g.Groups(r, who); err == nil { // best effort: the list is useful without it
		for _, grp := range groups {
			for _, n := range grp.ServiceNames() {
				in[n] = append(in[n], grp.Name)
			}
		}
	}
	places := g.servicePlaces(r.Context(), who)
	if q := r.URL.Query(); q.Has("application") {
		// Narrowed to an application, a service runs where the application's members of that name do, not everywhere it runs.
		if groups, err := g.Groups(r, who); err == nil {
			if grp, err := fusionapi.FindGroup(groups, q.Get("application")); err == nil {
				for name, ps := range places {
					places[name] = slices.DeleteFunc(ps, func(p servicePlaceDoc) bool { return !memberAt(grp, name, p) })
				}
			}
		}
	}
	out := make([]serviceDoc, 0, len(svcs))
	for _, sv := range svcs {
		names := in[sv.Name]
		if names == nil {
			names = []string{}
		}
		runs := places[sv.Name]
		if runs == nil {
			runs = []servicePlaceDoc{}
		}
		out = append(out, serviceDoc{sv, names, runs})
	}
	httpkit.WriteJSON(w, 200, servicesDoc{Services: out, Sources: sources, Warnings: orEmpty(warnings), From: tr.From, To: tr.To})
}

// memberAt says whether a member of the application has this name (or alias) at this place.
func memberAt(g *fusionapi.AppGroup, name string, p servicePlaceDoc) bool {
	for _, m := range g.Members {
		if (m.Name == name || slices.Contains(m.Aliases, name)) && (m.Namespace == "" || m.Namespace == p.Namespace) && (m.Cluster == "" || m.Cluster == p.Cluster) {
			return true
		}
	}
	return false
}

// servicePlaces is, for each name telemetry may carry as service.name (a service's own, or its app label), where ikhnos'
// topology has services of that name now, within what the caller may see. Best effort: without the topology it is empty.
func (g *Guard) servicePlaces(ctx context.Context, who Caller) map[string][]servicePlaceDoc {
	out := map[string][]servicePlaceDoc{}
	ex := g.Extras()
	if ex == nil {
		return out
	}
	view, err := ex.Topology(ctx)
	if err != nil || view == nil {
		return out
	}
	for _, sv := range view.Services {
		if !who.Scope.NamespaceVisible(sv.Namespace) || !who.Scope.ClusterVisible(sv.Cluster) {
			continue
		}
		p := servicePlaceDoc{ID: sv.ID, Namespace: sv.Namespace, Cluster: sv.Cluster, Kind: sv.Kind}
		seen := map[string]bool{}
		for _, n := range []string{sv.Name, sv.Labels["app"], sv.Labels["app.kubernetes.io/name"]} {
			if n != "" && !seen[n] {
				seen[n] = true
				out[n] = append(out[n], p)
			}
		}
	}
	for _, ps := range out {
		sort.Slice(ps, func(i, j int) bool {
			if ps[i].Cluster != ps[j].Cluster {
				return ps[i].Cluster < ps[j].Cluster
			}
			return ps[i].Namespace < ps[j].Namespace
		})
	}
	return out
}

// Application is one ikhnos application at a glance, named by its id or its name.
func (g *Guard) Application(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller) {
	tr, err := Range(r, g.Now())
	if err != nil {
		g.Err(w, err)
		return
	}
	groups, err := g.Groups(r, who)
	if err != nil {
		g.Err(w, err)
		return
	}
	app, err := fusionapi.FindGroup(groups, r.PathValue("name"))
	if err != nil {
		g.Err(w, err)
		return
	}
	if !g.chargeReads(w, who, fusionapi.ApplicationOverviewReads(who.Scope)) {
		return
	}
	o, err := c.ApplicationOverview(r.Context(), who.Scope, app, tr)
	if err != nil {
		g.Err(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, o)
}

// topologyAter is what the extras add when the server remembers the past (the server's TopologyAt).
type topologyAter interface {
	TopologyAt(ctx context.Context, appID string, at time.Time) (*fusionapi.TopologyView, []fusionapi.AppMember, error)
}

// AppTopology is one application's services and the traffic between them and the rest of the landscape, now or (at=) as of a
// past moment, with what changed around them in the window before it.
func (g *Guard) AppTopology(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller) {
	q := r.URL.Query()
	window, err := fusionapi.DurationParam(q.Get("window"))
	switch {
	case err != nil:
	case q.Has("window") && window <= 0: // (0 would read as the default, which is not what was asked)
		err = &fusionapi.Error{Status: http.StatusBadRequest, Msg: "window must be longer than 0; leave it out for the default"}
	case window > fusionapi.MaxTopologyWindow:
		err = &fusionapi.Error{Status: http.StatusBadRequest, Msg: "window can be at most 7d"}
	case window == 0:
		window = fusionapi.DefaultTopologyWindow
	}
	var noise bool
	if err == nil {
		noise, err = fusionapi.ParseBool("noise", q.Get("noise"), false)
	}
	var placement bool
	if err == nil {
		placement, err = fusionapi.ParseBool("placement", q.Get("placement"), false)
	}
	var format string
	if err == nil {
		format, err = fusionapi.ParseFormat(q.Get("format"))
	}
	if err == nil && format == fusionapi.FormatNodeLink && q.Has("window") { // (the graph form carries no changes, which window is the window of)
		err = &fusionapi.Error{Status: http.StatusBadRequest, Msg: "window is the window of the changes, which format=nodelink does not carry; leave it out"}
	}
	var at time.Time
	if err == nil && q.Has("at") { // in the forms from and to take: RFC 3339, unix seconds, now-<duration>
		now := g.Now()
		if at, err = fusionapi.ParseTime(q.Get("at"), now); err != nil {
			err = &fusionapi.Error{Status: http.StatusBadRequest, Msg: "at: " + err.Error()}
		} else if at.After(now) {
			err = &fusionapi.Error{Status: http.StatusBadRequest, Msg: "at is in the future; the topology of now is read without it"}
		}
	}
	if err != nil {
		g.Err(w, err)
		return
	}
	groups, err := g.Groups(r, who)
	var app *fusionapi.AppGroup
	if err == nil {
		app, err = fusionapi.FindGroup(groups, r.PathValue("name"))
	}
	if err != nil {
		g.Err(w, err)
		return
	}
	ex, until := g.Extras(), g.Now()
	var v *fusionapi.TopologyView
	if !at.IsZero() {
		h, ok := ex.(topologyAter)
		if !ok {
			g.Err(w, &fusionapi.Error{Status: 404, Msg: "history is not enabled on this server: it needs the graph database"})
			return
		}
		var members []fusionapi.AppMember
		if v, members, err = h.TopologyAt(r.Context(), app.ID, at); err == nil {
			then := *app
			then.Members, then.Unresolved = members, 0
			if vis := fusionapi.VisibleGroups([]fusionapi.AppGroup{then}, who.Scope); len(vis) > 0 { // as the caller may see it, then
				app, until = &vis[0], at
			} else {
				err = &fusionapi.Error{Status: 404, Msg: "the application had no services you may see at that time"}
			}
		}
	} else {
		v, err = ex.Topology(r.Context())
	}
	if err != nil {
		g.Err(w, err)
		return
	}
	t := fusionapi.BuildAppTopology(app, v, who.Scope, noise)
	if placement {
		t.AddPlacement(v)
	}
	if !at.IsZero() {
		snap := v.At
		t.Source, t.AsOf, t.SnapshotAt = "history", at, &snap
	}
	// A caller who may see no service or link of the application is asked nothing: a store reads an empty list of targets as every event.
	if ids := t.TargetsFor(who.Scope); len(ids) > 0 && format != fusionapi.FormatNodeLink { // (the graph carries no changes)
		evs, err := ex.Changes(r.Context(), until.Add(-window), until, nil, ids, fusionapi.MaxAppChanges+1)
		if err != nil {
			g.Err(w, err)
			return
		}
		t.AddVisibleChanges(evs, v, who.Scope)
	}
	if format == fusionapi.FormatNodeLink {
		httpkit.WriteJSON(w, 200, t.NodeLink())
		return
	}
	httpkit.WriteJSON(w, 200, t)
}

// Service is one service at a glance, by the service.name its telemetry carries.
func (g *Guard) Service(w http.ResponseWriter, r *http.Request, c *fusionapi.Client, who Caller) {
	tr, err := Range(r, g.Now())
	if err != nil {
		g.Err(w, err)
		return
	}
	if !g.chargeReads(w, who, fusionapi.OverviewReads(who.Scope)) {
		return
	}
	q := r.URL.Query()
	o, err := c.ServiceOverview(r.Context(), who.Scope, fusionapi.AppMember{Name: r.PathValue("name"), Namespace: q.Get("namespace"), Cluster: q.Get("cluster")}, tr)
	if err != nil {
		g.Err(w, err)
		return
	}
	httpkit.WriteJSON(w, 200, o)
}
