package fusionapi

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"ikhnos/internal/model"
)

// The topology of one ikhnos application: its services, the traffic they have with each other and with everything else, and
// what changed around them. It is a set of pointers, not telemetry: each service says by which labels its metrics, logs and
// traces are found, and the caller reads them with `application=<id>` on any other route.

// Most of each part that one answer carries; the answer says when a part was cut (AppTruncated).
const (
	maxAppLinks      = 2000
	maxAppNeighbours = 200
	maxAppExternals  = 50
	maxAppChanges    = 100

	DefaultTopologyWindow = 24 * time.Hour
	MaxTopologyWindow     = 7 * 24 * time.Hour
)

// AppTopology is the answer.
type AppTopology struct {
	Application AppInfo        `json:"application"`
	AsOf        time.Time      `json:"asOf"`
	Source      string         `json:"source"` // live | history
	SnapshotAt  *time.Time     `json:"snapshotAt,omitempty"`
	Health      AppHealth      `json:"health"`
	Services    []AppTopoSvc   `json:"services"`
	Links       []AppTopoLink  `json:"links"`
	Neighbours  []AppTopoOther `json:"neighbours"`
	Externals   []AppTopoExt   `json:"externals"`
	Changes     []ChangeEvent  `json:"changes"`
	Unresolved  int            `json:"unresolvedMembers"`
	Truncated   AppTruncated   `json:"truncated"`

	ids []string // what events about the application are about, for Targets
}

// AppInfo is the application itself.
type AppInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Origin      string `json:"origin,omitempty"`
	Confidence  string `json:"confidence,omitempty"`
}

// AppHealth rolls the services up: healthy, degraded (some not ready), down (none ready), unknown (not in the topology).
type AppHealth struct {
	Status     string          `json:"status"`
	Reason     string          `json:"reason,omitempty"`
	Services   AppHealthCounts `json:"services"`
	Restarts   int             `json:"restarts"`
	StaleLinks int             `json:"staleLinks"`
}

// AppHealthCounts counts the services by state.
type AppHealthCounts struct {
	Total    int `json:"total"`
	Healthy  int `json:"healthy"`
	Degraded int `json:"degraded"`
	Down     int `json:"down"`
	Unknown  int `json:"unknown"`
}

// AppTopoSvc is a service of the application.
type AppTopoSvc struct {
	Key       string       `json:"key"`
	ID        string       `json:"id,omitempty"`
	Name      string       `json:"name"`
	Namespace string       `json:"namespace,omitempty"`
	Cluster   string       `json:"cluster,omitempty"`
	Kind      string       `json:"kind,omitempty"`
	Image     string       `json:"image,omitempty"`
	Status    string       `json:"status,omitempty"`
	Replicas  int          `json:"replicas"`
	Ready     int          `json:"readyReplicas"`
	Restarts  int          `json:"restarts"`
	Telemetry AppTelemetry `json:"telemetry"`
	// Placement is where the service runs, with `placement=true`; absent for a service the topology does not know.
	Placement *AppPlacement `json:"placement,omitempty"`
}

// AppPlacement is the cluster a service runs in and the nodes of it that hold its pods.
type AppPlacement struct {
	Cluster AppPlacementCluster `json:"cluster"`
	Nodes   []AppPlacementNode  `json:"nodes"`
}

// AppPlacementCluster is the cluster a service runs in.
type AppPlacementCluster struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Tier string `json:"tier,omitempty"`
}

// AppPlacementNode is a node that holds pods of a service. Pods is how many, when the topology reads pods (tier 2); otherwise it is 0 and left out.
type AppPlacementNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Role string `json:"role,omitempty"`
	Zone string `json:"zone,omitempty"`
	Pods int    `json:"pods,omitempty"`
}

// AppTelemetry says how to find a service's telemetry: Member is the `member` label of the application info series, Filter
// the labels the telemetry itself carries.
type AppTelemetry struct {
	Member string          `json:"member"`
	Filter AppTelemetryKey `json:"filter"`
}

// AppTelemetryKey is the label values that select a service's series: its service names (its name and aliases) within its
// namespace and cluster, and its workload for the metrics that name that instead.
type AppTelemetryKey struct {
	Cluster     string            `json:"ikhnos_cluster_id,omitempty"`
	Namespace   string            `json:"k8s_namespace_name,omitempty"`
	ServiceName []string          `json:"service_name"`
	Workload    map[string]string `json:"workload,omitempty"`
}

// AppTopoLink is traffic seen between two things, one at least a service of the application. A service is named by its key;
// an address outside the clusters by its id in `externals`.
type AppTopoLink struct {
	// ID is the id of the dependency, which the changes (kind dependency) and the other routes name it by. From and To are keys, and two
	// services of different kinds can share one, so two links can look the same without it.
	ID           string      `json:"id,omitempty"`
	From         string      `json:"from"`
	FromKind     string      `json:"fromKind"` // service | external
	To           string      `json:"to"`
	ToKind       string      `json:"toKind"`
	Direction    string      `json:"direction"` // internal | outbound | inbound
	Port         int         `json:"port,omitempty"`
	Protocol     string      `json:"protocol,omitempty"`
	Confidence   string      `json:"confidence,omitempty"`
	Stale        bool        `json:"stale,omitempty"`
	CrossCluster bool        `json:"crossCluster,omitempty"`
	Traffic      LinkTraffic `json:"traffic"`
}

// AppTopoOther is a service outside the application that has traffic with it.
type AppTopoOther struct {
	Key          string   `json:"key"`
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Namespace    string   `json:"namespace,omitempty"`
	Cluster      string   `json:"cluster,omitempty"`
	Applications []string `json:"applications"`
}

// AppTopoExt is an address outside the clusters that services of the application were seen talking to.
type AppTopoExt struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Port     int      `json:"port,omitempty"`
	Protocol string   `json:"protocol,omitempty"`
	CalledBy []string `json:"calledBy"`
}

// AppTruncated says which parts were cut at their maximum.
type AppTruncated struct {
	Links      bool `json:"links"`
	Neighbours bool `json:"neighbours"`
	Externals  bool `json:"externals"`
	Changes    bool `json:"changes"`
}

// BuildAppTopology works out the answer from the application and the topology (as of now or as of a past moment), for a caller
// with this Scope: a neighbour the Scope may not see is left out, with the link to it, and so are all addresses outside the
// clusters unless the Scope is unrestricted. Machinery links (dns, system) are left out unless noise is set. It is one pass
// over the services and one over the links. visible are the applications the caller may know of (VisibleGroups): a
// neighbour names only those of its applications (Scope.shownService).
func BuildAppTopology(g *AppGroup, v *TopologyView, s Scope, noise bool, visible []AppGroup) *AppTopology {
	t := &AppTopology{Application: AppInfo{ID: g.ID, Name: g.Name, Description: g.Description, Origin: g.Origin, Confidence: g.Confidence},
		AsOf: v.At, Source: "live", Services: []AppTopoSvc{}, Links: []AppTopoLink{}, Neighbours: []AppTopoOther{}, Externals: []AppTopoExt{},
		Changes: []ChangeEvent{}, Unresolved: g.Unresolved, ids: []string{g.ID}}
	byID := v.byID()
	byKey := map[model.ServiceKey]*TopoService{}
	for i := range v.Services {
		if k := v.Services[i].Key(); byKey[k] == nil {
			byKey[k] = &v.Services[i]
		}
	}

	member := map[string]string{} // topology service id -> key, for the services of the application
	var notReady []string
	for _, m := range g.Members {
		key := m.Key()
		svc := AppTopoSvc{Key: key.String(), ID: m.ID, Name: m.Name, Namespace: m.Namespace, Cluster: m.Cluster, Kind: m.Kind,
			Telemetry: AppTelemetry{Member: key.String(), Filter: AppTelemetryKey{Cluster: m.Cluster, Namespace: m.Namespace, ServiceName: append([]string{m.Name}, m.Aliases...)}}}
		if w := workloadLabel[m.Kind]; w != "" {
			svc.Telemetry.Filter.Workload = map[string]string{w: m.Name}
		}
		sv := byID[m.ID]
		if sv == nil {
			sv = byKey[key]
		}
		if s.AppLimited && !s.serviceVisible(sv) {
			// (A member is matched with the topology by its id for a caller limited to applications: a service that only has
			// its key is not taken for it.)
			sv = nil
		}
		h := &t.Health.Services
		h.Total++
		switch {
		case sv == nil || sv.Replicas == 0:
			h.Unknown++
		case sv.Ready == 0:
			h.Down++
			notReady = append(notReady, m.Name)
		case sv.Ready < sv.Replicas:
			h.Degraded++
			notReady = append(notReady, m.Name)
		default:
			h.Healthy++
		}
		if sv != nil {
			svc.ID, svc.Kind, svc.Image, svc.Status = sv.ID, sv.Kind, sv.Image, sv.Status
			svc.Replicas, svc.Ready, svc.Restarts = sv.Replicas, sv.Ready, sv.Restarts
			member[sv.ID] = svc.Key
			t.Health.Restarts += sv.Restarts
			t.ids = append(t.ids, sv.ID)
		}
		t.Services = append(t.Services, svc)
	}
	sort.Slice(t.Services, func(i, j int) bool { return t.Services[i].Key < t.Services[j].Key })
	t.Health.Status, t.Health.Reason = healthOf(t.Health.Services, notReady)

	// the links with a service of the application at one end or both, whose other end the Scope may see
	type cand struct {
		AppTopoLink
		own, other, otherKind string // the application's service (its key) and what is on the far side (its topology id), when the link leaves or enters it
	}
	keyOf := func(id string) string {
		if k, ok := member[id]; ok {
			return k
		}
		if o := byID[id]; o != nil {
			return o.Key().String()
		}
		return id // an address outside the clusters is named by its id
	}
	var cands []cand
	for _, l := range v.Links {
		if l.Noise != "" && !noise {
			continue
		}
		_, fromIn := member[l.From]
		_, toIn := member[l.To]
		fromIn, toIn = fromIn && l.FromKind != "external", toIn && l.ToKind != "external"
		c := cand{AppTopoLink: AppTopoLink{ID: l.ID, From: keyOf(l.From), FromKind: l.FromKind, To: keyOf(l.To), ToKind: l.ToKind, Direction: "internal", Port: l.Port, Protocol: l.Protocol,
			Confidence: l.Confidence, Stale: l.Stale, CrossCluster: l.CrossCluster, Traffic: l.Traffic}}
		switch {
		case fromIn && toIn:
		case fromIn:
			c.Direction, c.own, c.other, c.otherKind = "outbound", c.From, l.To, l.ToKind
		case toIn:
			c.Direction, c.own, c.other, c.otherKind = "inbound", c.To, l.From, l.FromKind
		default:
			continue
		}
		if c.other != "" && !endVisible(s, byID, c.other, c.otherKind) {
			continue
		}
		cands = append(cands, c)
		t.ids = append(t.ids, l.ID)
	}
	// live links first, the busiest first
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Stale != cands[j].Stale {
			return !cands[i].Stale
		}
		if cands[i].Traffic.Bytes != cands[j].Traffic.Bytes {
			return cands[i].Traffic.Bytes > cands[j].Traffic.Bytes
		}
		return cands[i].ID < cands[j].ID // the same answer every time, whatever order the links were read in
	})
	if t.Truncated.Links = len(cands) > maxAppLinks; t.Truncated.Links {
		cands = cands[:maxAppLinks]
	}

	// who is on the other side of those links
	near, far := map[string]bool{}, map[string]*AppTopoExt{}
	for _, c := range cands {
		t.Links = append(t.Links, c.AppTopoLink)
		if c.Stale {
			t.Health.StaleLinks++
		}
		if c.other == "" {
			continue
		}
		if o := byID[c.other]; o != nil {
			if !near[o.ID] {
				near[o.ID] = true
				t.Neighbours = append(t.Neighbours, AppTopoOther{Key: o.Key().String(), ID: o.ID, Name: o.Name, Namespace: o.Namespace, Cluster: o.Cluster,
					Applications: append([]string{}, s.shownService(o, visible).Applications...)})
			}
			continue
		}
		x := far[c.other]
		if x == nil {
			x = &AppTopoExt{ID: c.other, Name: v.Externals[c.other], Port: c.Port, Protocol: c.Protocol, CalledBy: []string{}}
			if x.Name == "" {
				x.Name = c.other
			}
			far[c.other] = x
		}
		if !slices.Contains(x.CalledBy, c.own) {
			x.CalledBy = append(x.CalledBy, c.own)
		}
	}
	for _, x := range far {
		t.Externals = append(t.Externals, *x)
	}
	sort.Slice(t.Neighbours, func(i, j int) bool { return t.Neighbours[i].Key < t.Neighbours[j].Key })
	sort.Slice(t.Externals, func(i, j int) bool {
		if t.Externals[i].Name != t.Externals[j].Name {
			return t.Externals[i].Name < t.Externals[j].Name
		}
		return t.Externals[i].ID < t.Externals[j].ID
	})
	if t.Truncated.Neighbours = len(t.Neighbours) > maxAppNeighbours; t.Truncated.Neighbours {
		t.Neighbours = t.Neighbours[:maxAppNeighbours]
	}
	if t.Truncated.Externals = len(t.Externals) > maxAppExternals; t.Truncated.Externals {
		t.Externals = t.Externals[:maxAppExternals]
	}
	return t
}

func healthOf(c AppHealthCounts, notReady []string) (status, reason string) {
	known := c.Total - c.Unknown
	switch {
	case c.Total == 0:
		return "unknown", "the application has no services"
	case known == 0:
		return "unknown", "none of its services is in the topology"
	case c.Down == known:
		return "down", "no service has a replica ready: " + names(notReady)
	case c.Down+c.Degraded > 0:
		return "degraded", fmt.Sprintf("%d of %d services are not fully ready: %s", c.Down+c.Degraded, known, names(notReady))
	case c.Unknown > 0:
		return "healthy", fmt.Sprintf("%d service(s) are not in the topology", c.Unknown)
	}
	return "healthy", ""
}

// names is a few of the names, sorted, for a message. It sorts l itself, which none of its callers reads again.
func names(l []string) string {
	sort.Strings(l)
	return strings.Join(firstOf(l, 3, func(int) string { return "..." }), ", ")
}

// Targets are the ids that events about the application are about: itself, its services and the links it shows.
func (t *AppTopology) Targets() []string { return t.ids }

// TargetsFor are the ids to read events about for a caller with Scope s. The application's own events say which services it
// gained and lost, by id and by name, and an application holds services a limited caller may not see (another namespace, another
// cluster), so a limited caller is given the events about the services and links it can see, and not the application's.
//
// The answer is never nil: a nil list asks the events for every target, an empty one for none, and a limited caller who can see no
// service or link of the application must be given none.
func (t *AppTopology) TargetsFor(s Scope) []string {
	switch {
	case len(t.ids) == 0:
		return []string{}
	case s.Unrestricted():
		return t.ids
	}
	return t.ids[1:] // ids[0] is the application itself (BuildAppTopology)
}

// MaxAppChanges is the most events one answer carries.
const MaxAppChanges = maxAppChanges

// AddChanges sets the events, newest first, and says when there were more than an answer carries.
func (t *AppTopology) AddChanges(evs []ChangeEvent) {
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Time.After(evs[j].Time) })
	if t.Truncated.Changes = len(evs) > maxAppChanges; t.Truncated.Changes {
		evs = evs[:maxAppChanges]
	}
	t.Changes = evs
}

// AddVisibleChanges sets the events a caller with Scope s may see, as AddChanges does. The events are read by the ids
// of the services and links the caller may see (TargetsFor), but what the store answers is checked once more against
// the topology v, with the same rule the events of a trace are kept by (changeVisible). There may be more when the
// store gave more than an answer carries, even if fewer than that are left.
func (t *AppTopology) AddVisibleChanges(evs []ChangeEvent, v *TopologyView, s Scope) {
	more := len(evs) > maxAppChanges
	t.AddChanges(visibleChanges(evs, v, s))
	t.Truncated.Changes = t.Truncated.Changes || more
}

// AddPlacement says, for each service of the application that the topology knows, which cluster it runs in and on which nodes, as the view
// has them. A node the topology does not know is not named (the same rule the service's own node list follows), so a service can
// have fewer nodes than replicas.
func (t *AppTopology) AddPlacement(v *TopologyView) {
	byID := v.byID()
	clusters := map[string]TopoCluster{}
	for _, c := range v.Clusters {
		clusters[c.ID] = c
	}
	nodes := map[string]TopoNode{}
	for _, n := range v.Nodes {
		nodes[n.ID] = n
	}
	for i := range t.Services {
		sv := byID[t.Services[i].ID]
		if sv == nil {
			continue
		}
		c := clusters[sv.Cluster]
		p := &AppPlacement{Cluster: AppPlacementCluster{ID: sv.Cluster, Name: c.Name, Tier: c.Tier}, Nodes: []AppPlacementNode{}}
		for _, id := range sv.NodeIDs {
			if n, ok := nodes[id]; ok {
				p.Nodes = append(p.Nodes, AppPlacementNode{ID: n.ID, Name: n.Name, Role: n.Role, Zone: n.Zone, Pods: sv.PodsOnNode[n.ID]})
			}
		}
		sort.Slice(p.Nodes, func(a, b int) bool {
			if p.Nodes[a].Name != p.Nodes[b].Name {
				return p.Nodes[a].Name < p.Nodes[b].Name
			}
			return p.Nodes[a].ID < p.Nodes[b].ID
		})
		t.Services[i].Placement = p
	}
}
