package chart

import (
	"path/filepath"
	"testing"

	"ikhnos/internal/install"
)

// An agent that a controller keeps may mark that one resource when the server revokes it, and may do nothing else with it. An agent
// installed with Helm has nothing of the kind.
func TestAnAgentTheControllerKeepsMayPatchItsOwnResourceAndNothingElseOfIt(t *testing.T) {
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
	if roles != 1 || bindings != 1 || len(rules) != 1 || rules[0] != "[ikhnos.io][ikhnosagents][this-cluster][patch]" {
		t.Fatalf("roles %d, bindings %d, rules %v", roles, bindings, rules)
	}
	if roles, bindings, _ = marks(); roles != 0 || bindings != 0 {
		t.Fatalf("an agent installed with Helm was given the right to patch a resource: %d %d", roles, bindings)
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
