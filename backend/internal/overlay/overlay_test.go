package overlay

import "testing"

func node(name string, ips []string, ifs ...Iface) Node {
	return Node{Name: name, IPs: ips, Ifaces: ifs}
}
func ts(addr string) Iface { return Iface{Name: "tailscale0", Kind: "tun", Addr: addr} }

func TestATailscaleInterfaceOnMostNodesIsSaidWithItsRange(t *testing.T) {
	o := Detect([]Node{
		node("a", nil, ts("100.64.12.4/32")),
		node("b", nil, ts("100.64.12.9/32")),
		node("c", nil, ts("100.64.12.19/32")),
		node("d", []string{"10.0.0.4"}),
	})
	if o == nil || o.Product != "tailscale" || o.Interface != "tailscale0" || o.Kind != "tun" {
		t.Fatalf("%+v", o)
	}
	if o.Range != "100.64.12.0/27" || o.Nodes != 3 || o.OfNodes != 4 {
		t.Fatalf("range %s on %d of %d", o.Range, o.Nodes, o.OfNodes)
	}
	if len(o.Why) != 2 {
		t.Fatalf("why: %v", o.Why)
	}
}

func TestNetBirdWireGuardAndTheOthersAreToldByTheirInterfaces(t *testing.T) {
	for _, c := range []struct{ name, kind, product string }{
		{"wt0", "wireguard", "netbird"},
		{"netbird", "wireguard", "netbird"},
		{"nebula1", "tun", "nebula"},
		{"ztabcdef12", "tun", "zerotier"},
		{"wg0", "wireguard", "wireguard"},
		{"mesh0", "wireguard", "wireguard"}, // a WireGuard link with a name of its own is still one
	} {
		o := Detect([]Node{node("a", nil, Iface{Name: c.name, Kind: c.kind, Addr: "10.50.0.2/24"})})
		if o == nil || o.Product != c.product {
			t.Errorf("%s/%s: %+v, want %s", c.name, c.kind, o, c.product)
		}
	}
}

// Calico, Cilium and Flannel encrypt the traffic between the nodes of one cluster with WireGuard. That is not a network that joins
// clusters, and it must not be offered as one.
func TestAWireGuardLinkOfTheClusterNetworkPluginIsNotAnOverlay(t *testing.T) {
	for _, name := range []string{"wireguard.cali", "cilium_wg0", "flannel-wg", "flannel.1", "cali1234abcd"} {
		if o := Detect([]Node{node("a", nil, Iface{Name: name, Kind: "wireguard", Addr: "10.244.1.0/32"})}); o != nil {
			t.Errorf("%s was taken for an overlay: %+v", name, o)
		}
	}
}

func TestAPlainTunnelAndLoopbackAreNotAnOverlay(t *testing.T) {
	if o := Detect([]Node{node("a", nil, Iface{Name: "tun0", Kind: "tun", Addr: "10.8.0.6/24"}, Iface{Name: "tailscale0", Kind: "tun", Addr: "127.0.0.1/8"})}); o != nil {
		t.Fatalf("%+v", o)
	}
}

func TestTheMostCommonProductWinsAndANamedOneBeatsAnUnnamedWireGuardOnATie(t *testing.T) {
	o := Detect([]Node{
		node("a", nil, ts("100.64.1.1/32"), Iface{Name: "wg0", Kind: "wireguard", Addr: "10.9.0.1/24"}),
		node("b", nil, Iface{Name: "wg0", Kind: "wireguard", Addr: "10.9.0.2/24"}),
	})
	if o.Product != "wireguard" || o.Nodes != 2 {
		t.Fatalf("more nodes on wg0: %+v", o)
	}
	o = Detect([]Node{node("a", nil, ts("100.64.1.1/32"), Iface{Name: "wg0", Kind: "wireguard", Addr: "10.9.0.1/24"})})
	if o.Product != "tailscale" {
		t.Fatalf("a tie goes to the named one: %+v", o)
	}
}

func TestWithoutTheProbeOnlyAddressesInTheSharedRangeAreSaid(t *testing.T) {
	o := Detect([]Node{node("a", []string{"100.64.7.2"}), node("b", []string{"100.64.7.3"}), node("c", []string{"10.0.0.1"})})
	if o == nil || o.Interface != "" || o.Product != "" || o.Nodes != 2 || o.OfNodes != 3 {
		t.Fatalf("%+v", o)
	}
	if Detect([]Node{node("a", []string{"10.0.0.1"})}) != nil {
		t.Fatal("private addresses say nothing about an overlay")
	}
	if Detect(nil) != nil {
		t.Fatal("no nodes")
	}
}

func TestTwoClustersOnTheSameProductAreSaidToBeOnTheSameNetwork(t *testing.T) {
	a := Detect([]Node{node("a", nil, ts("100.64.12.4/32"))})
	b := Detect([]Node{node("b", nil, ts("100.101.3.2/32"))})
	if same, _ := Relate(a, b); !same {
		t.Fatal("two Tailscales")
	}
	nb := Detect([]Node{node("c", nil, Iface{Name: "wt0", Kind: "wireguard", Addr: "100.64.9.9/32"})})
	if same, maybe := Relate(a, nb); same || maybe {
		t.Fatal("Tailscale against NetBird")
	}
}

func TestUnnamedNetworksNeedOverlappingAddresses(t *testing.T) {
	w := func(addr string) *Overlay {
		return Detect([]Node{node("x", nil, Iface{Name: "wg0", Kind: "wireguard", Addr: addr})})
	}
	if same, _ := Relate(w("10.9.0.1/24"), w("10.9.0.7/24")); !same {
		t.Fatal("the same range")
	}
	if same, _ := Relate(w("10.9.0.1/24"), w("10.70.0.7/24")); same {
		t.Fatal("different ranges")
	}
}

func TestAddressesAloneAgainstATailscaleAreAMaybeAndTwoOfThemAreNothing(t *testing.T) {
	a := Detect([]Node{node("a", nil, ts("100.64.12.4/32"))})
	b := Detect([]Node{node("b", []string{"100.64.7.2"})})
	if same, maybe := Relate(a, b); same || !maybe {
		t.Fatalf("same %v maybe %v", same, maybe)
	}
	if same, maybe := Relate(b, b); same || maybe {
		t.Fatal("two address-only readings say nothing")
	}
	if same, maybe := Relate(nil, a); same || maybe {
		t.Fatal("nothing")
	}
}

func TestTheCoveringPrefixHoldsEveryAddress(t *testing.T) {
	o := Detect([]Node{node("a", nil, ts("100.64.0.1/32")), node("b", nil, ts("100.127.255.254/32"))})
	if o.Range != "100.64.0.0/10" {
		t.Fatalf("%s", o.Range)
	}
	o = Detect([]Node{node("a", nil, ts("100.64.5.5/32"))})
	if o.Range != "100.64.5.5/32" {
		t.Fatalf("%s", o.Range)
	}
}
