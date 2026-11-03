package controller

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"ikhnos/internal/install"
	"ikhnos/internal/relay"
)

// reported is the agent's write: status.identity, which leaves the rest of the status alone.
func (g *rig) reported(name, clusterID string, org ...string) {
	g.t.Helper()
	u := g.get(name)
	st, _ := u.Object["status"].(map[string]any)
	if st == nil {
		st = map[string]any{}
	}
	id := map[string]any{"clusterId": clusterID}
	if len(org) > 0 {
		id["orgId"] = org[0]
	}
	st["identity"] = id
	u.Object["status"] = st
	if _, err := g.dyn.Resource(GVR).Namespace(ns).Update(context.Background(), u, metav1.UpdateOptions{}); err != nil {
		g.t.Fatal(err)
	}
}

// stamped is the cluster id the release is installed with on its telemetry, "" when none; stampedOrg is the organisation's.
func stamped(values map[string]any) string { return resourceValue(values, "clusterId") }

func stampedOrg(values map[string]any) string { return resourceValue(values, "orgId") }

func resourceValue(values map[string]any, key string) string {
	tel, _ := values["telemetry"].(map[string]any)
	res, _ := tel["resource"].(map[string]any)
	id, _ := res[key].(string)
	return id
}

// A cluster kept by a controller is told who it is by its own agent: the id the server gave it is stamped on its telemetry unless the
// resource says one, so that nobody has to copy it from the server into the resource for the data to say which cluster it is from.
func TestTheClusterIdTheAgentReportsIsStampedOnTheTelemetryWhenTheSpecHasNone(t *testing.T) {
	g := newRig(t)
	g.r.AllowTelemetry = true
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", specWithTelemetry(someTelemetry))
	g.must("a")
	if id := stamped(g.inst.lastApplied()); id != "" || g.inst.applies() != 1 {
		t.Fatalf("before the agent says who the cluster is: %q after %d installs", id, g.inst.applies())
	}

	g.reported("a", "cl-a287110206", "org-9")
	g.must("a")
	if id := stamped(g.inst.lastApplied()); id != "cl-a287110206" || stampedOrg(g.inst.lastApplied()) != "org-9" || g.inst.applies() != 2 {
		t.Fatalf("after: %q / %q after %d installs", id, stampedOrg(g.inst.lastApplied()), g.inst.applies())
	}
	// The controller's own writes to the status leave the agent's word where it is, and nothing is installed again for it.
	if st := g.status("a"); st.Identity == nil || st.Identity.ClusterID != "cl-a287110206" || st.Phase != PhaseReady {
		t.Fatalf("status %+v", st)
	}
	g.must("a")
	g.must("a")
	if g.inst.applies() != 2 {
		t.Errorf("%d installs with nothing changed", g.inst.applies())
	}

	// What the resource says stands: an owner of its own is not replaced by what the agent reports.
	spec := specWithTelemetry(map[string]any{
		"signals":     someTelemetry["signals"],
		"destination": someTelemetry["destination"],
		"owner":       map[string]any{"orgId": "org-1", "clusterId": "cl-chosen"},
	})
	g.setSpec("a", spec)
	g.must("a")
	if id := stamped(g.inst.lastApplied()); id != "cl-chosen" || stampedOrg(g.inst.lastApplied()) != "org-1" {
		t.Errorf("the spec's own ids were replaced: %q / %q", id, stampedOrg(g.inst.lastApplied()))
	}
}

func TestAnOwnerWithOnlyTheOrganisationGetsTheClusterIdAndKeepsTheOrganisation(t *testing.T) {
	g := newRig(t)
	g.r.AllowTelemetry = true
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", specWithTelemetry(map[string]any{
		"signals":     someTelemetry["signals"],
		"destination": someTelemetry["destination"],
		"owner":       map[string]any{"orgId": "org-1"},
	}))
	g.reported("a", "cl-7")
	g.must("a")
	v := g.inst.lastApplied()
	res := v["telemetry"].(map[string]any)["resource"].(map[string]any)
	if res["clusterId"] != "cl-7" || res["orgId"] != "org-1" {
		t.Fatalf("resource %v", res)
	}
}

func TestWithIdentityChangesNothingItShouldNot(t *testing.T) {
	tel := &install.ResourceTelemetry{Owner: &install.Owner{OrgID: "o"}}
	spec := Spec{Telemetry: tel}
	got := withIdentity(spec, &relay.Identity{ClusterID: "cl-1", OrgID: "other"}) // (the organisation the spec names stands)
	if got.Telemetry.Owner.ClusterID != "cl-1" || got.Telemetry.Owner.OrgID != "o" {
		t.Errorf("owner %+v", got.Telemetry.Owner)
	}
	if tel.Owner.ClusterID != "" {
		t.Errorf("the spec given was changed: %+v", tel.Owner)
	}
	// Nothing to stamp on, nothing to stamp, or an id that is not a plain word (it ends up in the values of a release).
	for name, c := range map[string]struct {
		spec Spec
		id   *relay.Identity
	}{
		"no telemetry":   {Spec{}, &relay.Identity{ClusterID: "cl-1"}},
		"no identity":    {spec, nil},
		"an empty id":    {spec, &relay.Identity{}},
		"a space":        {spec, &relay.Identity{ClusterID: "cl 1"}},
		"a quote":        {spec, &relay.Identity{ClusterID: `cl-1"`}},
		"a comma":        {spec, &relay.Identity{ClusterID: "a,b=c"}},
		"too long":       {spec, &relay.Identity{ClusterID: strings.Repeat("a", 129)}},
		"a line break":   {spec, &relay.Identity{ClusterID: "cl-1\n"}},
		"a curly brace":  {spec, &relay.Identity{ClusterID: "{cl}"}},
		"the spec's own": {Spec{Telemetry: &install.ResourceTelemetry{Owner: &install.Owner{ClusterID: "mine", OrgID: "mine"}}}, &relay.Identity{ClusterID: "cl-1", OrgID: "org"}},
	} {
		got := withIdentity(c.spec, c.id)
		if (c.spec.Telemetry == nil) != (got.Telemetry == nil) || (got.Telemetry != nil && got.Telemetry.Owner != c.spec.Telemetry.Owner) {
			t.Errorf("%s: the spec was changed: %+v", name, got.Telemetry)
		}
	}
}

// Each id is taken on its own: a spec that names the organisation only gets the cluster, one that names the cluster only gets the organisation,
// and an organisation that is not a plain word is left out while the cluster is still stamped.
func TestEachIdIsTakenOnItsOwn(t *testing.T) {
	only := func(o install.Owner) Spec { return Spec{Telemetry: &install.ResourceTelemetry{Owner: &o}} }
	got := withIdentity(only(install.Owner{ClusterID: "mine"}), &relay.Identity{ClusterID: "cl-1", OrgID: "org-1"})
	if got.Telemetry.Owner.ClusterID != "mine" || got.Telemetry.Owner.OrgID != "org-1" {
		t.Errorf("%+v", got.Telemetry.Owner)
	}
	got = withIdentity(only(install.Owner{}), &relay.Identity{ClusterID: "cl-1", OrgID: "bad org"})
	if got.Telemetry.Owner.ClusterID != "cl-1" || got.Telemetry.Owner.OrgID != "" {
		t.Errorf("%+v", got.Telemetry.Owner)
	}
}
