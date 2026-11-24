package fusionapi

import (
	"slices"
	"strings"
)

// The places a query reads, and how each store is told them.
//
// A caller's rights (Scope.Namespaces and Clusters) and the application it chose to look at (the Focus fields) both come
// down to the same thing: the namespaces, clusters and names telemetry must carry to be read. placeSet is that, once, and
// its methods write it in the words of each store: PromQL label matchers and selectors, LogQL stream matchers and label
// filters, TraceQL conditions. The second checks on what the stores answer read the same set (contains), so a reader
// finds here, in one file, what any query this package builds may read.

// placeSet is where a query may read. The first three fields are the union cut: telemetry is read only if its namespace,
// cluster and name are each one of these (an empty list leaves that dimension open). exact, when there is any, narrows
// that cut to the places themselves: telemetry must also be from one of them, so that a name is read only where a member
// of that name runs.
//
// none marks the set of a caller limited to applications none of which has a place it may read: it contains nothing, and
// what it renders for a store matches nothing either (nowhere), so that no query built from it is ever an open one.
type placeSet struct {
	namespaces, clusters, names []string
	exact                       []FocusPlace
	none                        bool
}

// places is where a query built from structured filters reads: the focus where there is one (FocusOn keeps it inside
// the rights), else the rights. A focus on names alone keeps the rights' namespaces and clusters. Exact places past
// maxFocusPlaces are left to the union cut, which the stores can serve; what they answer is checked against the rights'
// exact places all the same (rights keeps them all).
func (s Scope) places() placeSet {
	p := s.rights()
	if p.none {
		return p
	}
	if len(s.FocusServices) > 0 || len(s.Focus) > 0 {
		p.names, p.exact = s.FocusServices, s.Focus
		if len(s.FocusNamespaces) > 0 {
			p.namespaces = s.FocusNamespaces
		}
		if len(s.FocusClusters) > 0 {
			p.clusters = s.FocusClusters
		}
	}
	if len(p.exact) > maxFocusPlaces && !s.AppLimited {
		// (A caller limited to applications is always read place by place, however many: its rights are those places, and a
		// list of names, which cannot be checked afterwards, must not be read by their union. MaxTokenPlaces bounds them.)
		p.exact = nil
	}
	return p
}

// rights is where the caller may read at all, whatever it chose to look at: what a raw PromQL query is held to, and what
// every answer is checked against once more. For a caller limited to applications it is their places, every one of them
// (however many), each inside the namespace and cluster limits, and the union of those as the cut; with none, nowhere.
func (s Scope) rights() placeSet {
	if !s.AppLimited {
		return placeSet{namespaces: s.Namespaces, clusters: s.Clusters}
	}
	var p placeSet
	for _, f := range s.RightsPlaces {
		// (LimitToApplications made them so; a Scope put together otherwise is held to the same rule here.)
		if f.Namespace == "" || f.Cluster == "" || len(f.Names) == 0 || !within(s.Namespaces, f.Namespace) || !within(s.Clusters, f.Cluster) {
			continue
		}
		p.exact = append(p.exact, f)
		p.namespaces = append(p.namespaces, f.Namespace)
		p.clusters = append(p.clusters, f.Cluster)
		p.names = append(p.names, f.Names...)
	}
	if len(p.exact) == 0 {
		return nowhere()
	}
	p.namespaces, p.clusters, p.names = distinctSorted(p.namespaces), distinctSorted(p.clusters), distinctSorted(p.names)
	return p
}

// noPlace is a value no namespace, cluster or service is called (it is not a Kubernetes name, nor an ikhnos cluster id):
// what nowhere renders, so that a query built for a caller who may read nothing matches nothing.
const noPlace = "(nowhere: this token may read no place)"

// nowhere is the set of a caller who may read nothing (placeSet.none).
func nowhere() placeSet {
	no := []string{noPlace}
	return placeSet{namespaces: no, clusters: no, names: no, exact: []FocusPlace{{Namespace: noPlace, Cluster: noPlace, Names: no}}, none: true}
}

// cut is the union cut of an application on its own, before any caller's rights: its names, and the namespaces and
// clusters its members run in. A member that names no namespace (or cluster) leaves the application open in that
// dimension, because its telemetry may come from any.
func (g *AppGroup) cut() placeSet {
	p := placeSet{names: g.ServiceNames()}
	every := func(of func(AppMember) string) bool {
		return !slices.ContainsFunc(g.Members, func(m AppMember) bool { return of(m) == "" })
	}
	if every(func(m AppMember) string { return m.Namespace }) {
		p.namespaces = g.Namespaces()
	}
	if every(func(m AppMember) string { return m.Cluster }) {
		p.clusters = g.Clusters()
	}
	return p
}

// unnamed is the set with no names in its cut, for a query that names them itself: one selector per name label, or per
// place.
func (p placeSet) unnamed() placeSet {
	p.names = nil
	return p
}

// within says whether v is one of a limit's values; an empty limit is no limit. A value that is not there ("") is in no
// limit, so telemetry that does not say its namespace or cluster is invisible to a caller limited in that dimension.
func within(limit []string, v string) bool {
	return len(limit) == 0 || (v != "" && slices.Contains(limit, v))
}

// holds says whether telemetry from namespace ns of cluster cl, carrying name, is from this place. A place with no
// namespace or cluster (a member that names none) takes any there.
func (p FocusPlace) holds(ns, cl, name string) bool {
	return (p.Namespace == "" || p.Namespace == ns) && (p.Cluster == "" || p.Cluster == cl) && slices.Contains(p.Names, name)
}

// contains is the rule the queries below carry, for checking what came back: in the cut, and in one of the exact places
// when there are any.
func (p placeSet) contains(ns, cl, name string) bool {
	if p.none {
		return false
	}
	if !within(p.namespaces, ns) || !within(p.clusters, cl) || !within(p.names, name) {
		return false
	}
	return len(p.exact) == 0 || slices.ContainsFunc(p.exact, func(f FocusPlace) bool { return f.holds(ns, cl, name) })
}

// named says whether the set has names to match (the rights of a caller limited to applications, or a focus).
func (p placeSet) named() bool { return len(p.names) > 0 || len(p.exact) > 0 }

// containsWorkload says whether a workload of this name, under the workload label label, is from one of the exact places:
// in a place of a caller limited to applications only a member of that kind (FocusPlace.Workloads), in a focus's any name.
func (p placeSet) containsWorkload(ns, cl, label, name string) bool {
	if p.none || !within(p.namespaces, ns) || !within(p.clusters, cl) || !within(p.names, name) {
		return false
	}
	return len(p.exact) == 0 || slices.ContainsFunc(p.exact, func(f FocusPlace) bool {
		if f.Workloads == nil {
			return f.holds(ns, cl, name)
		}
		return (f.Namespace == "" || f.Namespace == ns) && (f.Cluster == "" || f.Cluster == cl) && slices.Contains(f.Workloads[label], name)
	})
}

// containsNamed is contains for telemetry that carries several names it may be known by (a resource's service.name and its
// workload's name, a series' service_name and workload labels): it is from one of the places under any of them. A set with
// no names to match (rights limited by namespace and cluster alone) asks the place only, as contains with no name does.
func (p placeSet) containsNamed(ns, cl string, names ...string) bool {
	if !p.named() {
		return p.contains(ns, cl, "")
	}
	for _, n := range names {
		if n != "" && p.contains(ns, cl, n) {
			return true
		}
	}
	return false
}

// NamespaceVisible and ClusterVisible check a result's own attributes against the Scope's rights, one dimension each; they
// are the second line behind the matchers the queries carry. For a caller limited to applications they are the namespaces and
// clusters its applications' places are in, which is all a check of one dimension can say: a result that carries a name is
// checked by its place as well (visibleNamed).
func (s Scope) NamespaceVisible(ns string) bool {
	p := s.rights()
	return !p.none && within(p.namespaces, ns)
}

func (s Scope) ClusterVisible(cl string) bool {
	p := s.rights()
	return !p.none && within(p.clusters, cl)
}

// visibleAt is the second check on a result's own namespace and cluster: they are ones the caller's rights may see. It is
// the line behind the matchers every query carries. It knows no name, so a caller limited to applications sees nothing by
// it: telemetry of no service is no application's.
func (s Scope) visibleAt(ns, cl string) bool { return s.rights().contains(ns, cl, "") }

// visibleNamed is visibleAt for telemetry that names what it is from: in a place of the rights under one of its names.
// For a caller not limited to applications the names change nothing.
func (s Scope) visibleNamed(ns, cl string, names ...string) bool {
	return s.rights().containsNamed(ns, cl, names...)
}

// RunsAt says whether a member of the application has this name (or alias) in this namespace and cluster: the rule an
// exact focus puts on every query (see FocusOn), for an answer built from something else, such as the topology. The empty
// name is no member's.
func (g *AppGroup) RunsAt(name, namespace, cluster string) bool {
	places := g.places()
	return len(places) > 0 && placeSet{exact: places}.contains(namespace, cluster, name)
}

// dialect is how a query language writes a condition on one key: eq for one value, in for any of several. and and or
// join conditions, in the languages that join them with words (LogQL's label filters, TraceQL).
type dialect struct {
	namespace, cluster, name string
	eq                       func(key, value string) string
	in                       func(key string, values []string) string
	and, or                  string
}

// labelSyntax is PromQL's and LogQL's: both take Prometheus' label matchers, in a selector and (LogQL) in a label filter
// of the pipeline alike, and anchor the regular expression of =~ themselves.
var labelSyntax = dialect{
	namespace: lblNamespace, cluster: lblCluster, name: lblService,
	eq:  func(k, v string) string { return k + "=" + quote(v) },
	in:  func(k string, vs []string) string { return k + "=~" + quote(regexAny(vs)) },
	and: " and ", or: " or ",
}

// traceSyntax is TraceQL's, on the resource attributes. Any of several values is one anchored regular expression of
// the values, each escaped (anyOf).
var traceSyntax = dialect{
	namespace: "resource." + attrNamespace, cluster: "resource." + attrCluster, name: "resource." + attrService,
	eq:  func(k, v string) string { return k + " = " + quote(v) },
	in:  anyOf,
	and: " && ", or: " || ",
}

// named is the dialect with the names under another key: a pod's metrics name their workload, not their service.
func (d dialect) named(key string) dialect {
	d.name = key
	return d
}

// cutOn is the union cut as three conditions, of the namespaces, the clusters and the names, each "" where the cut is
// open. LogQL puts them in different parts of a query; the other languages take them together (cutConds).
func (p placeSet) cutOn(d dialect) (ns, cl, names string) {
	if len(p.namespaces) > 0 {
		ns = d.in(d.namespace, p.namespaces)
	}
	if len(p.clusters) > 0 {
		cl = d.in(d.cluster, p.clusters)
	}
	if len(p.names) > 0 {
		names = d.in(d.name, p.names)
	}
	return ns, cl, names
}

// cutConds is the union cut as a list of conditions, in the order namespace, cluster, names, leaving out the open ones.
func (p placeSet) cutConds(d dialect) []string {
	ns, cl, names := p.cutOn(d)
	return slices.DeleteFunc([]string{ns, cl, names}, func(c string) bool { return c == "" })
}

// placeConds are the conditions of each exact place, for a query language to join with its own "or": the place's
// namespace and cluster (where it has them) and its names, under the keys the dialect knows them by.
func (p placeSet) placeConds(d dialect) [][]string {
	out := make([][]string, 0, len(p.exact))
	for _, f := range p.exact {
		var c []string
		if f.Namespace != "" {
			c = append(c, d.eq(d.namespace, f.Namespace))
		}
		if f.Cluster != "" {
			c = append(c, d.eq(d.cluster, f.Cluster))
		}
		out = append(out, append(c, d.in(d.name, f.Names)))
	}
	return out
}

// either joins alternatives, each a list of conditions that must all hold, into one condition that holds where any of
// them does: (a and b) or (c). An alternative written twice is written once, as it can only match the same.
func (d dialect) either(alts [][]string) string {
	out := make([]string, len(alts))
	for i, c := range alts {
		out[i] = "(" + strings.Join(c, d.and) + ")"
	}
	return strings.Join(uniqueInOrder(out), d.or)
}

// promSelectors are the PromQL selectors that read the places: base (the filter's own matchers, with the cut's
// namespaces and clusters but not its names) once per name label, with the cut's names under that label, or, with exact
// places, once per name label and place, with the place's namespace, cluster and names. A series matches one of them
// only if it carries one of the names under one of the labels where it may be read; a set with neither names nor places
// is base alone. A selector holds only matchers that must all hold, so an exact focus cannot be one "or" as in the other
// languages and is read as one selector per place, whose results the store unites.
func (p placeSet) promSelectors(base, nameLabels []string) []string {
	sel := func(m []string) string { return "{" + strings.Join(append(slices.Clone(base), m...), ",") + "}" }
	if len(p.names) == 0 && len(p.exact) == 0 {
		return []string{sel(nil)}
	}
	var out []string
	for _, l := range nameLabels {
		d := labelSyntax.named(l)
		if len(p.exact) == 0 {
			out = append(out, sel([]string{d.in(l, p.names)}))
			continue
		}
		for _, f := range p.exact {
			names := f.Names
			if f.Workloads != nil && l != lblService {
				// A member's name as a workload's only under its own kind's label (a place of a caller limited to applications).
				if names = f.Workloads[l]; len(names) == 0 {
					continue
				}
			}
			var c []string
			if f.Namespace != "" {
				c = append(c, d.eq(d.namespace, f.Namespace))
			}
			if f.Cluster != "" {
				c = append(c, d.eq(d.cluster, f.Cluster))
			}
			out = append(out, sel(append(c, d.in(l, names))))
		}
	}
	return out
}

// promLimits is the set as withMatchers takes it, for PromQL a caller wrote: the values each label must have, by label.
// withMatchers adds them to every selector of the parsed query in an order of its own (service, namespace, cluster),
// printed by Prometheus' parser rather than written here. An exact focus is not part of it: a query that is not built
// here has nowhere to put an "or" of places (see narrowToApplication).
func (p placeSet) promLimits() map[string][]string {
	if p.none || len(p.exact) > 0 {
		// (Not reached: a caller limited to applications sends no PromQL of its own, see restrictPromQL. Should it be, the
		// limits are nowhere, never the union of places, which would let a name be read in another member's namespace.)
		no := []string{noPlace}
		return map[string][]string{lblService: no, lblNamespace: no, lblCluster: no}
	}
	return map[string][]string{lblService: p.names, lblNamespace: p.namespaces, lblCluster: p.clusters}
}
