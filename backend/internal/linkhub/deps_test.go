package linkhub

import (
	"os/exec"
	"strings"
	"testing"
)

// The hub holds no credential of the server and no Kubernetes token, and it should not be able to: it must not link the client of the
// Kubernetes API, the agent, or anything of the server's. What it links is what it needs to serve and to be told what to serve.
func TestTheHubLinksNeitherTheKubernetesClientNorTheAgentNorTheServer(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool to list the imports with")
	}
	for _, pkg := range []string{".", "../cli/linkhub"} {
		out, err := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", pkg).CombinedOutput()
		if err != nil {
			t.Fatalf("go list %s: %v\n%s", pkg, err, out)
		}
		for _, p := range strings.Fields(string(out)) {
			switch {
			case strings.HasPrefix(p, "k8s.io/"), strings.HasPrefix(p, "sigs.k8s.io/"):
				t.Errorf("%s links %s", pkg, p)
			case strings.HasPrefix(p, "ikhnos/internal/agent"), strings.HasPrefix(p, "ikhnos/internal/server"), strings.HasPrefix(p, "ikhnos/internal/flow"), strings.HasPrefix(p, "ikhnos/internal/store"):
				t.Errorf("%s links %s", pkg, p)
			}
		}
	}
}
