package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func secretFile(t *testing.T, name, value string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A secret that two kinds of pod share lets either sign what only the other is meant to: refused at start-up, naming both flags and what it would allow.
func TestTheSharedSecretsMustAllDiffer(t *testing.T) {
	probe := secretFile(t, "probe", "probe-secret-0123456789")
	flow := secretFile(t, "flow", "flow-secret-0123456789a")
	peers := secretFile(t, "peers", "peers-secret-0123456789")
	iperf := secretFile(t, "iperf", "iperf-secret-0123456789")
	all := func(p, f, pe, i string) error {
		return SecretsDiffer(SharedSecret{Flag: "--probe-secret-file", File: p}, SharedSecret{Flag: "--flow-secret-file", File: f}, SharedSecret{Flag: "--link-peers-secret-file", File: pe}, SharedSecret{Flag: "--link-iperf-secret-file", File: i})
	}
	if err := all(probe, flow, peers, iperf); err != nil {
		t.Fatalf("four different secrets: %v", err)
	}
	// Not set, or not readable, is not a clash; the place that reads the secret says so.
	if err := all(probe, "", "", ""); err != nil {
		t.Fatalf("one secret: %v", err)
	}
	if err := all(probe, filepath.Join(t.TempDir(), "missing"), "", ""); err != nil {
		t.Fatalf("an unreadable one: %v", err)
	}
	same := secretFile(t, "same", "  probe-secret-0123456789\n")
	for name, c := range map[string]struct {
		err  error
		want []string
	}{
		"probe and flow":  {all(probe, same, "", ""), []string{"--probe-secret-file", "--flow-secret-file", "forge traffic reports"}},
		"probe and peers": {all(probe, "", same, ""), []string{"--probe-secret-file", "--link-peers-secret-file", "forge node facts"}},
		"flow and iperf":  {all("", flow, "", secretFile(t, "f2", "flow-secret-0123456789a\n")), []string{"--flow-secret-file", "--link-iperf-secret-file"}},
		"peers and iperf": {all("", "", peers, secretFile(t, "p2", "peers-secret-0123456789")), []string{"--link-peers-secret-file", "--link-iperf-secret-file", "private key"}},
	} {
		if c.err == nil {
			t.Errorf("%s: the same secret was accepted", name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(c.err.Error(), w) {
				t.Errorf("%s: %q does not say %q", name, c.err, w)
			}
		}
	}
}
