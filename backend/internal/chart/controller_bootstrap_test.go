package chart

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"

	"ikhnos/internal/controller"
	"ikhnos/internal/install"
)

// The one command the Connect page prints sets chart values; the chart turns them into the Secret of the token and the controller's
// bootstrap resource. Whatever the page was asked for must come out of that as the resource the page's own manifest would have been, so
// the single command and the resource that is kept in a repository say the same thing.
func TestTheOneCommandsValuesBecomeTheResourceTheManifestWouldHaveBeen(t *testing.T) {
	h, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm is not installed")
	}
	const token = "ikh_the-one-time-token"
	for name, res := range map[string]install.AgentResource{
		"plain": {Server: "ik.example.com:8443", CAPin: "sha256/AbC+/=", Tier: 2},
		"everything": {
			Server: "ik.example.com:8443", CAPin: "sha256/AbC+/=", Tier: 2,
			Collectors:    install.Collectors{NodeProbe: true, FlowObserver: true, Measurements: true},
			Scope:         install.Scope{Namespaces: []string{"shop", "pay"}, Exclude: []string{"pay-test"}, Selector: "ikhnos.io/scope=yes"},
			PurgeIdentity: true,
			Image:         &install.ResourceImage{Repository: "reg.example/ikhnos", Tag: "1.2.3", Digest: "sha256:" + strings.Repeat("a", 64)},
		},
		"a lower tier": {Server: "ik.example.com:8443", CAPin: "sha256/AbC+/=", Tier: 1, Collectors: install.Collectors{NodeProbe: true}},
	} {
		ctl := install.ControllerInstall{Chart: "oci://reg.example/ikhnos-controller", MaxTier: res.Tier, Registry: "reg.example"}
		values, err := ctl.BootstrapValues(res, token)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		doc, err := install.HelmValues(values)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		raw, _ := json.Marshal(doc)
		file := filepath.Join(t.TempDir(), "values.json")
		if err := os.WriteFile(file, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(h, "template", "ik-ctl", controllerChart(), "--namespace", ctlNamespace, "-f", file).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: helm template: %v\n%s", name, err, out)
		}
		var spec, secretToken string
		dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(out), 4096)
		for {
			var o struct {
				Kind     string `json:"kind"`
				Metadata struct {
					Name string `json:"name"`
				} `json:"metadata"`
				Data       map[string]string `json:"data"`
				StringData map[string]string `json:"stringData"`
			}
			if err := dec.Decode(&o); err == io.EOF {
				break
			} else if err != nil {
				t.Fatal(err)
			}
			switch {
			case o.Kind == "ConfigMap" && o.Metadata.Name == "ikhnos-controller-bootstrap":
				spec = o.Data[controller.BootstrapKey]
			case o.Kind == "Secret" && o.Metadata.Name == install.ControllerTokenSecret:
				secretToken = o.StringData[install.ControllerTokenKey]
			}
		}
		if secretToken != token {
			t.Errorf("%s: the Secret holds %q, not the token", name, secretToken)
		}
		if spec == "" {
			t.Fatalf("%s: no bootstrap resource was rendered", name)
		}
		if strings.Contains(spec, token) {
			t.Errorf("%s: the token is in the resource", name)
		}
		var got controller.Spec
		d := json.NewDecoder(strings.NewReader(spec))
		d.DisallowUnknownFields()
		if err := d.Decode(&got); err != nil {
			t.Fatalf("%s: the controller would refuse what the chart wrote: %v\n%s", name, err, spec)
		}
		if p := got.Problems(); len(p) > 0 {
			t.Errorf("%s: the controller would refuse it: %v", name, p)
		}
		manifest, err := res.Manifest(ctlNamespace)
		if err != nil {
			t.Fatal(err)
		}
		var want struct {
			Spec controller.Spec `json:"spec"`
		}
		if err := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096).Decode(&want); err != nil {
			t.Fatal(err)
		}
		// The manifest leaves the chart's default tier unsaid; the bootstrap says it. Both mean the same.
		tier := func(s controller.Spec) int {
			if s.AccessTier == nil {
				return 2
			}
			return *s.AccessTier
		}
		if tier(got) != tier(want.Spec) {
			t.Errorf("%s: tier %d, the manifest says %d", name, tier(got), tier(want.Spec))
		}
		got.AccessTier, want.Spec.AccessTier = nil, nil
		if !reflect.DeepEqual(got, want.Spec) {
			t.Errorf("%s: the chart wrote\n%+v\nthe manifest says\n%+v", name, got, want.Spec)
		}
	}
}
