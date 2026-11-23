package fusionapi

import (
	"sort"
)

// touched is what a trace is about in the topology, by the ids that events about it carry.
type touched struct {
	services map[string]*Resource // service id
	deps     map[string]*Resource // dependency id: a link of one of the services
	apps     map[string]*Resource // application id
	nodes    map[string]*Resource // node id, and node name for a node the topology does not know
	clusters map[string]bool
	ns       map[string]string // service id -> namespace
}

// touchedBy is what the resources are about; only is the application the read is narrowed to ("" for none), whose events are
// then the only application events kept (the others' services may be on the resources too).
func touchedBy(v *TopologyView, resources []*Resource, only string) touched {
	t := touched{map[string]*Resource{}, map[string]*Resource{}, map[string]*Resource{}, map[string]*Resource{}, map[string]bool{}, map[string]string{}}
	for _, r := range resources {
		if v != nil {
			if sv, _ := v.matchService(r); sv != nil {
				t.services[sv.ID], t.ns[sv.ID] = r, sv.Namespace
				for _, a := range sv.AppIDs {
					if only == "" || a == only {
						t.apps[a] = r
					}
				}
			}
			for _, n := range v.Nodes {
				if r.Node != "" && n.Name == r.Node && (r.Cluster == "" || n.Cluster == r.Cluster) {
					t.nodes[n.ID] = r
				}
			}
		}
		if r.Node != "" {
			t.nodes[r.Node] = r
		}
		if r.Cluster != "" {
			t.clusters[r.Cluster] = true
		}
	}
	if v != nil {
		for _, l := range v.Links {
			if l.Noise != "" || l.ID == "" {
				continue
			}
			if r := t.services[l.From]; r != nil {
				t.deps[l.ID] = r
			} else if r := t.services[l.To]; r != nil {
				t.deps[l.ID] = r
			}
		}
	}
	return t
}

// ids are the targets events about the trace are about: all of them but the nodes the topology does not know by id.
func (t touched) ids() []string {
	var out []string
	for _, m := range []map[string]*Resource{t.services, t.deps, t.apps, t.nodes} {
		for id := range m {
			out = append(out, id)
		}
	}
	for c := range t.clusters {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// relevantChanges keeps the events that are about the trace: those of its services (matched through the topology), of the
// traffic seen to and from them, of the applications they are in, of the nodes its pods ran on, and of its clusters. A Scope
// limited to namespaces sees only the events of services in them, and of links between what it may see.
func relevantChanges(evs []ChangeEvent, v *TopologyView, s Scope, resources []*Resource, only string) []ChangeEvent {
	t := touchedBy(v, resources, only)
	var byID map[string]*TopoService
	var link map[string]TopoLink
	if v != nil {
		byID, link = v.byID(), map[string]TopoLink{}
		for _, l := range v.Links {
			link[l.ID] = l
		}
	}
	limited := len(s.Namespaces) > 0
	var out []ChangeEvent
	for _, e := range evs {
		if !s.ClusterVisible(e.Cluster) {
			continue
		}
		switch e.TargetKind {
		case "service":
			r, ok := t.services[e.TargetID]
			if !ok {
				continue
			}
			e.Namespace, e.Resource = t.ns[e.TargetID], r.Key
			if !s.NamespaceVisible(e.Namespace) {
				continue
			}
		case "dependency":
			r, ok := t.deps[e.TargetID]
			if !ok || !endVisible(s, byID, link[e.TargetID].From, link[e.TargetID].FromKind) || !endVisible(s, byID, link[e.TargetID].To, link[e.TargetID].ToKind) {
				continue
			}
			e.Resource = r.Key
		case "application":
			r, ok := t.apps[e.TargetID]
			if !ok || !s.Unrestricted() { // it holds services the Scope may not see, in a namespace or a cluster it is not limited to
				continue
			}
			e.Resource = r.Key
		case "node":
			if limited {
				continue
			}
			r := t.nodes[e.TargetID]
			if r == nil {
				r = t.nodes[e.Name]
			}
			if r == nil {
				continue
			}
			e.Resource = r.Key
		case "cluster":
			if limited || !t.clusters[e.TargetID] && !t.clusters[e.Cluster] {
				continue
			}
		default:
			continue
		}
		out = append(out, e)
	}
	return out
}
