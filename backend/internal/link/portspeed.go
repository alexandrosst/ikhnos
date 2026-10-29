package link

import (
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// PortSpeedFunc tells the speed, in bits per second, of the network port that traffic to addr leaves through (0: not known).
type PortSpeedFunc func(addr netip.Addr) uint64

// sysClassNet is where the kernel lists a network interface's negotiated speed.
const sysClassNet = "/sys/class/net"

// portSpeed finds the interface a datagram to addr would leave through and reads its negotiated speed. Opening a UDP socket
// towards an address only asks the kernel for the route: nothing is sent. A virtual interface, a bridge or a Wi-Fi card has no
// speed to read, and a link that is down reports -1; all of those are 0 here, which the series leaves out, so what it shows is
// always a speed the hardware negotiated.
func portSpeed(addr netip.Addr) uint64 {
	c, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(addr, 9)))
	if err != nil {
		return 0
	}
	defer c.Close()
	ua, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0
	}
	local, ok := netip.AddrFromSlice(ua.IP)
	if !ok {
		return 0
	}
	ifs, err := net.Interfaces()
	if err != nil {
		return 0
	}
	for _, ifc := range ifs {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if p, err := netip.ParsePrefix(a.String()); err == nil && p.Addr().Unmap() == local.Unmap() {
				return speedOf(sysClassNet, ifc.Name)
			}
		}
	}
	return 0
}

// speedOf reads the speed file of an interface under dir: megabits per second as a number, or -1 (or an error) when there is none.
func speedOf(dir, name string) uint64 {
	b, err := os.ReadFile(filepath.Join(dir, name, "speed"))
	if err != nil {
		return 0
	}
	mbit, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || mbit <= 0 {
		return 0
	}
	return uint64(mbit) * 1_000_000
}
