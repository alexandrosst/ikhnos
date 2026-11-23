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
func touchedBy(ix *topoIndex, resources []*Resource, only string) touched {
	t := touched{map[string]*Resource{}, map[string]*Resource{}, map[string]*Resource{}, map[string]*Resource{}, map[string]bool{}, map[string]string{}}
	var v *TopologyView
	if ix != nil {
		v = ix.v
	}
	for _, r := range resources {
		if v != nil {
			if sv, _ := ix.matchService(r); sv != nil {
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
// traffic seen to and from them, of the applications they are in, of the nodes its pods ran on, and of its clusters (t is
// what the trace's resources touch: touchedBy, which also said which events to read). A Scope limited to namespaces sees
// only the events of services in them, and of links between what it may see.
func relevantChanges(evs []ChangeEvent, ix *topoIndex, s Scope, t touched) []ChangeEvent {
	var byID map[string]*TopoService
	var link map[string]TopoLink
	if ix != nil {
		byID, link = ix.byID, ix.link
	}
	var out []ChangeEvent
	for _, e := range evs {
		if !changeVisible(s, e, byID, link) {
			continue
		}
		switch e.TargetKind {
		case "service":
			r, ok := t.services[e.TargetID]
			if !ok {
				continue
			}
			e.Namespace, e.Resource = t.ns[e.TargetID], r.Key
		case "dependency":
			r, ok := t.deps[e.TargetID]
			if !ok {
				continue
			}
			e.Resource = r.Key
		case "application":
			r, ok := t.apps[e.TargetID]
			if !ok {
				continue
			}
			e.Resource = r.Key
		case "node":
			r := t.nodes[e.TargetID]
			if r == nil {
				r = t.nodes[e.Name]
			}
			if r == nil {
				continue
			}
			e.Resource = r.Key
		case "cluster":
			if !t.clusters[e.TargetID] && !t.clusters[e.Cluster] {
				continue
			}
		default:
			continue
		}
		out = append(out, e)
	}
	return out
}

// changeVisible is whether a caller with Scope s may see an event at all, by what the topology says its target is;
// relevantChanges and the events of an application's topology both ask it, so that the two cannot drift apart. byID and
// link are the topology's services and links by id.
//
// An event's cluster must be one the Scope may see. A service's event needs the service known to the topology, in a
// namespace and cluster the Scope may see (whatever cluster the event itself names); a link's event needs
// both its ends visible. An application's event says which services it gained and lost, some of which a limited caller
// may not see, so it is for an unrestricted caller only; a node's or a cluster's is about everything running there, so
// not for one limited to namespaces.
func changeVisible(s Scope, e ChangeEvent, byID map[string]*TopoService, link map[string]TopoLink) bool {
	if e.Cluster != "" && !s.ClusterVisible(e.Cluster) {
		return false
	}
	switch e.TargetKind {
	case "service":
		sv := byID[e.TargetID]
		return sv != nil && s.NamespaceVisible(sv.Namespace) && s.ClusterVisible(sv.Cluster)
	case "dependency":
		l, ok := link[e.TargetID]
		return ok && endVisible(s, byID, l.From, l.FromKind) && endVisible(s, byID, l.To, l.ToKind)
	case "application":
		return s.Unrestricted()
	case "node", "cluster":
		return len(s.Namespaces) == 0 && s.ClusterVisible(e.Cluster)
	}
	return false
}

// visibleChanges keeps the events a caller with Scope s may see, by changeVisible, against the topology v.
func visibleChanges(evs []ChangeEvent, v *TopologyView, s Scope) []ChangeEvent {
	if v == nil {
		return []ChangeEvent{}
	}
	byID, link := v.byID(), make(map[string]TopoLink, len(v.Links))
	for _, l := range v.Links {
		link[l.ID] = l
	}
	out := make([]ChangeEvent, 0, len(evs))
	for _, e := range evs {
		if changeVisible(s, e, byID, link) {
			out = append(out, e)
		}
	}
	return out
}
