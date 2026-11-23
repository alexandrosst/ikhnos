package fusionapi

import (
	"time"

	"ikhnos/internal/model"
)

// TopologyView is the part of the landscape the fused read joins: services and the traffic seen between them.
type TopologyView struct {
	At       time.Time
	Services []TopoService
	Nodes    []TopoNode
	// Clusters are the clusters of the topology, for the name a service's placement gives its cluster.
	Clusters []TopoCluster
	Links    []TopoLink
	// Externals names the addresses outside the clusters that services were seen talking to, by id.
	Externals map[string]string
}

// TopoNode is one node of the topology.
type TopoNode struct{ ID, Name, Cluster, Role, Zone string }

// TopoCluster is one cluster of the topology.
type TopoCluster struct{ ID, Name, Tier string }

// TopoService is one service of the topology.
type TopoService struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Cluster   string            `json:"cluster,omitempty"`
	Kind      string            `json:"kind,omitempty"` // Deployment, StatefulSet ...
	Image     string            `json:"image,omitempty"`
	Status    string            `json:"status,omitempty"`
	Replicas  int               `json:"replicas"`
	Ready     int               `json:"readyReplicas"`
	Restarts  int               `json:"restarts,omitempty"`
	Labels    map[string]string `json:"-"`
	// Applications names the ikhnos applications this service is in, AppIDs says which they are.
	Applications []string `json:"applications,omitempty"`
	AppIDs       []string `json:"-"`
	// NodeIDs are the nodes the service runs on, and PodsOnNode how many of its pods each one holds (empty below tier 2, where pods are not read).
	NodeIDs    []string       `json:"-"`
	PodsOnNode map[string]int `json:"-"`
}

// Key is the identity telemetry knows the service by.
func (s TopoService) Key() model.ServiceKey {
	return model.ServiceKey{Cluster: s.Cluster, Namespace: s.Namespace, Name: s.Name}
}

// TopoLink is traffic seen from one entity to another.
type TopoLink struct {
	ID               string // the dependency's id, which events about it carry
	From, To         string // topology ids
	FromKind, ToKind string // service | external
	Protocol         string
	Port             int
	Confidence       string
	Stale            bool
	Noise            string // dns | system: machinery rather than the applications' own
	CrossCluster     bool   // the two ends are in different clusters
	Traffic          LinkTraffic
}

// LinkTraffic is what was measured on a link, as of the topology it comes from. A zero is "not measured" as often as it is "none":
// only eBPF sees retransmits and round trips.
type LinkTraffic struct {
	Bytes             uint64  `json:"bytes"`
	Connections       uint64  `json:"connections"`
	BytesPerSec       float64 `json:"bytesPerSec"`
	ConnectionsPerMin float64 `json:"connectionsPerMin"`
	RttMs             float64 `json:"rttMs"`
	RetransmitsPerMin float64 `json:"retransmitsPerMin"`
}

// ChangeEvent is something that happened to part of the landscape.
type ChangeEvent struct {
	Time        time.Time `json:"time"`
	Kind        string    `json:"kind"`       // service-scaled, node-status, cluster-added ...
	TargetKind  string    `json:"targetKind"` // cluster | node | service | dependency | agent
	TargetID    string    `json:"targetId,omitempty"`
	Name        string    `json:"name,omitempty"`
	Cluster     string    `json:"cluster,omitempty"`
	ClusterName string    `json:"clusterName,omitempty"`
	Detail      string    `json:"detail,omitempty"`
	Cause       string    `json:"cause,omitempty"`
	Severity    string    `json:"severity,omitempty"`
	// Namespace is that of the service the event is about, when it is about one and the topology knows it.
	Namespace string `json:"namespace,omitempty"`
	// Resource is the key of the trace resource the event is about, when it is about one.
	Resource string `json:"resource,omitempty"`
}

// ResourceTopology is what ikhnos knows about the service behind a resource.
type ResourceTopology struct {
	// Service is the service as ikhnos sees it, matched by name, namespace and cluster.
	Service *TopoService `json:"service,omitempty"`
	// Calls are the services and external addresses it was seen calling; CalledBy those seen calling it.
	Calls    []Neighbour `json:"calls"`
	CalledBy []Neighbour `json:"calledBy"`
}

// Neighbour is one end of observed traffic.
type Neighbour struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Kind       string `json:"kind"` // service | external
	Namespace  string `json:"namespace,omitempty"`
	Cluster    string `json:"cluster,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       int    `json:"port,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Stale      bool   `json:"stale,omitempty"`
}

const (
	defaultChangesBefore = 30 * time.Minute
	maxChangesBefore     = 24 * time.Hour
	defaultMaxChanges    = 50
	hardMaxChanges       = 500
	maxNeighbours        = 50
)
