package collector

import (
	"os/exec"
	"strings"
	"testing"
)

// The collector runs on every node, privileged enough to load an eBPF program. It must not link the agent's side of the flow pipeline, whose
// resolver reads the cluster's workloads through the Kubernetes client: that is a large amount of code, none of it used there, in a process that
// holds a kernel hook. What the two sides share is in package flow/wire.
func TestTheCollectorDoesNotLinkTheAgentsSideOrTheKubernetesClient(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool to list the imports with")
	}
	out, err := exec.Command(goBin, "list", "-deps", "-f", "{{.ImportPath}}", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, p := range strings.Fields(string(out)) {
		switch {
		case strings.HasPrefix(p, "k8s.io/"):
			t.Errorf("the collector links %s", p)
		case p == "ikhnos/internal/flow", p == "ikhnos/internal/agent/collect", p == "ikhnos/internal/link":
			t.Errorf("the collector links %s, which is the agent's side", p)
		}
	}
}
