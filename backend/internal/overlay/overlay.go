// Package overlay says, from what a cluster's nodes report about their own network interfaces, whether the cluster is on an overlay
// network (a VPN or mesh such as Tailscale, NetBird or WireGuard that gives each node an address of its own), which one it looks
// like, and which range its nodes' addresses fall in. It also says whether two clusters look to be on the same one.
//
// Everything here is read from facts already reported; nothing is probed. It is a reading of evidence, not a proof: that two
// clusters can reach each other over it is shown by the link measurement, never by this package. The words it uses for a product
// come from the interface's name, which a person can change, so callers present the product as what the interface looks like.
package overlay

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
)

// Iface is one address on one interface of a node, as the node probe reports it.
type Iface struct {
	Name string
	Kind string // the kernel link driver ("wireguard", "tun", ...), empty for a physical link
	Addr string // "ip/prefixlen" or a bare ip
}

// Node is what one node reports.
type Node struct {
	Name   string
	Ifaces []Iface  // from the node probe; empty when the probe is off
	IPs    []string // the addresses Kubernetes knows for the node
}

// Overlay is what a cluster's nodes say about the overlay network they are on.
type Overlay struct {
	// Product is what the interface looks like: tailscale, netbird, zerotier, nebula or wireguard (an unnamed one). Empty when only
	// addresses were seen, not an interface.
	Product string `json:"product,omitempty"`
	// Interface is the name most of those nodes give it ("tailscale0"); Kind is the kernel driver of it. Both are empty when only
	// addresses were seen.
	Interface string `json:"interface,omitempty"`
	Kind      string `json:"kind,omitempty"`
	// Range is the smallest prefix that holds the addresses seen on it.
	Range string `json:"range"`
	// Nodes is how many nodes are on it, of OfNodes in the cluster.
	Nodes   int `json:"nodes"`
	OfNodes int `json:"ofNodes"`
	// Why is what was read, as short sentences, for a person who asks.
	Why []string `json:"why"`
}

// Peer is another cluster that looks to be on the same overlay network, or may be.
type Peer struct {
	ClusterID string `json:"clusterId"`
	// Maybe is set when the evidence is one side's addresses only, in a range that carriers also use.
	Maybe bool `json:"maybe,omitempty"`
}

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// A name that belongs to the cluster's own network plugin or the machine's plumbing is never an overlay between clusters, even when
// its driver is WireGuard (Calico, Cilium and Flannel encrypt the traffic between the nodes of one cluster that way).
var plumbing = regexp.MustCompile(`^(cilium|cali|wireguard\.cali|vxlan\.calico|flannel|kube|antrea|weave|cni|docker|br-|veth|virbr|lo$)`)

// Names of the common overlay products' interfaces, in the order they are tried.
var products = []struct {
	name string
	re   *regexp.Regexp
}{
	{"tailscale", regexp.MustCompile(`^tailscale\d*$`)}, // Headscale's clients use the same name
	{"netbird", regexp.MustCompile(`^(wt\d+|netbird.*)$`)},
	{"zerotier", regexp.MustCompile(`^zt[0-9a-z]{6,}$`)},
	{"nebula", regexp.MustCompile(`^nebula\d*$`)},
	{"wireguard", regexp.MustCompile(`^wg\d*$`)},
}

// productOf names the product an interface looks like, or "".
func productOf(name, kind string) string {
	n := strings.ToLower(name)
	if plumbing.MatchString(n) {
		return ""
	}
	for _, p := range products {
		if p.re.MatchString(n) {
			return p.name
		}
	}
	if kind == "wireguard" {
		return "wireguard"
	}
	return ""
}

type seen struct {
	node string
	name string
	kind string
	pfx  netip.Prefix // the subnet the interface says it is on (a host prefix when it says none)
}

// Detect reads one cluster's nodes. It returns nil when there is nothing to say.
func Detect(nodes []Node) *Overlay {
	if len(nodes) == 0 {
		return nil
	}
	byProduct := map[string][]seen{}
	for _, n := range nodes {
		for _, f := range n.Ifaces {
			p := productOf(f.Name, f.Kind)
			if p == "" {
				continue
			}
			pf, ok := pfxOf(f.Addr)
			a := pf.Addr()
			if !ok || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsUnspecified() {
				continue
			}
			byProduct[p] = append(byProduct[p], seen{node: n.Name, name: f.Name, kind: f.Kind, pfx: pf})
		}
	}
	if best := mostNodes(byProduct); best != "" {
		return fromInterfaces(best, byProduct[best], len(nodes))
	}
	return fromAddresses(nodes)
}

// mostNodes is the product seen on the most nodes; a named product wins a tie over an unnamed WireGuard.
func mostNodes(by map[string][]seen) string {
	best, bestN := "", 0
	names := make([]string, 0, len(by))
	for p := range by {
		names = append(names, p)
	}
	sort.Strings(names)
	for _, p := range names {
		n := distinctNodes(by[p])
		if n > bestN || (n == bestN && best == "wireguard" && p != "wireguard") {
			best, bestN = p, n
		}
	}
	return best
}

func distinctNodes(ss []seen) int {
	m := map[string]bool{}
	for _, s := range ss {
		m[s.node] = true
	}
	return len(m)
}

func fromInterfaces(product string, ss []seen, of int) *Overlay {
	count := map[string]int{}
	kind := map[string]string{}
	var pfxs []netip.Prefix
	for _, s := range ss {
		count[s.name]++
		kind[s.name] = s.kind
		pfxs = append(pfxs, s.pfx)
	}
	name := ""
	for n, c := range count {
		if name == "" || c > count[name] || (c == count[name] && n < name) {
			name = n
		}
	}
	o := &Overlay{Product: product, Interface: name, Kind: kind[name], Nodes: distinctNodes(ss), OfNodes: of}
	if p, ok := cover(pfxs); ok {
		o.Range = p.String()
	}
	o.Why = []string{fmt.Sprintf("Interface %s on %d of %d nodes%s.", name, o.Nodes, of, driverWords(o.Kind))}
	if o.Range != "" {
		o.Why = append(o.Why, "Its addresses are inside "+o.Range+".")
	}
	return o
}

func driverWords(kind string) string {
	if kind == "" {
		return ""
	}
	return ", kernel driver " + kind
}

// fromAddresses is the case with no overlay interface seen: the node probe is off, or the nodes simply have none. Node addresses in
// the range Tailscale and NetBird hand out are worth saying, with what they do not prove.
func fromAddresses(nodes []Node) *Overlay {
	var pfxs []netip.Prefix
	on := 0
	for _, n := range nodes {
		hit := false
		for _, ip := range n.IPs {
			if pf, ok := pfxOf(ip); ok && cgnat.Contains(pf.Addr()) {
				pfxs = append(pfxs, hostPrefix(pf.Addr()))
				hit = true
			}
		}
		if hit {
			on++
		}
	}
	if on == 0 {
		return nil
	}
	p, _ := cover(pfxs)
	return &Overlay{Range: p.String(), Nodes: on, OfNodes: len(nodes), Why: []string{
		fmt.Sprintf("%d of %d nodes have an address in %s and no overlay interface was seen (the node probe may be off).", on, len(nodes), cgnat),
		"Mesh networks use that range, and so do carriers' shared networks and some cloud pod networks.",
	}}
}

// pfxOf reads "ip/len" (the subnet it names, masked) or a bare ip (a host prefix).
func pfxOf(s string) (netip.Prefix, bool) {
	if p, err := netip.ParsePrefix(s); err == nil {
		a := p.Addr().Unmap()
		bits := p.Bits()
		if p.Addr().Is4In6() {
			bits -= 96
		}
		if bits < 0 || bits > a.BitLen() {
			return netip.Prefix{}, false
		}
		m, _ := a.Prefix(bits)
		return m, true
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return hostPrefix(a.Unmap()), true
	}
	return netip.Prefix{}, false
}

func hostPrefix(a netip.Addr) netip.Prefix { return netip.PrefixFrom(a, a.BitLen()) }

// cover is the smallest prefix that holds every prefix, taken from those of one family (IPv4 when there are any).
func cover(pfxs []netip.Prefix) (netip.Prefix, bool) {
	var fam []netip.Prefix
	for _, p := range pfxs {
		if p.Addr().Is4() {
			fam = append(fam, p)
		}
	}
	if len(fam) == 0 {
		for _, p := range pfxs {
			if p.Addr().Is6() {
				fam = append(fam, p)
			}
		}
	}
	if len(fam) == 0 {
		return netip.Prefix{}, false
	}
	bits := fam[0].Bits()
	for bits >= 0 {
		c, _ := fam[0].Addr().Prefix(bits)
		all := true
		for _, p := range fam[1:] {
			if p.Bits() < bits || !c.Contains(p.Addr()) {
				all = false
				break
			}
		}
		if all {
			return c, true
		}
		bits--
	}
	c, _ := fam[0].Addr().Prefix(0)
	return c, true
}

// Relate says whether two clusters look to be on the same overlay network. Tailscale and NetBird both draw their addresses from one
// range, so the same product on both is enough; the others (and a plain WireGuard) have no known range, so their addresses have to
// overlap. One side with addresses in the carrier range and no interface seen, against a Tailscale or NetBird on the other, is a
// maybe: a way to find out is the node probe, or the link measurement.
func Relate(a, b *Overlay) (same, maybe bool) {
	if a == nil || b == nil {
		return false, false
	}
	pa, pb := prefixOf(a), prefixOf(b)
	switch {
	case a.Interface != "" && b.Interface != "":
		if a.Product != b.Product {
			return false, false
		}
		if a.Product == "tailscale" || a.Product == "netbird" {
			return true, false
		}
		return pa.IsValid() && pb.IsValid() && pa.Overlaps(pb), false
	case a.Interface == "" && b.Interface == "":
		return false, false // two sets of addresses and nothing else say nothing
	}
	withInterface := a
	if b.Interface != "" {
		withInterface = b
	}
	return false, withInterface.Product == "tailscale" || withInterface.Product == "netbird"
}

func prefixOf(o *Overlay) netip.Prefix {
	p, err := netip.ParsePrefix(o.Range)
	if err != nil {
		return netip.Prefix{}
	}
	return p
}
