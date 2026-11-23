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
type placeSet struct {
	namespaces, clusters, names []string
	exact                       []FocusPlace
}

// places is where a query built from structured filters reads: the focus where there is one (FocusOn keeps it inside
// the rights), else the rights. A focus on names alone keeps the rights' namespaces and clusters.
func (s Scope) places() placeSet {
	p := placeSet{namespaces: s.Namespaces, clusters: s.Clusters, names: s.FocusServices, exact: s.Focus}
	if len(s.FocusNamespaces) > 0 {
		p.namespaces = s.FocusNamespaces
	}
	if len(s.FocusClusters) > 0 {
		p.clusters = s.FocusClusters
	}
	return p
}

// rights is where the caller may read at all, whatever it chose to look at: what a raw PromQL query is held to, and what
// every answer is checked against once more.
func (s Scope) rights() placeSet { return placeSet{namespaces: s.Namespaces, clusters: s.Clusters} }

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
	if !within(p.namespaces, ns) || !within(p.clusters, cl) || !within(p.names, name) {
		return false
	}
	return len(p.exact) == 0 || slices.ContainsFunc(p.exact, func(f FocusPlace) bool { return f.holds(ns, cl, name) })
}

// NamespaceVisible and ClusterVisible check a result's own attributes against the Scope's rights, one dimension each; they
// are the second line behind the matchers the queries carry.
func (s Scope) NamespaceVisible(ns string) bool { return within(s.Namespaces, ns) }

func (s Scope) ClusterVisible(cl string) bool { return within(s.Clusters, cl) }

// visibleAt is the second check on a result's own namespace and cluster: they are ones the caller's rights may see. It is
// the line behind the matchers every query carries.
func (s Scope) visibleAt(ns, cl string) bool { return s.rights().contains(ns, cl, "") }

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
		for _, c := range p.placeConds(d) {
			out = append(out, sel(c))
		}
	}
	return out
}

// promLimits is the set as withMatchers takes it, for PromQL a caller wrote: the values each label must have, by label.
// withMatchers adds them to every selector of the parsed query in an order of its own (service, namespace, cluster),
// printed by Prometheus' parser rather than written here. An exact focus is not part of it: a query that is not built
// here has nowhere to put an "or" of places (see narrowToApplication).
func (p placeSet) promLimits() map[string][]string {
	return map[string][]string{lblService: p.names, lblNamespace: p.namespaces, lblCluster: p.clusters}
}
