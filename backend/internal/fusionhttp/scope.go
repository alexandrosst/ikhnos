package fusionhttp

import (
	"fmt"
	"net/http"
	"time"

	"ikhnos/internal/fusionapi"
)

// Range is the time range the request asks for: from and to, in the forms fusionapi.ParseRange reads.
func Range(r *http.Request, now time.Time) (fusionapi.TimeRange, error) {
	q := r.URL.Query()
	return fusionapi.ParseRange(q.Get("from"), q.Get("to"), now)
}

// Scope is the Scope a structured list read runs with: the caller's own, narrowed to one ikhnos application when the
// request names one (application=<id or name>). The application is resolved here, at read time, from what ikhnos knows
// now, so changing an application changes what it shows without touching any stored telemetry. The caller's own Scope
// (who.Scope) is what reading a trace keeps using: a fused read is narrowed to an application by what it joins (fuseFocus),
// never by the spans of the trace, which a search for the application's traces must return whole.
func (g *Guard) Scope(r *http.Request, who Caller) (fusionapi.Scope, error) {
	q := r.URL.Query()
	if !q.Has("application") {
		return who.Scope, nil
	}
	// System telemetry (the hosts, the collectors, energy, accelerators, the links between clusters) carries no service, so it
	// is no application's: asked for together, the answer would be empty every time.
	if cats, _ := fusionapi.ParseCategories(q.Get("category")); len(cats) == 1 && cats[0] == fusionapi.CategorySystem {
		return fusionapi.Scope{}, &fusionapi.Error{Status: http.StatusBadRequest, Msg: "category=system is the hosts, the collectors and the links between clusters, which are no application's; leave application out to read them"}
	}
	if n := q.Get("name"); n != "" && fusionapi.MetricCategory(n) == fusionapi.CategorySystem {
		return fusionapi.Scope{}, &fusionapi.Error{Status: http.StatusBadRequest, Msg: fmt.Sprintf("%s is a system metric (of a host, a collector, an energy meter or an accelerator), which carries no service and so is no application's; leave application out, or join it with the application's pods in your own PromQL", n)}
	}
	groups, err := g.Groups(r, who)
	if err != nil {
		return fusionapi.Scope{}, err
	}
	grp, err := fusionapi.FindGroup(groups, q.Get("application"))
	if err != nil {
		return fusionapi.Scope{}, err
	}
	return who.Scope.FocusOn(grp)
}

// fuseFocus gives a fused read the applications its resources are tagged with and, when ref names one (application=), the
// application its joins are narrowed to. The tags are a help, not the answer: without them (a server that does not know the
// applications, or cannot read them now) the read goes on untagged. A named application must be found, or the read fails.
func (g *Guard) fuseFocus(r *http.Request, who Caller, ref string, opts *fusionapi.FuseOptions) error {
	if ref == "" {
		if err := opts.CheckApplicationVars(); err != nil { // before the search, not once per hit
			return err
		}
	}
	if g.Extras() == nil && ref == "" {
		return nil
	}
	groups, err := g.Groups(r, who)
	if err != nil {
		if ref == "" {
			return nil
		}
		return err
	}
	opts.Applications = groups
	if ref != "" {
		grp, err := fusionapi.FindGroup(groups, ref)
		if err != nil {
			return err
		}
		opts.Application = grp
	}
	return nil
}

// Groups are the ikhnos applications the caller may know of: each with only the services in the caller's namespaces
// and clusters, and only those that have any (fusionapi.VisibleGroups).
func (g *Guard) Groups(r *http.Request, who Caller) ([]fusionapi.AppGroup, error) {
	ex := g.Extras()
	if ex == nil {
		return nil, &fusionapi.Error{Status: http.StatusServiceUnavailable, Msg: "this server does not know the applications"}
	}
	groups, err := ex.Applications(r.Context())
	if err != nil {
		return nil, err
	}
	return fusionapi.VisibleGroups(groups, who.Scope), nil
}

// orEmpty is v, or an empty list where it is nil, so it is answered as [] and not as null.
func orEmpty(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}
