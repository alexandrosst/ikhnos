package chart

import (
	"strings"
	"testing"
)

// The gateway stamps the organisation FUSION belongs to on everything it takes in: from the settings the server writes when
// it manages FUSION, from central.orgId when it does not; with neither set, nothing is stamped and the senders' own stands.
func TestTheGatewayStampsTheOrganisation(t *testing.T) {
	r := fusionRender(t, "f", "--set", "switch.managed=true")
	cfg := r.configs["f-fusion-central"].Data["otel-collector-config.yaml"]
	if !strings.Contains(cfg, `set(attributes["ikhnos.org.id"], "${env:IKHNOS_ORG_ID}") where "${env:IKHNOS_ORG_ID}" != ""`) {
		t.Fatalf("no organisation stamp:\n%s", cfg)
	}
	pipelines := otelConfig(t, r.configs["f-fusion-central"].Data)["service"].(map[string]any)["pipelines"].(map[string]any)
	for _, p := range []string{"metrics", "logs", "traces"} {
		procs, _ := pipelines[p].(map[string]any)["processors"].([]any)
		has := false
		for _, x := range procs {
			has = has || x == "transform/org"
		}
		if !has {
			t.Errorf("the %s pipeline does not run transform/org: %v", p, procs)
		}
	}
	if _, ok := r.configs["f-fusion-settings"].Data["central.orgId"]; !ok {
		t.Fatalf("no central.orgId in the settings: %v", r.configs["f-fusion-settings"].Data)
	}
	env := r.deploys["f-fusion-central"].Spec.Template.Spec.Containers[0].Env
	found := false
	for _, e := range env {
		if e.Name == "IKHNOS_ORG_ID" {
			found = e.ValueFrom != nil && e.ValueFrom.ConfigMapKeyRef != nil && e.ValueFrom.ConfigMapKeyRef.Key == "central.orgId" && *e.ValueFrom.ConfigMapKeyRef.Optional
		}
	}
	if !found {
		t.Fatalf("managed: the gateway's IKHNOS_ORG_ID does not come from the settings: %+v", env)
	}

	r = fusionRender(t, "f", "--set", "switch.managed=false", "--set", "central.orgId=edge-lab")
	for _, e := range r.deploys["f-fusion-central"].Spec.Template.Spec.Containers[0].Env {
		if e.Name == "IKHNOS_ORG_ID" && e.Value != "edge-lab" {
			t.Fatalf("not managed: IKHNOS_ORG_ID = %q", e.Value)
		}
	}
	if _, err := fusionTemplate(t, "f", "--set", "central.orgId=a b"); err == nil {
		t.Fatal("an organisation id that is not one is refused")
	}
}
