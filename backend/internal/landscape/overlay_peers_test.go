package landscape

import (
	"testing"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
	"ikhnos/internal/facts"
	"ikhnos/internal/overlay"
	"ikhnos/internal/store"
)

func stateWith(nodes ...*ikhnosv1.NodeFacts) *facts.State {
	st := facts.New()
	for _, n := range nodes {
		st.Nodes[n.Key] = n
	}
	return st
}

func tsNode(name, addr string) *ikhnosv1.NodeFacts {
	return &ikhnosv1.NodeFacts{Key: name, Name: name, Probe: &ikhnosv1.HostProbe{HostAddresses: []*ikhnosv1.HostAddress{{Interface: "tailscale0", Kind: "tun", Address: addr}}}}
}

func TestANodesInterfacesAndAddressesReachTheOverlayReading(t *testing.T) {
	n := tsNode("a", "100.64.12.4/32")
	n.InternalIps = []string{"10.0.0.4"}
	o := overlay.Detect(overlayNodes(stateWith(n)))
	if o == nil || o.Product != "tailscale" || o.Nodes != 1 {
		t.Fatalf("%+v", o)
	}
	if overlay.Detect(overlayNodes(stateWith(&ikhnosv1.NodeFacts{Key: "b", Name: "b", InternalIps: []string{"10.0.0.5"}}))) != nil {
		t.Fatal("a node with a private address and no overlay interface says nothing")
	}
}

func TestPeersAreTheOtherApprovedClustersOnTheSameOverlayAndNeverNil(t *testing.T) {
	ok := string(store.StatusApproved)
	read := func(n *ikhnosv1.NodeFacts) *overlay.Overlay { return overlay.Detect(overlayNodes(stateWith(n))) }
	docs := []AgentDoc{
		{ClusterID: "c-a", Status: ok, Overlay: read(tsNode("a", "100.64.12.4/32"))},
		{ClusterID: "c-b", Status: ok, Overlay: read(tsNode("b", "100.101.3.2/32"))},
		{ClusterID: "c-c", Status: ok, Overlay: read(&ikhnosv1.NodeFacts{Key: "c", Name: "c", InternalIps: []string{"100.64.9.9"}})},
		{ClusterID: "c-d", Status: ok},
		{ClusterID: "c-e", Status: string(store.StatusRevoked), Overlay: read(tsNode("e", "100.64.1.1/32"))},
	}
	got := overlayPeers(docs, 0)
	if len(got) != 2 || got[0] != (overlay.Peer{ClusterID: "c-b"}) || got[1] != (overlay.Peer{ClusterID: "c-c", Maybe: true}) {
		t.Fatalf("%+v", got)
	}
	if p := overlayPeers(docs, 3); p == nil || len(p) != 0 {
		t.Fatalf("a cluster with no overlay has no peers, and the list is not null: %#v", p)
	}
}
