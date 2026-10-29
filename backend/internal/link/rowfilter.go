package link

import (
	"fmt"
	"net/netip"
	"strings"

	ikhnosv1 "ikhnos/gen/ikhnosv1"
)

// RowSink takes in the rows of a flow report that were admitted for the link series. The meter does, in the agent's process; the
// forwarder does, when the series are kept by the link hub instead.
type RowSink interface {
	ObserveRows(rows []*ikhnosv1.RawFlow, bytesKnown bool)
}

// RowFilter leaves out the rows that are not the link's traffic: the link prober's own ports and the connections of the agent's own
// addresses (see Meter.IgnorePorts and Meter.IgnoreOwn, which say why). It is what the forwarder applies before a row leaves the agent,
// because the agent is the one that knows its own addresses.
type RowFilter struct {
	ports []portRange
	own   map[netip.Addr]bool
}

// NewRowFilter reads the two lists a Meter is given by the chart: ports and ranges ("8861,8862,18900-18999") and addresses.
func NewRowFilter(ports, own string) (*RowFilter, error) {
	r, err := parsePorts(ports)
	if err != nil {
		return nil, err
	}
	addrs := map[netip.Addr]bool{}
	for _, f := range strings.Split(own, ",") {
		if f = strings.TrimSpace(f); f == "" {
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not an address", f)
		}
		addrs[a.Unmap()] = true
	}
	return &RowFilter{ports: r, own: addrs}, nil
}

// Keep says whether the row is the link's traffic. A nil filter keeps everything that is a row.
func (f *RowFilter) Keep(r *ikhnosv1.RawFlow) bool {
	if r == nil {
		return false
	}
	if f == nil {
		return true
	}
	for _, p := range f.ports {
		if r.Port >= p.lo && r.Port <= p.hi {
			return false
		}
	}
	if len(f.own) > 0 {
		if a, err := netip.ParseAddr(r.LocalIp); err == nil && f.own[a.Unmap()] {
			return false
		}
	}
	return true
}
