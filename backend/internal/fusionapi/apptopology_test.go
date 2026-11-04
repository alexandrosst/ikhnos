package fusionapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func shopTopology() (*AppGroup, *TopologyView) {
	g := &AppGroup{ID: "app-1", Name: "Shop", Origin: "manual", Members: []AppMember{
		{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment", Aliases: []string{"cart-app"}},
		{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Kind: "StatefulSet"},
		{ID: "by-hand", Name: "billing", Namespace: "shop", Cluster: "cl-1"}, // written in by hand: the topology has it under another id
	}}
	v := &TopologyView{At: time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC),
		Services: []TopoService{
			{ID: "s-cart", Name: "cart", Namespace: "shop", Cluster: "cl-1", Kind: "Deployment", Image: "cart:1", Replicas: 3, Ready: 3, Restarts: 2},
			{ID: "s-web", Name: "web", Namespace: "shop", Cluster: "cl-1", Replicas: 2, Ready: 1, Restarts: 5},
			{ID: "s-billing", Name: "billing", Namespace: "shop", Cluster: "cl-1", Replicas: 1, Ready: 1},
			{ID: "s-pay", Name: "pay", Namespace: "pay", Cluster: "cl-2", Applications: []string{"Pay"}},
			{ID: "s-db", Name: "ledger", Namespace: "secret", Cluster: "cl-1"},
			{ID: "s-dns", Name: "coredns", Namespace: "kube-system", Cluster: "cl-1"},
		},
		Links: []TopoLink{
			{ID: "d1", From: "s-web", To: "s-cart", FromKind: "service", ToKind: "service", Protocol: "tcp", Port: 8080, Traffic: LinkTraffic{Bytes: 500}},
			{ID: "d2", From: "s-cart", To: "s-billing", FromKind: "service", ToKind: "service", Stale: true},
			{ID: "d3", From: "s-cart", To: "s-pay", FromKind: "service", ToKind: "service", CrossCluster: true, Traffic: LinkTraffic{Bytes: 900, RttMs: 12}},
			{ID: "d4", From: "s-cart", To: "s-db", FromKind: "service", ToKind: "service"},
			{ID: "d5", From: "s-cart", To: "x-1", FromKind: "service", ToKind: "external", Port: 443},
			{ID: "d6", From: "s-cart", To: "s-dns", FromKind: "service", ToKind: "service", Noise: "dns"},
			{ID: "d7", From: "s-pay", To: "s-web", FromKind: "service", ToKind: "service"},
			{ID: "d8", From: "s-pay", To: "s-db", FromKind: "service", ToKind: "service"}, // nothing to do with the application
		},
		Externals: map[string]string{"x-1": "GitHub"}}
	return g, v
}

func TestTheTopologyOfAnApplicationIsItsServicesTheirLinksAndNeighbours(t *testing.T) {
	g, v := shopTopology()
	got := BuildAppTopology(g, v, AllSignals(), false)
	if got.Source != "live" || !got.AsOf.Equal(v.At) || got.Application.Origin != "manual" {
		t.Fatalf("%+v", got)
	}
	var keys []string
	for _, s := range got.Services {
		keys = append(keys, s.Key)
	}
	if strings.Join(keys, " ") != "billing/shop/cl-1 cart/shop/cl-1 web/shop/cl-1" {
		t.Fatalf("services %v", keys)
	}
	cart := got.Services[1]
	if cart.ID != "s-cart" || cart.Image != "cart:1" || cart.Restarts != 2 || cart.Telemetry.Member != "cart/shop/cl-1" ||
		strings.Join(cart.Telemetry.Filter.ServiceName, ",") != "cart,cart-app" || cart.Telemetry.Filter.Workload["k8s_deployment_name"] != "cart" || cart.Telemetry.Filter.Cluster != "cl-1" {
		t.Fatalf("%+v", cart)
	}
	if got.Services[2].Telemetry.Filter.Workload["k8s_statefulset_name"] != "web" {
		t.Fatalf("%+v", got.Services[2])
	}
	if got.Services[0].ID != "s-billing" { // the member written by hand is the topology's service of that key
		t.Fatalf("%+v", got.Services[0])
	}

	// internal, outbound and inbound; the machinery and the unrelated stay out; the busiest live link first, the stale one last
	var links []string
	for _, l := range got.Links {
		links = append(links, l.Direction+":"+l.From+">"+l.To)
	}
	want := "outbound:cart/shop/cl-1>pay/pay/cl-2 internal:web/shop/cl-1>cart/shop/cl-1 outbound:cart/shop/cl-1>ledger/secret/cl-1 outbound:cart/shop/cl-1>x-1 inbound:pay/pay/cl-2>web/shop/cl-1 internal:cart/shop/cl-1>billing/shop/cl-1"
	if strings.Join(links, " ") != want {
		t.Fatalf("links\n%v\nwant\n%s", strings.Join(links, "\n"), want)
	}
	if !got.Links[0].CrossCluster || got.Links[0].Traffic.RttMs != 12 || got.Links[1].Port != 8080 || got.Health.StaleLinks != 1 {
		t.Fatalf("%+v %+v", got.Links[:2], got.Health)
	}
	if len(got.Neighbours) != 2 || got.Neighbours[0].Key != "ledger/secret/cl-1" || got.Neighbours[1].Key != "pay/pay/cl-2" || got.Neighbours[1].Applications[0] != "Pay" || got.Neighbours[0].Applications == nil {
		t.Fatalf("%+v", got.Neighbours)
	}
	if len(got.Externals) != 1 || got.Externals[0].Name != "GitHub" || got.Externals[0].Port != 443 || strings.Join(got.Externals[0].CalledBy, ",") != "cart/shop/cl-1" {
		t.Fatalf("%+v", got.Externals)
	}
	// one web replica of two is not ready, and the restarts add up
	if h := got.Health; h.Status != "degraded" || h.Services.Total != 3 || h.Services.Healthy != 2 || h.Services.Degraded != 1 || h.Restarts != 7 || !strings.Contains(h.Reason, "web") {
		t.Fatalf("%+v", h)
	}
	// events are about the application, its services and the links shown
	if strings.Join(got.Targets(), " ") != "app-1 s-cart s-web s-billing d1 d2 d3 d4 d5 d7" {
		t.Fatalf("targets %v", got.Targets())
	}
	// the machinery is there when asked for
	if n := len(BuildAppTopology(g, v, AllSignals(), true).Links); n != 7 {
		t.Fatalf("%d links with noise", n)
	}
}

func TestTheTopologyOfAnApplicationShowsAScopedCallerOnlyWhatItMaySee(t *testing.T) {
	g, v := shopTopology()
	got := BuildAppTopology(g, v, Scope{Signals: Signals, Namespaces: []string{"shop"}}, false)
	for _, l := range got.Links {
		if strings.Contains(l.From+l.To, "pay") || strings.Contains(l.From+l.To, "ledger") || l.ToKind == "external" {
			t.Errorf("a link the caller may not see: %+v", l)
		}
	}
	if len(got.Links) != 2 || len(got.Neighbours) != 0 || len(got.Externals) != 0 {
		t.Fatalf("%+v %+v %+v", got.Links, got.Neighbours, got.Externals)
	}
	for _, id := range got.Targets() {
		if id == "d3" || id == "d4" || id == "d5" {
			t.Errorf("asks for the events of %s, which the caller may not see", id)
		}
	}
}

func TestTheTopologyOfAnApplicationSaysWhatItCutAndWhatItDoesNotKnow(t *testing.T) {
	g, v := shopTopology()
	v.Services, v.Links = v.Services[:2], nil
	for i := 0; i < maxAppExternals+1; i++ {
		x := fmt.Sprintf("x%d", i)
		v.Links = append(v.Links, TopoLink{ID: "a" + x, From: "s-cart", To: x, FromKind: "service", ToKind: "external"}) // (ids that sort first: equal traffic is ordered by id)
	}
	for i := 0; i < maxAppLinks-maxAppExternals; i++ { // one link more than an answer carries
		id := fmt.Sprintf("n%d", i)
		v.Services = append(v.Services, TopoService{ID: id, Name: id, Namespace: "x", Cluster: "cl-1"})
		v.Links = append(v.Links, TopoLink{ID: "l" + id, From: "s-cart", To: id, FromKind: "service", ToKind: "service"})
	}
	got := BuildAppTopology(g, v, AllSignals(), false)
	tr := got.Truncated
	if !tr.Links || !tr.Neighbours || !tr.Externals || tr.Changes || len(got.Links) != maxAppLinks || len(got.Neighbours) != maxAppNeighbours || len(got.Externals) != maxAppExternals {
		t.Fatalf("%+v %d %d %d", tr, len(got.Links), len(got.Neighbours), len(got.Externals))
	}
	evs := make([]ChangeEvent, maxAppChanges+1)
	for i := range evs {
		evs[i] = ChangeEvent{Time: v.At.Add(time.Duration(i) * time.Minute), Kind: "service-scaled"}
	}
	got.AddChanges(evs)
	if !got.Truncated.Changes || len(got.Changes) != maxAppChanges || !got.Changes[0].Time.After(got.Changes[1].Time) {
		t.Fatalf("%+v", got.Truncated)
	}

	// Members the topology does not have are listed, and unknown; an application with none of its services there is unknown
	g.Unresolved = 2
	got = BuildAppTopology(g, &TopologyView{}, AllSignals(), false)
	if got.Health.Status != "unknown" || got.Health.Services.Unknown != 3 || got.Unresolved != 2 || len(got.Services) != 3 || got.Services[0].Status != "" {
		t.Fatalf("%+v", got)
	}
	if got := BuildAppTopology(&AppGroup{ID: "a", Name: "Empty"}, &TopologyView{}, AllSignals(), false); got.Health.Status != "unknown" || got.Services == nil || got.Links == nil || got.Changes == nil {
		t.Fatalf("%+v", got)
	}
	// all of them down
	g, v = shopTopology()
	for i := range v.Services {
		v.Services[i].Ready = 0
	}
	if h := BuildAppTopology(g, v, AllSignals(), false).Health; h.Status != "down" {
		t.Fatalf("%+v", h)
	}
}

// Two services of different kinds can share a key, so two links can read the same; the id of the dependency tells them apart and
// is what the changes name, and the order does not depend on the order the links were read in.
func TestTheLinksOfAnApplicationCarryTheirIdAndComeInTheSameOrder(t *testing.T) {
	g, v := shopTopology()
	got := BuildAppTopology(g, v, AllSignals(), false)
	for _, l := range got.Links {
		if l.ID == "" {
			t.Fatalf("a link with no id: %+v", l)
		}
	}
	rev := *v
	rev.Links = append([]TopoLink(nil), v.Links...)
	for i, j := 0, len(rev.Links)-1; i < j; i, j = i+1, j-1 {
		rev.Links[i], rev.Links[j] = rev.Links[j], rev.Links[i]
	}
	again := BuildAppTopology(g, &rev, AllSignals(), false)
	a, _ := json.Marshal(got.Links)
	b, _ := json.Marshal(again.Links)
	if string(a) != string(b) {
		t.Fatalf("the order depends on how the links were read:\n%s\n%s", a, b)
	}
}

// The node-link form is the same topology as a graph other tools read as it is: every link points at nodes that exist, the ids tell a
// service from an address outside the clusters, a limited caller's graph holds only what its topology does, and it marshals as the
// node-link shape (nodes with an id, links with source, target and key).
func TestTheTopologyAsANodeLinkGraphHoldsWhatTheTopologyDoes(t *testing.T) {
	g, v := shopTopology()
	for name, scope := range map[string]Scope{"no limit": AllSignals(), "shop only": {Signals: Signals, Namespaces: []string{"shop"}}} {
		topo := BuildAppTopology(g, v, scope, false)
		ng := topo.NodeLink()
		ids := map[string]bool{}
		for _, n := range ng.Nodes {
			if ids[n.ID] {
				t.Errorf("%s: node %s twice", name, n.ID)
			}
			ids[n.ID] = true
		}
		if !ng.Directed || !ng.Multigraph || len(ng.Links) != len(topo.Links) {
			t.Fatalf("%s: %+v", name, ng)
		}
		for _, l := range ng.Links {
			if !ids[l.Source] || !ids[l.Target] || l.Key == "" {
				t.Errorf("%s: a link points at nothing or has no key: %+v", name, l)
			}
		}
		for _, s := range topo.Services {
			if !ids["service:"+s.Key] {
				t.Errorf("%s: service %s is not a node", name, s.Key)
			}
		}
		if name == "shop only" {
			for id := range ids {
				if strings.Contains(id, "pay") || strings.Contains(id, "ledger") || strings.HasPrefix(id, "external:") {
					t.Errorf("a limited caller's graph holds %s", id)
				}
			}
		} else if !ids["external:x-1"] || !ids["service:pay/pay/cl-2"] {
			t.Errorf("an unrestricted graph lacks the external or the neighbour: %v", ids)
		}
	}
	raw, _ := json.Marshal(func() NodeLinkGraph {
		a, b := shopTopology()
		return BuildAppTopology(a, b, AllSignals(), false).NodeLink()
	}())
	var shape struct {
		Nodes []map[string]any `json:"nodes"`
		Links []map[string]any `json:"links"`
	}
	if err := json.Unmarshal(raw, &shape); err != nil || len(shape.Nodes) == 0 || shape.Links[0]["source"] == nil || shape.Links[0]["target"] == nil || shape.Nodes[0]["id"] == nil {
		t.Fatalf("not node-link: %s %v", raw, err)
	}
	if _, err := ParseFormat("graphml"); err == nil {
		t.Error("an unknown format is accepted")
	}
}
