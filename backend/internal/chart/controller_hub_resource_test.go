package chart

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ikhnos/internal/controller"
	"ikhnos/internal/install"
)

// An agent that a controller keeps has the link hub whenever its resource turns the cluster-links signal on, and none without it: the
// hub is part of cluster links, not a field of the resource.
func TestAResourceWithClusterLinksGetsTheLinkHub(t *testing.T) {
	b, err := Package()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tgz := filepath.Join(dir, Filename())
	if err := writeFile(tgz, b); err != nil {
		t.Fatal(err)
	}
	two := 2
	render := func(links bool) []object {
		spec := controller.Spec{
			Server: controller.ServerSpec{Address: "a.example:8443", CAPin: "sha256/ab"}, AccessTier: &two,
			Telemetry: &install.ResourceTelemetry{
				Signals:      install.ResourceSignals{ClusterLinks: links, ResourceUsage: true},
				ClusterLinks: &install.ResourceClusterLinks{Probe: links},
				Destination:  &install.ResourceDestination{Endpoint: "otel.example.com:4317"},
			},
		}
		vals, err := spec.Values("t")
		if err != nil {
			t.Fatal(err)
		}
		j, err := json.Marshal(vals)
		if err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(dir, "values.json")
		if err := writeFile(f, j); err != nil {
			t.Fatal(err)
		}
		return helmObjects(t, tgz, "-f", f, "--set", "controller.resource="+install.ControllerResourceName)
	}
	has := func(objs []object, name string) bool {
		for _, o := range objs {
			if o.Kind == "Deployment" && strings.HasSuffix(o.Metadata.Name, name) {
				return true
			}
		}
		return false
	}
	if !has(render(true), "link-hub") {
		t.Error("a resource with cluster links renders no hub")
	}
	if has(render(false), "link-hub") {
		t.Error("a resource without cluster links renders a hub")
	}
}
