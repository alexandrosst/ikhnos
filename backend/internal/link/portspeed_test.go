package link

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThePortSpeedIsWhatTheKernelNegotiatedOrNothing(t *testing.T) {
	dir := t.TempDir()
	write := func(iface, content string) {
		if err := os.MkdirAll(filepath.Join(dir, iface), 0o755); err != nil {
			t.Fatal(err)
		}
		if content != "" {
			if err := os.WriteFile(filepath.Join(dir, iface, "speed"), []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("eno1", "100\n")
	write("enp3s0", "2500\n")
	write("down0", "-1\n")
	write("wlan0", "") // no speed file, as for a virtual interface
	write("odd0", "fast\n")
	for name, want := range map[string]uint64{"eno1": 100e6, "enp3s0": 2500e6, "down0": 0, "wlan0": 0, "odd0": 0, "missing": 0} {
		if got := speedOf(dir, name); got != want {
			t.Errorf("%s: %d, want %d", name, got, want)
		}
	}
}

// Whatever the machine is, asking for a route to an address sends nothing and never panics.
func TestFindingThePortOfARouteNeverFails(t *testing.T) {
	_ = portSpeed(netip.MustParseAddr("192.0.2.1"))
	_ = portSpeed(netip.MustParseAddr("2001:db8::1"))
	_ = portSpeed(netip.Addr{})
}

func TestThePortSpeedIsExportedForAPeerThatKnowsOneAndOtherwiseLeftOut(t *testing.T) {
	f := &fakeIperf{}
	var asked []netip.Addr
	speed := uint64(100e6)
	c, ck := capFor(t, f, func(c *Capacity) {
		c.Reverse, c.Parallel = false, 0
		c.PortSpeed = func(a netip.Addr) uint64 { asked = append(asked, a); return speed }
	})
	settle(c)
	key := `ikhnos_link_throughput_port_bits_per_second{cluster="cl-me",peer="cl-a",method="iperf3"}`
	if got := render(c.Meter)[key]; got != "100000000" {
		t.Errorf("port speed series = %q", got)
	}
	if len(asked) != 1 || asked[0].String() != "10.0.0.1" {
		t.Errorf("asked about %v", asked)
	}
	// A port that reports none leaves the series out: a wrong 0 would read as a link of no capacity.
	speed = 0
	ck.add(25 * 60 * 60 * 1e9)
	settle(c)
	for k := range render(c.Meter) {
		if strings.Contains(k, "port_bits_per_second") && strings.Contains(k, `peer="cl-a"`) {
			t.Errorf("a port with no speed is exported: %s", k)
		}
	}
}
