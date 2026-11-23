package server

import (
	"fmt"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/landscape"
	"ikhnos/internal/model"
)

// topologyView is the part of the state a fused read joins.
func topologyView(doc landscape.StateDoc) *fusionapi.TopologyView {
	topo := doc.Topology
	v := &fusionapi.TopologyView{Externals: map[string]string{}}
	if at, err := time.Parse(time.RFC3339, doc.GeneratedAt); err == nil {
		v.At = at
	}
	for _, s := range topo.Services {
		v.Services = append(v.Services, fusionapi.TopoService{ID: s.ID, Name: s.Name, Namespace: s.Namespace, Cluster: s.ClusterID,
			Kind: string(s.Kind), Image: s.Image, Status: string(s.Status), Replicas: int(s.Replicas), Ready: int(s.ReadyReplicas), Restarts: int(s.Restarts), Labels: s.Labels,
			NodeIDs: s.NodeIDs, PodsOnNode: podsOnNode(s.Pods)})
	}
	for _, n := range topo.Nodes {
		v.Nodes = append(v.Nodes, fusionapi.TopoNode{ID: n.ID, Name: n.Name, Cluster: n.ClusterID, Role: string(n.Role), Zone: n.Zone})
	}
	for _, c := range topo.Clusters {
		v.Clusters = append(v.Clusters, fusionapi.TopoCluster{ID: c.ID, Name: c.Name, Tier: string(c.Tier)})
	}
	for _, d := range topo.Dependencies {
		l := fusionapi.TopoLink{ID: d.ID, From: d.From, To: d.To, FromKind: string(d.FromKind), ToKind: string(d.ToKind), Protocol: d.Protocol,
			Port: d.Port, Confidence: string(d.Confidence), Stale: d.Stale, Noise: string(d.Noise), CrossCluster: d.CrossCluster,
			Traffic: fusionapi.LinkTraffic{Bytes: d.Bytes, Connections: d.Connections, RttMs: d.RttMs}}
		if d.Stats != nil {
			l.Traffic.BytesPerSec, l.Traffic.ConnectionsPerMin, l.Traffic.RetransmitsPerMin = d.Stats.BytesPerSec, d.Stats.ConnectionsPerMin, d.Stats.RetransmitsPerMin
		}
		v.Links = append(v.Links, l)
	}
	for _, x := range topo.ExternalEndpoints {
		name := x.Name
		if name == "" {
			name = x.Host
			if x.Port != 0 {
				name = fmt.Sprintf("%s:%d", x.Host, x.Port)
			}
		}
		v.Externals[x.ID] = name
	}
	return v
}

// podsOnNode counts a service's pods by the node they run on (a pod whose node the topology does not know has none to count).
func podsOnNode(pods []model.Pod) map[string]int {
	var m map[string]int
	for _, p := range pods {
		if p.NodeID == "" {
			continue
		}
		if m == nil {
			m = map[string]int{}
		}
		m[p.NodeID]++
	}
	return m
}
