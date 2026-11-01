package agent

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func agentRun(t *testing.T, args ...string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := Main(append([]string{"--server", "s.example:8443", "--ca-pin", "ab"}, args...))
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return code, string(out)
}

func writeSecret(t *testing.T, name, v string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Cluster links are served by the link hub: the agent listens for nothing of the links and holds none of the secrets the prober and the responders
// sign with, those are the hub's. A flag that is wrong for that is refused at start-up, before anything connects, in words that say which.
func TestTheLinkHubFlagsAreHeldToWhatTheyMean(t *testing.T) {
	flow := writeSecret(t, "flow", "flow-secret-0123456789abcdef")
	hub := writeSecret(t, "hub", "hub-secret-0123456789abcdef")
	probe := writeSecret(t, "probe", "probe-secret-0123456789abcdef")
	const url = "http://hub.example:9106"
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no secret for the hub":           {[]string{"--link-hub-url", url, "--flow-secret-file", flow}, "--link-hub-url needs --link-hub-secret-file"},
		"no flow secret":                  {[]string{"--link-hub-url", url, "--link-hub-secret-file", hub}, "--link-hub-url needs --flow-secret-file"},
		"not a URL":                       {[]string{"--link-hub-url", "hub.example:9106", "--link-hub-secret-file", hub, "--flow-secret-file", flow}, "must be an http(s) URL"},
		"a secret with no hub":            {[]string{"--link-hub-secret-file", hub}, "--link-hub-secret-file needs --link-hub-url"},
		"serving peers with no hub":       {[]string{"--link-serve-peers"}, "--link-serve-peers needs --link-hub-url"},
		"serving credentials with no hub": {[]string{"--link-serve-iperf"}, "--link-serve-iperf needs --link-hub-url"},
		"credentials without the prober":  {[]string{"--link-hub-url", url, "--link-hub-secret-file", hub, "--flow-secret-file", flow, "--link-serve-iperf"}, "--link-serve-iperf needs --link-serve-peers"},
		"prober ports that are not ports": {[]string{"--link-hub-url", url, "--link-hub-secret-file", hub, "--flow-secret-file", flow, "--link-prober-ports", "8861,eight"}, "--link-prober-ports"},
		"the hub's secret is the flow's":  {[]string{"--link-hub-url", url, "--link-hub-secret-file", flow, "--flow-secret-file", flow}, "--flow-secret-file and --link-hub-secret-file hold the same secret"},
		"the hub's secret is the probe's": {[]string{"--link-hub-url", url, "--link-hub-secret-file", probe, "--flow-secret-file", flow, "--probe-listen", ":8081", "--probe-secret-file", probe}, "--probe-secret-file and --link-hub-secret-file hold the same secret"},
		"a short secret":                  {[]string{"--link-hub-url", url, "--link-hub-secret-file", writeSecret(t, "short", "tiny"), "--flow-secret-file", flow, "--probe-listen", ":8081"}, "--link-hub-secret-file must hold a secret of at least 16 characters"},
	} {
		code, out := agentRun(t, c.args...)
		if code == 0 || !strings.Contains(out, c.want) {
			t.Errorf("%s: code %d, output:\n%s\nwant %q", name, code, out, c.want)
		}
	}
}

// Asking for the prober to be served is no request for a listener and a secret, only a statement of what the install contains.
func TestServingPeersNeedsNoListenerWhenTheHubDoesIt(t *testing.T) {
	flow := writeSecret(t, "flow", "flow-secret-0123456789abcdef")
	hub := writeSecret(t, "hub", "hub-secret-0123456789abcdef")
	code, out := agentRun(t, "--link-hub-url", "http://hub.example:9106", "--link-hub-secret-file", hub, "--flow-secret-file", flow, "--probe-listen", ":0", "--link-serve-peers", "--link-serve-iperf", "--link-prober-ports", "8861")
	for _, not := range []string{"--link-serve-peers needs", "--link-serve-iperf needs", "--link-hub-url needs", "cannot be combined"} {
		if strings.Contains(out, not) {
			t.Errorf("code %d: %q was asked of an agent that has a hub:\n%s", code, not, out)
		}
	}
}

// The agent no longer has a listener, nor secrets, of its own for the links: the flags that said so do not exist, and an install that still
// passes one is refused by the flag parser instead of quietly serving nothing.
func TestTheAgentsOwnLinkServingFlagsAreGone(t *testing.T) {
	flow := writeSecret(t, "flow", "flow-secret-0123456789abcdef")
	for _, f := range []string{"--link-metrics-listen", "--link-peers-secret-file", "--link-iperf-secret-file"} {
		code, out := agentRun(t, "--flow-secret-file", flow, f, "x")
		if code == 0 || !strings.Contains(out, "flag provided but not defined") {
			t.Errorf("%s: code %d, output:\n%s", f, code, out)
		}
	}
}
