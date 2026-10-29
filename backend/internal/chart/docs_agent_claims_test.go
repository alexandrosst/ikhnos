package chart

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The discovery agent is described as the cluster's one door, and read against the code it does a few things that are not the door: it serves
// the passive cluster-link series for the cluster collector to scrape, it renews the collectors' client certificates, and it writes three
// narrow things. The pages and comments said otherwise, and a reader who trusted them would have looked in the wrong place. These phrases are
// the false ones; this test keeps them from coming back, and keeps the true statements that replaced them.

func readAll(t *testing.T, roots []string, suffixes ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			for _, s := range suffixes {
				if strings.HasSuffix(p, s) {
					if b, err := os.ReadFile(p); err == nil {
						out[p] = string(b)
					}
				}
			}
			return nil
		})
	}
	return out
}

func TestTheDocumentsAndCommentsDoNotClaimWhatTheAgentDoesNotDo(t *testing.T) {
	repo := filepath.Join("..", "..", "..")
	docs := readAll(t, []string{filepath.Join(repo, "docs-site", "docs"), filepath.Join("ikhnos-agent")}, ".md", ".mdx")
	code := readAll(t, []string{filepath.Join("..", "agent"), filepath.Join("..", "probe"), filepath.Join("..", "link"), filepath.Join("..", "controller"), filepath.Join("..", "install"), filepath.Join("..", "flow")}, ".go")
	if len(docs) < 20 || len(code) < 20 {
		t.Fatalf("found %d documents and %d sources to read", len(docs), len(code))
	}
	for _, c := range []struct {
		where  map[string]string
		phrase string
		why    string
	}{
		{docs, "never on the telemetry path", "the agent serves the passive link series that the cluster collector scrapes"},
		{docs, "agent is not on it either. it reads the collectors' counters", "the same claim, in the words components.md used"},
		{docs, "the agent does not touch the telemetry", "it renews the collectors' client certificates and serves the link series"},
		{docs, "it is read-only in every tier", "the agent writes its identity Secret, the certificate Secrets it renews and one status field"},
		{docs, "nothing about telemetry is pushed live", "the telemetry request is a narrow, opt-in way to ask for signals and sampling"},
		{docs, "only ever says *which* signals are on", "the agent also reports the export address, redaction, sampling and the sources"},
		{docs, "the next step is for the server to hand that change to the agent", "the relay exists"},
		{code, "reports are read and dropped", "below tier 2 the aggregator holds them up to its caps and nothing is sent"},
		{code, "cannot make the receiver buffer anything", "a request that passes the header checks is read up to MaxBody before the HMAC is verified"},
		{code, "reading it is the controller's next step", "the controller reads status.requested in relay.go"},
		{code, "nothing else is served", "the signed peer-list and iperf routes are mounted on the same listener"},
	} {
		for path, text := range c.where {
			if strings.Contains(strings.ToLower(text), c.phrase) {
				t.Errorf("%s says %q: %s", path, c.phrase, c.why)
			}
		}
	}

	// What replaced them.
	for _, c := range []struct{ file, phrase string }{
		{filepath.Join(repo, "docs-site", "docs", "architecture", "components.md"), "Where the agent touches the data plane today"},
		{filepath.Join(repo, "docs-site", "docs", "architecture", "components.md"), "the collectors' client certificates"},
		{filepath.Join(repo, "docs-site", "docs", "architecture", "permissions-reference.md"), "three narrow writes"},
		{filepath.Join("ikhnos-agent", "README.md"), "three narrow things"},
	} {
		b, err := os.ReadFile(c.file)
		if err != nil {
			t.Errorf("%s: %v", c.file, err)
			continue
		}
		if !strings.Contains(string(b), c.phrase) {
			t.Errorf("%s no longer says %q", c.file, c.phrase)
		}
	}
}
