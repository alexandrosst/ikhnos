package fusionapi

import "time"

// NodeLinkGraph is a topology as a directed multigraph in the node-link form that NetworkX (node_link_graph), D3, Cytoscape converters and
// jq read as they are: nodes with an id, links with a source, a target and a key. It is built from an answer already cut to what the caller
// may see, so it cannot show more than that answer does, and it carries what the answer carries: nothing is left out but the telemetry
// pointers (use the JSON for those).
type NodeLinkGraph struct {
	Directed   bool           `json:"directed"`
	Multigraph bool           `json:"multigraph"`
	Graph      NodeLinkMeta   `json:"graph"`
	Nodes      []NodeLinkNode `json:"nodes"`
	Links      []NodeLinkEdge `json:"links"`
}

// NodeLinkMeta describes the graph as a whole.
type NodeLinkMeta struct {
	Application AppInfo      `json:"application"`
	AsOf        time.Time    `json:"asOf"`
	Source      string       `json:"source"` // live | history
	SnapshotAt  *time.Time   `json:"snapshotAt,omitempty"`
	Truncated   AppTruncated `json:"truncated"`
	// Edge says which way a link points; Units says what its figures are.
	Edge  string            `json:"edge"`
	Units map[string]string `json:"units"`
}

// NodeLinkNode is a service of the application, a service outside it that has traffic with it, or an address outside the clusters. The id
// is "service:<key>" or "external:<id>", so the two cannot be mistaken for each other.
type NodeLinkNode struct {
	ID        string `json:"id"`
	Type      string `json:"type"` // service | external
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Cluster   string `json:"cluster,omitempty"`
	Kind      string `json:"kind,omitempty"`
	// Member is whether the service belongs to the application.
	Member   bool   `json:"member"`
	Status   string `json:"status,omitempty"`
	Replicas *int   `json:"replicas,omitempty"`
	Ready    *int   `json:"readyReplicas,omitempty"`
	Port     int    `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

// NodeLinkEdge is traffic seen from a source (the client) to a target (the server). Key is the id of the dependency, which the changes
// of kind dependency name, and which tells two parallel links apart.
type NodeLinkEdge struct {
	Source       string      `json:"source"`
	Target       string      `json:"target"`
	Key          string      `json:"key,omitempty"`
	Direction    string      `json:"direction"` // internal | outbound | inbound, as seen from the application
	Port         int         `json:"port,omitempty"`
	Protocol     string      `json:"protocol,omitempty"`
	Confidence   string      `json:"confidence,omitempty"`
	Stale        bool        `json:"stale,omitempty"`
	CrossCluster bool        `json:"crossCluster,omitempty"`
	Traffic      LinkTraffic `json:"traffic"`
}

// NodeLink is the topology as a NodeLinkGraph.
func (t *AppTopology) NodeLink() NodeLinkGraph {
	g := NodeLinkGraph{Directed: true, Multigraph: true, Nodes: []NodeLinkNode{}, Links: []NodeLinkEdge{},
		Graph: NodeLinkMeta{Application: t.Application, AsOf: t.AsOf, Source: t.Source, SnapshotAt: t.SnapshotAt, Truncated: t.Truncated,
			Edge: "source is the client, target the server",
			Units: map[string]string{"bytes": "cumulative bytes", "connections": "cumulative connections", "bytesPerSec": "bytes per second",
				"connectionsPerMin": "connections per minute", "rttMs": "milliseconds", "retransmitsPerMin": "retransmits per minute"}}}
	have := map[string]bool{}
	add := func(n NodeLinkNode) {
		if !have[n.ID] {
			have[n.ID] = true
			g.Nodes = append(g.Nodes, n)
		}
	}
	for _, s := range t.Services {
		r, ok := s.Replicas, s.ID != ""
		var rp, rd *int
		if ok { // a service the topology does not know has no replicas to report
			rd = new(int)
			*rd = s.Ready
			rp = &r
		}
		add(NodeLinkNode{ID: "service:" + s.Key, Type: "service", Name: s.Name, Namespace: s.Namespace, Cluster: s.Cluster, Kind: s.Kind, Member: true,
			Status: s.Status, Replicas: rp, Ready: rd})
	}
	for _, o := range t.Neighbours {
		add(NodeLinkNode{ID: "service:" + o.Key, Type: "service", Name: o.Name, Namespace: o.Namespace, Cluster: o.Cluster})
	}
	for _, x := range t.Externals {
		add(NodeLinkNode{ID: "external:" + x.ID, Type: "external", Name: x.Name, Port: x.Port, Protocol: x.Protocol})
	}
	end := func(key, kind string) string {
		if kind == "external" {
			return "external:" + key
		}
		return "service:" + key
	}
	for _, l := range t.Links {
		from, to := end(l.From, l.FromKind), end(l.To, l.ToKind)
		for _, id := range []string{from, to} {
			if !have[id] { // an end that was cut from the answer is still named, so that no link points at nothing
				typ, name := "service", id[len("service:"):]
				if id[0] == 'e' {
					typ, name = "external", id[len("external:"):]
				}
				add(NodeLinkNode{ID: id, Type: typ, Name: name})
			}
		}
		g.Links = append(g.Links, NodeLinkEdge{Source: from, Target: to, Key: l.ID, Direction: l.Direction, Port: l.Port, Protocol: l.Protocol,
			Confidence: l.Confidence, Stale: l.Stale, CrossCluster: l.CrossCluster, Traffic: l.Traffic})
	}
	return g
}

// Formats a topology can be asked for with format=: the JSON of the route, or a node-link graph.
const (
	FormatJSON     = "json"
	FormatNodeLink = "nodelink"
)

// ParseFormat reads format= (empty is json).
func ParseFormat(v string) (string, error) {
	switch v {
	case "", FormatJSON:
		return FormatJSON, nil
	case FormatNodeLink:
		return FormatNodeLink, nil
	}
	return "", badRequest("format must be %s or %s", FormatJSON, FormatNodeLink)
}
