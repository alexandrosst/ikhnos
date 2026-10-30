package chart

import (
	"sort"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
)

// The agent holds the cluster's identity and is the one door out, so what it may WRITE is the short list a person reviewing the install cares
// about most. permissions-reference.md says it is three things, each by name and none of them in a tier: its own identity Secret, the Secrets of the
// telemetry exporters' client certificates it renews, and the status of the one IkhnosAgent a controller keeps it from. TestAccessTierGrantsAreReadOnly
// covers the tiers; this covers everything else bound to the agent's ServiceAccount, with every optional piece turned on, so a fourth write added
// to any template is found here and not by the person who read the table.

func isRead(v string) bool { return v == "get" || v == "list" || v == "watch" }

// writesOf are the rules of the agent's ServiceAccount, across every Role and ClusterRole bound to it, that carry a verb other than get, list or watch,
// described as "namespace-or-cluster: apiGroup/resource names verbs".
func agentWrites(t *testing.T, args ...string) []string {
	t.Helper()
	docs := renderDocs(t, args...)
	roles := map[string][]rbacv1.PolicyRule{} // "Role/name" or "ClusterRole/name"
	var bindings []rbacv1.RoleBinding
	var cbindings []rbacv1.ClusterRoleBinding
	for _, d := range docs {
		switch d.kind() {
		case "Role":
			var r rbacv1.Role
			decodeInto(t, d, &r)
			roles["Role/"+r.Name] = r.Rules
		case "ClusterRole":
			var r rbacv1.ClusterRole
			decodeInto(t, d, &r)
			roles["ClusterRole/"+r.Name] = r.Rules
		case "RoleBinding":
			var b rbacv1.RoleBinding
			decodeInto(t, d, &b)
			bindings = append(bindings, b)
		case "ClusterRoleBinding":
			var b rbacv1.ClusterRoleBinding
			decodeInto(t, d, &b)
			cbindings = append(cbindings, b)
		}
	}
	forAgent := func(subjects []rbacv1.Subject) bool {
		for _, s := range subjects {
			if s.Kind == "ServiceAccount" && s.Name == "ikhnos-agent" {
				return true
			}
		}
		return false
	}
	var out []string
	add := func(scope string, rules []rbacv1.PolicyRule) {
		for _, r := range rules {
			var writes []string
			for _, v := range r.Verbs {
				if !isRead(v) {
					writes = append(writes, v)
				}
			}
			if len(writes) == 0 {
				continue
			}
			sort.Strings(writes)
			names := append([]string(nil), r.ResourceNames...)
			sort.Strings(names)
			out = append(out, scope+": "+strings.Join(r.APIGroups, ",")+"/"+strings.Join(r.Resources, ",")+" ["+strings.Join(names, ",")+"] "+strings.Join(writes, ","))
		}
	}
	for _, b := range bindings {
		if forAgent(b.Subjects) {
			add("namespace", roles[b.RoleRef.Kind+"/"+b.RoleRef.Name])
		}
	}
	for _, b := range cbindings {
		if forAgent(b.Subjects) {
			add("cluster", roles[b.RoleRef.Kind+"/"+b.RoleRef.Name])
		}
	}
	sort.Strings(out)
	return out
}

func TestTheAgentCanWriteExactlyThreeThingsEachByName(t *testing.T) {
	everything := append(append([]string{}, baseSet...),
		"--set", "access.tier=2",
		"--set", "controller.resource=my-agent", "--set", "controller.relayTelemetry=true",
		"--set", "telemetry.export.otlp.endpoint=collector.example:4317",
		"--set", "telemetry.export.otlp.tls.mtls.enabled=true", "--set", "telemetry.export.otlp.tls.mtls.secretName=op-client-tls",
		"--set", "telemetry.resourceUsage.metrics.enabled=true",
		"--set", "flowObserver.enabled=true", "--set", "nodeProbe.enabled=true",
		"--set", "telemetry.clusterLinks.metrics.enabled=true", "--set", "telemetry.clusterLinks.probe.enabled=true",
		"--set", "telemetry.clusterLinks.probe.capacity.enabled=true", "--set", "telemetry.clusterLinks.hub.enabled=true")
	want := []string{
		"namespace: /secrets [ikhnos-agent-identity] patch,update",
		"namespace: /secrets [op-client-tls] patch,update",
		"namespace: ikhnos.io/ikhnosagents/status [my-agent] patch",
	}
	for name, args := range map[string][]string{
		"cluster-wide": everything,
		"namespaced":   append(append([]string{}, everything...), "--set", "rbac.mode=namespaced", "--set", "scope.namespaces={apps}"),
	} {
		t.Run(name, func(t *testing.T) {
			got := agentWrites(t, args...)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("the agent's ServiceAccount may write:\n  %s\nwant exactly:\n  %s\n(permissions-reference.md and the install-time disclosure describe these three; a fourth needs those changed first)", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
			}
			for _, w := range got {
				if !strings.Contains(w, "[") || strings.Contains(w, "[]") {
					t.Errorf("a write that is not limited to named objects: %s", w)
				}
			}
		})
	}
}

// Without the optional pieces the agent has the identity Secret and nothing else: no renewal Role, no mark on a controller's resource.
func TestTheAgentWritesOnlyItsIdentityWhenNothingOptionalIsOn(t *testing.T) {
	got := agentWrites(t, append(append([]string{}, baseSet...), "--set", "access.tier=2")...)
	if len(got) != 1 || got[0] != "namespace: /secrets [ikhnos-agent-identity] patch,update" {
		t.Errorf("writes = %v", got)
	}
}
