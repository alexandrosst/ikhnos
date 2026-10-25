package chart

import (
	"path/filepath"
	"strings"
	"testing"

	"ikhnos/internal/install"
)

// An agent that a controller keeps may write the status of that one resource when the server revokes it, and may do nothing else with it
// (not its spec: a patch of the object itself would let it change where the telemetry goes). An agent
// installed with Helm has nothing of the kind.
func TestAnAgentTheControllerKeepsMayPatchTheStatusOfItsOwnResourceAndNothingElseOfIt(t *testing.T) {
	b, err := Package()
	if err != nil {
		t.Fatal(err)
	}
	tgz := filepath.Join(t.TempDir(), Filename())
	if err := writeFile(tgz, b); err != nil {
		t.Fatal(err)
	}
	marks := func(args ...string) (roles, bindings int, rules []string) {
		for _, o := range helmObjects(t, tgz, append(append([]string{}, baseSet...), args...)...) {
			switch {
			case o.Kind == "Role" && o.Metadata.Name == "ikhnos-agent-mark-revoked":
				roles++
				for _, r := range o.Rules {
					rules = append(rules, rulesString(r.APIGroups, r.Resources, r.ResourceNames, r.Verbs))
				}
			case o.Kind == "RoleBinding" && o.Metadata.Name == "ikhnos-agent-mark-revoked":
				bindings++
				if len(o.Subjects) != 1 || o.Subjects[0].Name != "ikhnos-agent" || o.RoleRef.Name != "ikhnos-agent-mark-revoked" {
					t.Errorf("binding %+v", o)
				}
			}
		}
		return
	}

	roles, bindings, rules := marks("--set", "controller.resource="+install.ControllerResourceName)
	if roles != 1 || bindings != 1 || len(rules) != 1 || rules[0] != "[ikhnos.io][ikhnosagents/status][this-cluster][patch]" {
		t.Fatalf("roles %d, bindings %d, rules %v", roles, bindings, rules)
	}
	if roles, bindings, _ = marks(); roles != 0 || bindings != 0 {
		t.Fatalf("an agent installed with Helm was given the right to patch a resource: %d %d", roles, bindings)
	}

	// With the relay on, the agent also reads that one status, to see what the controller made of a request; still not the object.
	roles, bindings, rules = marks("--set", "controller.resource="+install.ControllerResourceName, "--set", "controller.relayTelemetry=true")
	if roles != 1 || bindings != 1 || len(rules) != 1 || rules[0] != "[ikhnos.io][ikhnosagents/status][this-cluster][get patch]" {
		t.Fatalf("with the relay on: roles %d, bindings %d, rules %v", roles, bindings, rules)
	}
}

// The agent is told to pass a telemetry request on only when the relay is on, and only when a controller keeps it: the relay is a way to a
// resource, and without one there is nothing to write to.
func TestTheAgentIsToldToRelayTelemetryOnlyWhenTheRelayIsOnAndAControllerKeepsIt(t *testing.T) {
	relays := func(args ...string) bool {
		out, err := helmTemplate(t, args...)
		if err != nil {
			t.Fatalf("helm template %v: %v\n%s", args, err, out)
		}
		return strings.Contains(out, "IKHNOS_RELAY_TELEMETRY")
	}
	if relays() || relays("--set", "controller.relayTelemetry=true") || relays("--set", "controller.resource="+install.ControllerResourceName) {
		t.Fatal("the agent was told to relay without both a controller and the relay being on")
	}
	if !relays("--set", "controller.resource="+install.ControllerResourceName, "--set", "controller.relayTelemetry=true") {
		t.Fatal("the agent was not told to relay when a controller keeps it and the relay is on")
	}
}

func rulesString(groups, resources, names, verbs []string) string {
	return "" + join(groups) + join(resources) + join(names) + join(verbs)
}

func join(xs []string) string {
	s := "["
	for i, x := range xs {
		if i > 0 {
			s += " "
		}
		s += x
	}
	return s + "]"
}
