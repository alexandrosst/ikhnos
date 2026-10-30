package chart

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"ikhnos/internal/controller"
	"ikhnos/internal/install"
)

// An agent that a controller keeps takes the link hub from its resource: the values the controller makes from
// spec.telemetry.clusterLinks.hub render the hub, and a resource without it renders none.
func TestAResourceThatNamesTheLinkHubGetsOne(t *testing.T) {
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
	render := func(hub bool) []object {
		spec := controller.Spec{
			Server: controller.ServerSpec{Address: "a.example:8443", CAPin: "sha256/ab"}, AccessTier: &two,
			Telemetry: &install.ResourceTelemetry{
				Signals:      install.ResourceSignals{ClusterLinks: true},
				ClusterLinks: &install.ResourceClusterLinks{Probe: true, Hub: hub},
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
		t.Error("a resource that names the hub renders none")
	}
	if has(render(false), "link-hub") {
		t.Error("a resource that does not name the hub renders one")
	}
}
