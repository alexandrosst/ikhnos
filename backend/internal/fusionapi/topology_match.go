package fusionapi

import (
	"sort"
)

// matchService finds the topology service behind a resource by its name (failing that, its app label) and the namespace
// and cluster it carries. One that names no namespace or cluster matches by the rest only when that leaves a single service;
// with several it matches none, and ambiguous says so, rather than the match being whichever service came first. A resource
// that does carry its whole key can only fit the services of that key.
func (v *TopologyView) matchService(r *Resource) (sv *TopoService, ambiguous bool) {
	if r.Service == "" {
		return nil, false
	}
	var byName, byLabel []*TopoService
	for i := range v.Services {
		c := &v.Services[i]
		if (r.Namespace != "" && c.Namespace != r.Namespace) || (r.Cluster != "" && c.Cluster != r.Cluster) {
			continue
		}
		if c.Name == r.Service {
			byName = append(byName, c)
		} else if c.Labels["app"] == r.Service || c.Labels["app.kubernetes.io/name"] == r.Service {
			byLabel = append(byLabel, c)
		}
	}
	for _, found := range [][]*TopoService{byName, byLabel} { // the name wins over the label
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
	o := byID[id]
	return o != nil && s.NamespaceVisible(o.Namespace) && s.ClusterVisible(o.Cluster)
}

// topologyFor builds a resource's neighbours, leaving out what the Scope may not see.
func (v *TopologyView) topologyFor(s Scope, r *Resource) *ResourceTopology {
	sv, _ := v.matchService(r)
	if sv == nil {
		return nil
	}
	byID := v.byID()
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
	rt := &ResourceTopology{Service: sv, Calls: []Neighbour{}, CalledBy: []Neighbour{}}
	for _, l := range v.Links {
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
// not tell from several services.
func attachTopology(v *TopologyView, s Scope, looked []*Resource) (matched, ambiguous int) {
	for _, r := range looked {
		if rt := v.topologyFor(s, r); rt != nil {
			r.Topology = rt
			matched++
		} else if _, amb := v.matchService(r); amb {
			ambiguous++
		}
	}
	return matched, ambiguous
}
