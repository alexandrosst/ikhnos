package fusionapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
)

// maxAppServices bounds how many service names one application may put into a query. Past it the query would be a
// regular expression too long for the stores to take well, and an application that large is not a useful filter.
const maxAppServices = 500

// FindGroup finds an ikhnos application by id, or else by name without regard to case. A name that two applications
// share is ambiguous and is refused rather than guessed.
func FindGroup(groups []AppGroup, ref string) (*AppGroup, error) {
	ref = strings.TrimSpace(ref)
	if err := checkValue("application", ref); err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].ID == ref {
			return &groups[i], nil
		}
	}
	var hit *AppGroup
	for i := range groups {
		if strings.EqualFold(groups[i].Name, ref) {
			if hit != nil {
				return nil, badRequest("more than one application is called %q; use its id", ref)
			}
			hit = &groups[i]
		}
	}
	if hit == nil {
		return nil, errf(http.StatusNotFound, "no application %q", ref)
	}
	return hit, nil
}

// FocusOn narrows a Scope to one ikhnos application: the services it is made of, and the namespaces and clusters they run
// in. The caller's own limits stay in force - the focus is intersected with them, never widened by them - so a token
// limited to one namespace sees only the part of the application in it, and one that sees none of it gets "not found",
// the same as for an application that does not exist.
//
// The result is for structured queries only. It holds the union of the members' names, namespaces and clusters (a first
// cut the stores' indexes serve) and, for an application in at most maxFocusPlaces places, each member's names where that
// member runs (Focus), which the queries add so that a name is not matched in another member's namespace. A member with no
// namespace or cluster leaves that dimension open for its own names only.
func (s Scope) FocusOn(g *AppGroup) (Scope, error) {
	names := g.ServiceNames()
	if len(names) == 0 {
		if g.Unresolved > 0 {
			return Scope{}, errf(http.StatusNotFound, "application %q has no services FUSION can match: %d of its members are not in the live topology (a cluster that is not connected, or a service that has gone)", g.Name, g.Unresolved)
		}
		return Scope{}, errf(http.StatusNotFound, "application %q has no services: none has been added to it in ikhnos", g.Name)
	}
	if len(names) > maxAppServices {
		return Scope{}, badRequest("application %q has %d service names, more than the %d one filter can carry", g.Name, len(names), maxAppServices)
	}
	out := s
	out.FocusServices = names
	var err error
	if out.FocusNamespaces, err = narrow(s.Namespaces, g, g.Namespaces(), func(m AppMember) string { return m.Namespace }); err != nil {
		return Scope{}, errf(http.StatusNotFound, "no application %q", g.Name)
	}
	if out.FocusClusters, err = narrow(s.Clusters, g, g.Clusters(), func(m AppMember) string { return m.Cluster }); err != nil {
		return Scope{}, errf(http.StatusNotFound, "no application %q", g.Name)
	}
	if places := g.places(); len(places) <= maxFocusPlaces {
		out.Focus = places
	}
	return out, nil
}

// maxFocusPlaces bounds how many namespace-and-cluster places an exact focus names. A metric read adds a selector per place
// and per name label (the service's and the three workload labels), so at the bound one read is some 80 selectors and its
// request line tens of KiB. The stores are asked directly, at their in-cluster Services, and take request lines of up to
// 1 MiB (Go's default); past the bound the cost of the read itself is what grows too much, so the union alone applies.
const maxFocusPlaces = 20

// places groups the members' names by the namespace and cluster they run in, in a stable order.
func (g *AppGroup) places() []FocusPlace {
	idx := map[[2]string]int{}
	var out []FocusPlace
	for _, m := range g.Members {
		k := [2]string{m.Namespace, m.Cluster}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, FocusPlace{Namespace: m.Namespace, Cluster: m.Cluster})
		}
		for _, n := range append([]string{m.Name}, m.Aliases...) {
			if n != "" && !slices.Contains(out[i].Names, n) {
				out[i].Names = append(out[i].Names, n)
			}
		}
	}
	// A place none of whose members has a name says nothing to match (and an empty list would match every series there).
	out = slices.DeleteFunc(out, func(p FocusPlace) bool { return len(p.Names) == 0 })
	for i := range out {
		sort.Strings(out[i].Names)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Cluster != out[b].Cluster {
			return out[a].Cluster < out[b].Cluster
		}
		return out[a].Namespace < out[b].Namespace
	})
	return out
}

// placeConds are the conditions of each place, for a query language to join with its own "or": the place's namespace and
// cluster (where it has them) and its names, under the keys the language knows them by, written by eq and in.
func placeConds(places []FocusPlace, nsKey, clKey, nameKey string, eq func(key, value string) string, in func(key string, values []string) string) [][]string {
	out := make([][]string, 0, len(places))
	for _, p := range places {
		var c []string
		if p.Namespace != "" {
			c = append(c, eq(nsKey, p.Namespace))
		}
		if p.Cluster != "" {
			c = append(c, eq(clKey, p.Cluster))
		}
		out = append(out, append(c, in(nameKey, p.Names)))
	}
	return out
}

// narrow intersects a limit the caller has with the values the application's members have in one dimension. A member
// without a value there means the application is not confined in that dimension, so the caller's limit stands as it is.
func narrow(have []string, g *AppGroup, want []string, of func(AppMember) string) ([]string, error) {
	for _, m := range g.Members {
		if of(m) == "" {
			return nil, nil // not confined in this dimension: the caller's own limit stands alone
		}
	}
	if len(have) == 0 {
		return slices.Clone(want), nil
	}
	var out []string
	for _, v := range want {
		if slices.Contains(have, v) {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, errf(http.StatusNotFound, "outside the caller's scope")
	}
	return out, nil
}

// VisibleGroups is the applications a caller may know of, each with only the services the caller may see. A caller whose
// access is limited to certain namespaces or clusters sees an application only through the services of it in them, and not
// at all when it has none there: an application it cannot see does not exist for it, and nothing about it (that it exists,
// how many services it has, that two share a name) can be learned from how a lookup fails. A caller with no limit sees
// every application, including one with no services yet.
func VisibleGroups(groups []AppGroup, s Scope) []AppGroup {
	if s.Unrestricted() {
		return groups
	}
	out := make([]AppGroup, 0, len(groups))
	for _, g := range groups {
		kept := g
		kept.Members = nil
		kept.Unresolved = 0 // members the topology has no place for have no namespace or cluster to be checked against; their number is not the caller's to know
		for _, m := range g.Members {
			if s.NamespaceVisible(m.Namespace) && s.ClusterVisible(m.Cluster) {
				kept.Members = append(kept.Members, m)
			}
		}
		if len(kept.Members) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// AppView is one ikhnos application as the API lists it: what it is made of, and which signals FUSION has for it.
type AppView struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Services    []AppServiceView `json:"services"`
	// Signals are the signal types any of its services has telemetry for in the range.
	Signals    []Signal `json:"signals"`
	Namespaces []string `json:"namespaces"`
	Clusters   []string `json:"clusters"`
	// Unresolved is how many members ikhnos has an id for but cannot tie to a running service, and so cannot match with
	// telemetry; when it is not zero the application may look emptier here than it does in ikhnos.
	Unresolved int `json:"unresolvedMembers,omitempty"`
}

// AppServiceView is one service of an application with the signals its telemetry has in the range.
type AppServiceView struct {
	AppMember
	Signals []Signal `json:"signals"`
}

// DescribeGroups joins the applications with the services that have telemetry (from Client.Applications). A service is
// matched by the names its telemetry may carry (its name and its aliases), not by namespace or cluster, because the
// service listing is by service.name alone; a service of the same name elsewhere can make a member look like it has
// telemetry that it does not. The result is sorted by name, then id. DescribeGroupsIn is the exact one.
func DescribeGroups(groups []AppGroup, withTelemetry []Application) []AppView {
	perGroup := map[string][]Application{}
	for _, g := range groups {
		perGroup[g.ID] = withTelemetry
	}
	return DescribeGroupsIn(groups, perGroup)
}

// DescribeGroupsIn is DescribeGroups with each application's own list of services with telemetry, looked for where its members
// run (Client.GroupSignals): a member has a signal only if a service of its name has it in the application's own places.
func DescribeGroupsIn(groups []AppGroup, perGroup map[string][]Application) []AppView {
	out := make([]AppView, 0, len(groups))
	for _, g := range groups {
		have := map[string]map[string]bool{}
		for _, a := range perGroup[g.ID] {
			have[a.Name] = map[string]bool{}
			for _, sig := range a.Signals {
				have[a.Name][string(sig)] = true
			}
		}
		v := AppView{ID: g.ID, Name: g.Name, Description: g.Description, Services: []AppServiceView{},
			Namespaces: orNone(g.Namespaces()), Clusters: orNone(g.Clusters()), Unresolved: g.Unresolved}
		all := map[string]bool{}
		for _, m := range g.Members {
			sv := AppServiceView{AppMember: m}
			own := map[string]bool{}
			for _, n := range append([]string{m.Name}, m.Aliases...) {
				for sig := range have[n] {
					own[sig], all[sig] = true, true
				}
			}
			sv.Signals = signalList(own)
			v.Services = append(v.Services, sv)
		}
		v.Signals = signalList(all)
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if !strings.EqualFold(out[i].Name, out[j].Name) {
			return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// MaxGroupSignals is how many applications GroupSignals reads one by one; past it a caller matches by name (DescribeGroups).
const MaxGroupSignals = 25

// GroupSignals is, for each application, its services that have telemetry in the range, each looked for in the application's
// own places (s focused on it): what DescribeGroupsIn needs. It reads the stores once per application, a few at a time. The
// sources say, per signal, the worst any application's read went; the warnings are those of all of them, once each.
func (c *Client) GroupSignals(ctx context.Context, s Scope, groups []AppGroup, tr TimeRange) (map[string][]Application, map[string]string, []string, error) {
	type result struct {
		apps     []Application
		sources  map[string]string
		warnings []string
		err      error
	}
	results := make([]result, len(groups))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := range groups {
		fs, err := s.FocusOn(&groups[i])
		if err != nil {
			continue // an application with no services to look for has none with telemetry
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquire(ctx, sem)
			if err != nil {
				results[i].err = err
				return
			}
			defer release()
			r := &results[i]
			r.apps, r.sources, r.warnings, r.err = c.Applications(ctx, fs, tr)
		}()
	}
	wg.Wait()
	if err := callerGone(ctx); err != nil {
		return nil, nil, nil, err
	}
	rank := map[string]int{SourceOK: 0, SourceNotAllowed: 1, SourceUnavailable: 2, SourceError: 3}
	sources := map[string]string{}
	perGroup := map[string][]Application{}
	var warnings []string
	var firstErr error
	read := false
	for i, r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			warnings = append(warnings, fmt.Sprintf("application %s: %v", groups[i].Name, r.err))
			continue
		}
		if r.sources == nil {
			continue
		}
		read = true
		perGroup[groups[i].ID] = r.apps
		for sig, st := range r.sources {
			if cur, ok := sources[sig]; !ok || rank[st] > rank[cur] {
				sources[sig] = st
			}
		}
		warnings = append(warnings, r.warnings...)
	}
	if !read && firstErr != nil {
		return nil, nil, nil, firstErr
	}
	for _, sig := range Signals {
		if _, ok := sources[sig]; !ok {
			if s.Allows(sig) {
				sources[sig] = SourceOK
			} else {
				sources[sig] = SourceNotAllowed
			}
		}
	}
	return perGroup, sources, uniqueInOrder(warnings), nil
}

func signalList(set map[string]bool) []Signal {
	out := []Signal{}
	for _, sig := range Signals {
		if set[sig] {
			out = append(out, Signal(sig))
		}
	}
	return out
}

func orNone(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
