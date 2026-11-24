package fusionapi

import (
	"sort"
)

// topoIndex is a view's services and links by what a read's resources are matched and joined by. It is built once for all
// the resources of one read: each of them matched by a pass over every service and every link of the topology would
// cost a large organisation's topology for every resource of every trace.
type topoIndex struct {
	v    *TopologyView
	byID map[string]*TopoService
	// named and labelled are the services' indexes in v.Services by name, and by the app labels that are not their name
	// (each in the order of v.Services, so that a match is the one a pass over the services would find first).
	named, labelled map[string][]int
	// linksOf is the indexes in v.Links of the links that have a service at either end, by the service's id (in order).
	linksOf map[string][]int
	link    map[string]TopoLink // by the link's id
}

// newTopoIndex indexes a view; nil for no view.
func newTopoIndex(v *TopologyView) *topoIndex {
	if v == nil {
		return nil
	}
	ix := &topoIndex{v: v, byID: v.byID(), named: map[string][]int{}, labelled: map[string][]int{},
		linksOf: map[string][]int{}, link: make(map[string]TopoLink, len(v.Links))}
	for i := range v.Services {
		c := &v.Services[i]
		ix.named[c.Name] = append(ix.named[c.Name], i)
		for _, l := range uniqueInOrder([]string{c.Labels["app"], c.Labels["app.kubernetes.io/name"]}) {
			if l != "" && l != c.Name {
				ix.labelled[l] = append(ix.labelled[l], i)
			}
		}
	}
	for i, l := range v.Links {
		ix.linksOf[l.From] = append(ix.linksOf[l.From], i)
		if l.To != l.From {
			ix.linksOf[l.To] = append(ix.linksOf[l.To], i)
		}
		ix.link[l.ID] = l
	}
	return ix
}

// matchService finds the topology service behind a resource by its name (failing that, its app label) and the namespace
// and cluster it carries. One that names no namespace or cluster matches by the rest only when that leaves a single service;
// with several it matches none, and ambiguous says so, rather than the match being whichever service came first. A resource
// that does carry its whole key can only fit the services of that key.
func (v *TopologyView) matchService(r *Resource) (sv *TopoService, ambiguous bool) {
	return newTopoIndex(v).matchService(r)
}

func (ix *topoIndex) matchService(r *Resource) (sv *TopoService, ambiguous bool) {
	if r.Service == "" {
		return nil, false
	}
	fits := func(idx []int) (found []*TopoService) {
		for _, i := range idx {
			c := &ix.v.Services[i]
			if (r.Namespace != "" && c.Namespace != r.Namespace) || (r.Cluster != "" && c.Cluster != r.Cluster) {
				continue
			}
			found = append(found, c)
		}
		return found
	}
	// The name wins over the label.
	for _, found := range [][]*TopoService{fits(ix.named[r.Service]), fits(ix.labelled[r.Service])} {
		switch len(found) {
		case 0:
		case 1:
			return found[0], false
		default:
			return nil, true
		}
	}
	return nil, false
}

func (v *TopologyView) byID() map[string]*TopoService {
	m := make(map[string]*TopoService, len(v.Services))
	for i := range v.Services {
		m[v.Services[i].ID] = &v.Services[i]
	}
	return m
}

// endVisible says whether the Scope may see one end of a link: a service in a namespace and cluster it may see, or an address
// outside the clusters, which has neither to check and so is for an unrestricted Scope only.
func endVisible(s Scope, byID map[string]*TopoService, id, kind string) bool {
	if kind == "external" {
		return s.Unrestricted()
	}
	return s.serviceVisible(byID[id])
}

// topologyFor builds a resource's neighbours, leaving out what the Scope may not see. The service names only the
// applications the caller may know of (visible: VisibleGroups, see Scope.shownService).
func (ix *topoIndex) topologyFor(s Scope, r *Resource, visible []AppGroup) *ResourceTopology {
	sv, _ := ix.matchService(r)
	if sv == nil || (s.AppLimited && !s.serviceVisible(sv)) {
		// (A resource of the caller's applications by its workload's name may carry a service.name that is another service's
		// of the topology, one the caller may not see.)
		return nil
	}
	v, byID := ix.v, ix.byID
	neighbour := func(id, kind string, l TopoLink) (Neighbour, bool) {
		n := Neighbour{ID: id, Kind: kind, Protocol: l.Protocol, Port: l.Port, Confidence: l.Confidence, Stale: l.Stale}
		if kind == "external" {
			n.Name = v.Externals[id]
			if n.Name == "" {
				n.Name = id
			}
		} else if o := byID[id]; o != nil {
			n.Name, n.Namespace, n.Cluster = o.Name, o.Namespace, o.Cluster
		}
		return n, endVisible(s, byID, id, kind)
	}
	rt := &ResourceTopology{Service: s.shownService(sv, visible), Calls: []Neighbour{}, CalledBy: []Neighbour{}}
	for _, i := range ix.linksOf[sv.ID] { // (the links of every other service could match neither case below)
		l := v.Links[i]
		if l.Noise != "" {
			continue
		}
		switch {
		case l.From == sv.ID && len(rt.Calls) < maxNeighbours:
			if n, ok := neighbour(l.To, l.ToKind, l); ok {
				rt.Calls = append(rt.Calls, n)
			}
		case l.To == sv.ID && l.FromKind != "external" && len(rt.CalledBy) < maxNeighbours:
			if n, ok := neighbour(l.From, l.FromKind, l); ok {
				rt.CalledBy = append(rt.CalledBy, n)
			}
		}
	}
	byName := func(l []Neighbour) {
		sort.Slice(l, func(i, j int) bool {
			if l[i].Name != l[j].Name {
				return l[i].Name < l[j].Name
			}
			return l[i].ID < l[j].ID
		})
	}
	byName(rt.Calls)
	byName(rt.CalledBy)
	return rt
}

// attachTopology puts each resource's neighbours on it. Returns how many resources the topology knew, and how many it could
// not tell from several services. visible are the applications the caller may know of, the only ones the services name.
func attachTopology(ix *topoIndex, s Scope, looked []*Resource, visible []AppGroup) (matched, ambiguous int) {
	for _, r := range looked {
		if rt := ix.topologyFor(s, r, visible); rt != nil {
			r.Topology = rt
			matched++
		} else if _, amb := ix.matchService(r); amb {
			ambiguous++
		}
	}
	return matched, ambiguous
}
