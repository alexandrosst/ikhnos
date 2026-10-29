package linkhub

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, args ...string) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	code := Main(args)
	w.Close()
	os.Stderr = old
	out, _ := io.ReadAll(r)
	return code, string(out)
}

func secret(t *testing.T, name, v string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A hub started wrongly says so at once, before it listens on anything.
func TestAHubThatIsStartedWronglySaysSoAtOnce(t *testing.T) {
	hub := secret(t, "hub", "hub-secret-0123456789abcdef")
	peers := secret(t, "peers", "peers-secret-0123456789abcdef")
	iperf := secret(t, "iperf", "iperf-secret-0123456789abcdef")
	short := secret(t, "short", "tiny")
	for name, c := range map[string]struct {
		args []string
		want string
	}{
		"no secret":                          {nil, "--secret-file is required"},
		"a short secret":                     {[]string{"--secret-file", short}, "at least 16 characters"},
		"a missing file":                     {[]string{"--secret-file", filepath.Join(t.TempDir(), "nope")}, "--secret-file"},
		"iperf without peers":                {[]string{"--secret-file", hub, "--iperf-secret-file", iperf}, "--iperf-secret-file needs --peers-secret-file"},
		"the agent's secret as the prober's": {[]string{"--secret-file", hub, "--peers-secret-file", hub}, "--link-hub-secret-file and --link-peers-secret-file hold the same secret"},
		"one secret for both pods":           {[]string{"--secret-file", hub, "--peers-secret-file", peers, "--iperf-secret-file", peers}, "hold the same secret"},
		"ports that are not ports":           {[]string{"--secret-file", hub, "--prober-ports", "eight"}, "--prober-ports"},
		"an address that is taken":           {[]string{"--secret-file", hub, "--listen", "not an address"}, "--listen"},
	} {
		code, out := run(t, c.args...)
		if code == 0 || !strings.Contains(out, c.want) {
			t.Errorf("%s: code %d, output:\n%s\nwant %q", name, code, out, c.want)
		}
	}
}
