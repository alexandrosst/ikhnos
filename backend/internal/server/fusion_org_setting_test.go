package server

import (
	"testing"

	"ikhnos/internal/fusion"
)

// Turning FUSION on writes the organisation the gateway stamps into FUSION's settings, before the gateway starts and reads
// them; settings that are not there (a FUSION the server does not manage) are not created.
func TestTurningFusionOnWritesTheOrganisationTheGatewayStamps(t *testing.T) {
	a := newIntentRig(t)
	f, k := newFusion(t, a.adminRig)
	k.settings = map[string]string{"loki.retention": "336h"}
	a.a.Fusion = f
	if _, err := f.Enable(a.ctx, a.core.Fusion, "alex"); err != nil {
		t.Fatal(err)
	}
	if k.settings[fusion.OrgSettingKey] != "org-1" {
		t.Fatalf("settings = %v", k.settings)
	}
	f2, k2 := newFusion(t, a.adminRig) // no settings ConfigMap
	if _, err := f2.Enable(a.ctx, a.core.Fusion, "alex"); err != nil {
		t.Fatal(err)
	}
	if k2.settings != nil {
		t.Fatalf("settings were created: %v", k2.settings)
	}
}
