package fusion

import (
	"fmt"
	"sync"
	"time"

	"ikhnos/internal/fusionapi"
	"ikhnos/internal/kernel"
	"ikhnos/internal/store"
)

// FUSION as part of the server. The server's own Helm chart carries FUSION - the three stores and a central gateway -
// standing by at zero replicas. FusionControl is the switch (control_switch.go): it scales those workloads between 0
// and 1, and before the first start gives the gateway the certificates it needs. It does no rendering and installs
// nothing, so the permission it needs is the small Role in deploy/helm/ikhnos-server/templates/fusion-rbac.yaml (see
// KubeAPI).
//
// The gateway is also a regional operator in this server's own list (CentralOperatorID): the one door into FUSION.
// Other regional operators send to it over mTLS, with a client certificate this server issues from the gateway's own
// CA, and it sends on to the three stores. The stores are not exposed outside the cluster; only the gateway may be.

// fusionStore is one of the four workloads the switch drives.
type fusionStore struct {
	Component Component
	Label     string // what the UI calls it
	Kind      string // statefulsets | deployments
	Name      string
	// Optional is a workload an install may not have (Grafana: turned off in the chart, or an older release): its absence
	// is not an error, it is simply not part of FUSION there.
	Optional bool
}

// FusionControl switches the bundled FUSION on and off. Name is the chart's own object-name prefix (what the chart
// computes as "fusion.name"), passed as --fusion-name so the server and the chart cannot disagree.
type FusionControl struct {
	Name          string
	Namespace     string
	PublicAddress string // host:port other clusters dial to reach the gateway ("" = reachable inside this cluster only)
	// ClusterUID is the UID of this server's own kube-system namespace, which the chart passes in (IKHNOS_CLUSTER_ID): the
	// cluster the gateway runs in is the server's, so the agent whose cluster has this identity is the one that reports it.
	ClusterUID string
	Kube       kernel.KubeAPI
	// Data reads what FUSION saved (the shared API, internal/fusionhttp). Nil means the stores' in-cluster addresses by the
	// chart's naming convention (see dataClient); tests point it at fakes.
	Data *fusionapi.Client
	// Org is the one organisation that may turn FUSION on: everything saved in it is shared by whoever sends to the
	// gateway, so it belongs to a single organisation.
	Org string
	Now func() time.Time
	// PrometheusUI and GrafanaUI are where the two pages are reached when it is not their Services (tests point them at fakes).
	PrometheusUI, GrafanaUI string

	addrMu sync.RWMutex // guards PublicAddress, which the admin can change while the server runs (SetPublicAddress)

	// StatusTTL is how long a read of the workloads is reused (0 = fusionStatusTTL, negative = never; tests that change
	// the fake cluster between calls turn the cache off).
	StatusTTL time.Duration

	cacheMu sync.Mutex
	cache   statusCache

	// lastData is the newest sample time seen in Prometheus, and when it was last asked (see lastDataAt).
	lastDataMu    sync.Mutex
	lastDataSeen  time.Time
	lastDataTried time.Time
	// lastLogSeen and lastTraceSeen are the same for the logs and the traces store, asked at the same time.
	lastLogSeen   time.Time
	lastTraceSeen time.Time
	// traffic is what the gateway's counters said when they were last read (nil: they said nothing, or the read failed: a figure
	// that is not current is not kept the way the times above are).
	traffic *fusionapi.GatewayTraffic

	discoverMu sync.Mutex
	discoverAt time.Time // when the Service was last looked at for an address (see DiscoverAddress)

	// svc is the gateway's Service as it was last read (nil: unknown - not readable, or not asked yet), and svcAt when.
	svcMu sync.Mutex
	svc   *kernel.KubeService
	svcAt time.Time

	// switchMu serialises the calls that change the cluster: Enable, Disable and Renew. Each is a read, a few writes and
	// a report (Enable: certificate, Secret, then four scale calls), and two of them interleaving - an admin double
	// clicking, or the daily renewal landing on an Enable - could leave the stores half up and half down, or the gateway
	// started with a certificate that was already replaced. One at a time, in the order they arrive.
	switchMu sync.Mutex

	// retention caches the last measurement of the stores' disk use (see measure).
	retention retentionCache

	mu sync.Mutex
	// issuedFor is the host list the gateway's certificate was last issued for by this process ("" before it has issued one).
	issuedFor string
	data      *fusionapi.Client
	since     time.Time // when the stores were last asked to start; zero when off or unknown
	// notReady is when the stores were first seen not all ready, zero while they are (or FUSION is off). The grace
	// period counts from here, not from `since`: a pod that restarts a month into a healthy run is "starting"
	// again, not "attention after 43,200 minutes".
	notReady time.Time
}

// Clock is the time as this control reads it: the Now it was given, or the wall clock.
func (f *FusionControl) Clock() time.Time {
	if f.Now != nil {
		return f.Now()
	}
	return time.Now()
}

func (f *FusionControl) stores() []fusionStore {
	return []fusionStore{
		{"metrics", "Prometheus", "statefulsets", f.Name + "-prometheus", false},
		{"logs", "Loki", "statefulsets", f.Name + "-loki", false},
		{"traces", "Tempo", "statefulsets", f.Name + "-tempo", false},
		{"grafana", "Grafana", "statefulsets", f.Name + "-grafana", true},
		{"central", "Central operator", "deployments", f.Name + "-central", false},
	}
}

// dataClient is how the shared API reaches the three stores: their ClusterIP Services, plain HTTP, on the ports the
// chart gives them (see fusionRoutes, which uses the same convention for the OTLP side).
func (f *FusionControl) DataClient() *fusionapi.Client {
	if f.Data != nil {
		return f.Data
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.data == nil {
		host := func(suffix string, port int) string {
			return fmt.Sprintf("http://%s-%s.%s.svc:%d", f.Name, suffix, f.Namespace, port)
		}
		f.data = &fusionapi.Client{Prometheus: host("prometheus", 9090), Loki: host("loki", 3100), Tempo: host("tempo", 3200)}
	}
	return f.data
}

func (f *FusionControl) TLSSecretName() string { return f.Name + "-central-receiver-tls" }

// destination is the central operator's own destination: the three stores, by the chart's naming convention.
func (f *FusionControl) destination() store.Destination {
	return store.Destination{Kind: store.DestinationFusion, FusionRelease: f.Name, FusionNamespace: f.Namespace}
}
