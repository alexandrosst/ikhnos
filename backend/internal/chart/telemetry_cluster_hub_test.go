package chart

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// hubArgs turns the link hub on, with the prober and the throughput tests in the automatic mode, so that every secret the hub may hold is in play.
func hubArgs(extra ...string) []string {
	return autoArgs(append([]string{"telemetry.clusterLinks.hub.enabled=true"}, extra...)...)
}

func agentContainerOf(t *testing.T, r rendered) corev1.Container {
	t.Helper()
	d, ok := r.deployments["ikhnos-agent"]
	if !ok {
		t.Fatal("no agent deployment")
	}
	return containerNamed(t, d.Spec.Template.Spec.Containers, "agent")
}

func hubContainerOf(t *testing.T, r rendered) corev1.Container {
	t.Helper()
	d, ok := r.deployments["ikhnos-link-hub"]
	if !ok {
		t.Fatal("no link hub deployment")
	}
	return containerNamed(t, d.Spec.Template.Spec.Containers, "hub")
}

func serviceNamed(t *testing.T, docs []doc, name string) corev1.Service {
	t.Helper()
	for _, d := range docs {
		if d.kind() == "Service" && d.name() == name {
			var s corev1.Service
			decodeInto(t, d, &s)
			return s
		}
	}
	t.Fatalf("no Service %s", name)
	return corev1.Service{}
}

func hasServiceNamed(docs []doc, name string) bool {
	for _, d := range docs {
		if d.kind() == "Service" && d.name() == name {
			return true
		}
	}
	return false
}

func hasPort(c corev1.Container, name string, port int32) bool {
	for _, p := range c.Ports {
		if p.Name == name && p.ContainerPort == port {
			return true
		}
	}
	return false
}

// Without the switch nothing of the hub exists and the agent serves the link pieces exactly as it did.
func TestTheLinkHubIsOffByDefaultAndTheAgentServesTheLinksItself(t *testing.T) {
	args := autoArgs()
	r := render(t, args...)
	docs := renderDocs(t, args...)
	if _, ok := r.deployments["ikhnos-link-hub"]; ok {
		t.Error("a hub Deployment is rendered while the hub is off")
	}
	if _, ok := r.secrets["ikhnos-agent-link-hub"]; ok {
		t.Error("a hub Secret is rendered while the hub is off")
	}
	if _, ok := r.policies["ikhnos-link-hub"]; ok {
		t.Error("a hub NetworkPolicy is rendered while the hub is off")
	}
	if hasServiceNamed(docs, "ikhnos-agent-link-hub") {
		t.Error("the hub's Service is rendered while the hub is off")
	}
	a := agentContainerOf(t, r)
	env := iperfPodEnv(a)
	for k, want := range map[string]string{"IKHNOS_LINK_METRICS_LISTEN": ":9104", "IKHNOS_LINK_PEERS_SECRET_FILE": "/link-peers/secret", "IKHNOS_LINK_IPERF_SECRET_FILE": "/link-iperf/secret"} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
	for _, k := range []string{"IKHNOS_LINK_HUB_URL", "IKHNOS_LINK_HUB_SECRET_FILE"} {
		if _, ok := env[k]; ok {
			t.Errorf("the agent sets %s while the hub is off", k)
		}
	}
	if !hasPort(a, "link-metrics", 9104) {
		t.Error("the agent does not open the link metrics port")
	}
	if sel := serviceNamed(t, docs, "ikhnos-agent-link-metrics").Spec.Selector["app.kubernetes.io/name"]; sel != "ikhnos-agent" {
		t.Errorf("the link metrics Service selects %q, want the agent", sel)
	}
	if got := mountedSecrets(r.deployments["ikhnos-agent"].Spec.Template.Spec); !slices.Contains(got, "ikhnos-agent-link-peers") || !slices.Contains(got, "ikhnos-agent-link-iperf") {
		t.Errorf("the agent mounts %v", got)
	}
}

// With the hub the agent listens for nothing of the links and holds neither secret of the APIs the hub now answers; it holds the hub's.
func TestWithTheHubTheAgentListensForNoLinkRequestAndHoldsTheHubSecretOnly(t *testing.T) {
	r := render(t, hubArgs()...)
	a := agentContainerOf(t, r)
	env := iperfPodEnv(a)
	if env["IKHNOS_LINK_HUB_URL"] == "" || !strings.HasPrefix(env["IKHNOS_LINK_HUB_URL"], "http://ikhnos-agent-link-hub.") || !strings.HasSuffix(env["IKHNOS_LINK_HUB_URL"], ".svc:9106") {
		t.Errorf("IKHNOS_LINK_HUB_URL = %q", env["IKHNOS_LINK_HUB_URL"])
	}
	if env["IKHNOS_LINK_HUB_SECRET_FILE"] != "/link-hub/secret" {
		t.Errorf("IKHNOS_LINK_HUB_SECRET_FILE = %q", env["IKHNOS_LINK_HUB_SECRET_FILE"])
	}
	for _, k := range []string{"IKHNOS_LINK_METRICS_LISTEN", "IKHNOS_LINK_PEERS_SECRET_FILE", "IKHNOS_LINK_IPERF_SECRET_FILE"} {
		if v, ok := env[k]; ok {
			t.Errorf("the agent still sets %s=%s with the hub on", k, v)
		}
	}
	// What the agent has to know to pass on and to filter is still told.
	for _, k := range []string{"IKHNOS_LINK_SERVE_PEERS", "IKHNOS_LINK_SERVE_IPERF", "IKHNOS_LINK_PROBER_PORTS", "IKHNOS_LINK_OWN_ADDRESSES", "IKHNOS_FLOW_SECRET_FILE"} {
		if _, ok := env[k]; !ok {
			t.Errorf("the agent does not set %s with the hub on", k)
		}
	}
	for _, p := range a.Ports {
		if p.Name == "link-metrics" {
			t.Error("the agent opens the link metrics port with the hub on")
		}
	}
	got := mountedSecrets(r.deployments["ikhnos-agent"].Spec.Template.Spec)
	if !slices.Contains(got, "ikhnos-agent-link-hub") {
		t.Errorf("the agent does not mount the hub secret: %v", got)
	}
	for _, s := range []string{"ikhnos-agent-link-peers", "ikhnos-agent-link-iperf"} {
		if slices.Contains(got, s) {
			t.Errorf("the agent mounts %s with the hub on: it is the hub's", s)
		}
	}
	ann := r.deployments["ikhnos-agent"].Spec.Template.Annotations
	if ann["checksum/link-hub-secret"] == "" {
		t.Error("a change to the hub secret would not roll the agent")
	}
	for _, k := range []string{"checksum/link-peers-secret", "checksum/link-iperf-secret"} {
		if _, ok := ann[k]; ok {
			t.Errorf("the agent carries %s although it no longer reads that secret", k)
		}
	}
}

func TestTheHubIsASmallHardenedPodWithNoKubernetesToken(t *testing.T) {
	r := render(t, hubArgs()...)
	d := r.deployments["ikhnos-link-hub"]
	if *d.Spec.Replicas != 1 || d.Spec.Strategy.Type != "Recreate" {
		t.Errorf("replicas %d, strategy %s", *d.Spec.Replicas, d.Spec.Strategy.Type)
	}
	spec := d.Spec.Template.Spec
	if spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken {
		t.Error("the hub mounts a Kubernetes token")
	}
	if spec.ServiceAccountName != "" && spec.ServiceAccountName != "default" {
		t.Errorf("the hub runs as ServiceAccount %q: it needs none of the agent's", spec.ServiceAccountName)
	}
	if spec.HostNetwork {
		t.Error("the hub runs in the host network")
	}
	h := hubContainerOf(t, r)
	if got := strings.Join(h.Args, " "); got != "link-hub" {
		t.Errorf("args = %q", got)
	}
	if h.Image != agentContainerOf(t, r).Image {
		t.Errorf("the hub image is %q, the agent's %q: it is the same binary", h.Image, agentContainerOf(t, r).Image)
	}
	sc := h.SecurityContext
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem || sc.Capabilities == nil || !slices.Contains(sc.Capabilities.Drop, "ALL") {
		t.Errorf("security context = %+v", sc)
	}
	if spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot {
		t.Error("the hub does not run as non-root")
	}
	if !hasPort(h, "link-metrics", 9104) || !hasPort(h, "link-hub", 9106) {
		t.Errorf("ports = %+v", h.Ports)
	}
	env := iperfPodEnv(h)
	for k, want := range map[string]string{"IKHNOS_LINK_METRICS_LISTEN": ":9104", "IKHNOS_LINK_HUB_LISTEN": ":9106", "IKHNOS_LINK_HUB_SECRET_FILE": "/link-hub/secret", "IKHNOS_LINK_PEERS_SECRET_FILE": "/link-peers/secret", "IKHNOS_LINK_IPERF_SECRET_FILE": "/link-iperf/secret"} {
		if env[k] != want {
			t.Errorf("%s = %q, want %q", k, env[k], want)
		}
	}
	// What it leaves out and what the agent leaves out is the same list.
	if env["IKHNOS_LINK_PROBER_PORTS"] == "" || env["IKHNOS_LINK_PROBER_PORTS"] != iperfPodEnv(agentContainerOf(t, r))["IKHNOS_LINK_PROBER_PORTS"] {
		t.Errorf("prober ports: hub %q, agent %q", env["IKHNOS_LINK_PROBER_PORTS"], iperfPodEnv(agentContainerOf(t, r))["IKHNOS_LINK_PROBER_PORTS"])
	}
	// It reaches nothing: no server address, no CA pin, no token, no identity.
	for k := range env {
		if slices.Contains([]string{"IKHNOS_SERVER", "IKHNOS_CA_PIN", "IKHNOS_TOKEN", "IKHNOS_IDENTITY_SECRET", "IKHNOS_FLOW_SECRET_FILE", "IKHNOS_PROBE_SECRET_FILE"}, k) {
			t.Errorf("the hub is given %s", k)
		}
	}
	got := mountedSecrets(spec)
	slices.Sort(got)
	if want := []string{"ikhnos-agent-link-hub", "ikhnos-agent-link-iperf", "ikhnos-agent-link-peers"}; !slices.Equal(got, want) {
		t.Errorf("the hub mounts %v, want %v", got, want)
	}
	for _, k := range []string{"checksum/link-hub-secret", "checksum/link-peers-secret", "checksum/link-iperf-secret"} {
		if d.Spec.Template.Annotations[k] == "" {
			t.Errorf("the hub pod has no %s: a change to that secret would not roll it", k)
		}
	}
}

// Every address anyone is configured with stays: the Service keeps its name and port, and only the pods behind it change.
func TestTheLinkMetricsServiceKeepsItsNameAndPointsAtTheHub(t *testing.T) {
	args := hubArgs()
	r := render(t, args...)
	docs := renderDocs(t, args...)
	svc := serviceNamed(t, docs, "ikhnos-agent-link-metrics")
	if svc.Spec.Selector["app.kubernetes.io/name"] != "ikhnos-link-hub" {
		t.Errorf("selector = %v", svc.Spec.Selector)
	}
	if len(svc.Spec.Ports) != 1 || svc.Spec.Ports[0].Port != 9104 || svc.Spec.Ports[0].TargetPort.StrVal != "link-metrics" {
		t.Errorf("ports = %+v", svc.Spec.Ports)
	}
	ingest := serviceNamed(t, docs, "ikhnos-agent-link-hub")
	if ingest.Spec.Selector["app.kubernetes.io/name"] != "ikhnos-link-hub" || len(ingest.Spec.Ports) != 1 || ingest.Spec.Ports[0].Port != 9106 || ingest.Spec.Ports[0].TargetPort.StrVal != "link-hub" {
		t.Errorf("ingest Service = %+v", ingest.Spec)
	}
	// Both select pods that carry those labels and expose those named ports.
	tpl := r.deployments["ikhnos-link-hub"].Spec.Template
	for k, v := range svc.Spec.Selector {
		if tpl.Labels[k] != v {
			t.Errorf("the hub pod does not carry %s=%s that the Service selects", k, v)
		}
	}
	h := hubContainerOf(t, r)
	if !hasPort(h, "link-metrics", 9104) || !hasPort(h, "link-hub", 9106) {
		t.Errorf("the Services' target ports are not on the container: %+v", h.Ports)
	}
	// The agent's pods are no longer behind it: they must not match the hub's selector, nor the hub's the agent's.
	if r.deployments["ikhnos-agent"].Spec.Template.Labels["app.kubernetes.io/name"] == "ikhnos-link-hub" {
		t.Error("the agent pod carries the hub's name")
	}
	// The prober, the responders and the cluster collector use the same Service, so they need no change.
	if u := proberEnv(t, r)["IKHNOS_LINK_AGENT_URL"]; !strings.HasPrefix(u, "http://ikhnos-agent-link-metrics.") || !strings.HasSuffix(u, ".svc:9104") {
		t.Errorf("the prober's URL = %q", u)
	}
}

// The three secrets the hub holds, and the flow secret, are four different values, and the hub's goes to nothing but the agent and the hub.
func TestTheHubSecretIsGeneratedDistinctAndHeldByTheAgentAndTheHubAlone(t *testing.T) {
	r := render(t, hubArgs()...)
	s, ok := r.secrets["ikhnos-agent-link-hub"]
	if !ok {
		t.Fatal("no hub Secret")
	}
	val := s.StringData["secret"]
	if len(val) < 32 {
		t.Errorf("the generated secret is %d characters", len(val))
	}
	for _, other := range []string{"ikhnos-agent-link-peers", "ikhnos-agent-link-iperf", "ikhnos-agent-flows"} {
		if o, ok := r.secrets[other]; ok && o.StringData["secret"] == val {
			t.Errorf("the hub secret equals %s", other)
		}
	}
	for n, d := range r.deployments {
		if n == "ikhnos-agent" || n == "ikhnos-link-hub" {
			continue
		}
		if slices.Contains(mountedSecrets(d.Spec.Template.Spec), "ikhnos-agent-link-hub") {
			t.Errorf("deployment %s mounts the hub secret", n)
		}
	}
	for n, d := range r.daemonsets {
		if slices.Contains(mountedSecrets(d.Spec.Template.Spec), "ikhnos-agent-link-hub") {
			t.Errorf("daemonset %s mounts the hub secret", n)
		}
	}
	// A secret of your own is used as given, and the pods that read it roll when it changes.
	own := render(t, hubArgs("telemetry.clusterLinks.hub.secret=a-hub-secret-of-at-least-32-characters")...)
	if got := own.secrets["ikhnos-agent-link-hub"].StringData["secret"]; got != "a-hub-secret-of-at-least-32-characters" {
		t.Errorf("secret = %q", got)
	}
	a1 := r.deployments["ikhnos-agent"].Spec.Template.Annotations["checksum/link-hub-secret"]
	a2 := own.deployments["ikhnos-agent"].Spec.Template.Annotations["checksum/link-hub-secret"]
	h1 := r.deployments["ikhnos-link-hub"].Spec.Template.Annotations["checksum/link-hub-secret"]
	h2 := own.deployments["ikhnos-link-hub"].Spec.Template.Annotations["checksum/link-hub-secret"]
	if a1 == a2 || h1 == h2 {
		t.Error("changing the hub secret does not change the pods' checksums")
	}
	// A generated secret does not make the checksum differ from one render to the next (that would roll the pods on every upgrade).
	again := render(t, hubArgs()...)
	if again.deployments["ikhnos-agent"].Spec.Template.Annotations["checksum/link-hub-secret"] != a1 {
		t.Error("the agent's checksum changes between renders of the same values")
	}
}

func TestTheHubSecretMustDifferFromTheOthersAndBeLongEnough(t *testing.T) {
	const same = "one-secret-of-at-least-thirty-two-chars"
	for name, args := range map[string][]string{
		"the flow secret":    hubArgs("flowObserver.secret="+same, "telemetry.clusterLinks.hub.secret="+same),
		"the peers secret":   hubArgs("telemetry.clusterLinks.probe.peersSecret="+same, "telemetry.clusterLinks.hub.secret="+same),
		"the request secret": hubArgs("telemetry.clusterLinks.probe.capacity.auth.requestSecret="+same, "telemetry.clusterLinks.hub.secret="+same),
		"the probe secret":   hubArgs("nodeProbe.enabled=true", "nodeProbe.secret="+same, "telemetry.clusterLinks.hub.secret="+same),
	} {
		out, err := helmTemplate(t, args...)
		if err == nil {
			t.Errorf("%s: rendered although the hub secret equals it", name)
		} else if !strings.Contains(out, "telemetry.clusterLinks.hub.secret must differ") {
			t.Errorf("%s: the refusal does not say why:\n%s", name, out)
		}
	}
	if _, err := helmTemplate(t, hubArgs("telemetry.clusterLinks.hub.secret=short")...); err == nil {
		t.Error("a 5 character hub secret was accepted")
	}
}

func TestTheHubNeedsTheLinkMetricsAndRefusesWhatItDoesNotKnow(t *testing.T) {
	base := append([]string{}, agentTel...)
	base = append(base, "--set", "flowObserver.enabled=true", "--set", "telemetry.clusterLinks.hub.enabled=true")
	// Without metrics.enabled the hub has nothing to make: that is said, not rendered idle or silently left out.
	out, err := helmTemplate(t, base...)
	if err == nil {
		t.Errorf("a hub was accepted although the link metrics are off:\n%s", out)
	} else if !strings.Contains(out, "telemetry.clusterLinks.hub.enabled requires telemetry.clusterLinks.metrics.enabled") {
		t.Errorf("the refusal does not say why:\n%s", out)
	}
	if _, err := helmTemplate(t, hubArgs("telemetry.clusterLinks.hub.speed=fast")...); err == nil {
		t.Error("an unknown hub key was accepted")
	}
	if _, err := helmTemplate(t, hubArgs("telemetry.clusterLinks.hub.enabled=sometimes")...); err == nil {
		t.Error("a non-boolean switch was accepted")
	}
}

// Only the agent may send the hub anything. The collector scrapes; the host-network prober and responders ask by source address.
func TestTheHubNetworkPolicyLetsOnlyTheAgentReachTheIngestPort(t *testing.T) {
	r := render(t, hubArgs("flowObserver.networkPolicy=true", "flowObserver.nodeCIDRs[0]=10.42.0.0/16")...)
	p, ok := r.policies["ikhnos-link-hub"]
	if !ok {
		t.Fatal("no hub network policy")
	}
	if p.Spec.PodSelector.MatchLabels["app.kubernetes.io/name"] != "ikhnos-link-hub" {
		t.Errorf("the policy selects %v", p.Spec.PodSelector.MatchLabels)
	}
	var ingestFrom, scrapeFrom, askFrom []string
	for _, in := range p.Spec.Ingress {
		for _, port := range in.Ports {
			for _, f := range in.From {
				var who string
				switch {
				case f.PodSelector != nil:
					who = "pod:" + f.PodSelector.MatchLabels["app.kubernetes.io/name"]
				case f.IPBlock != nil:
					who = "cidr:" + f.IPBlock.CIDR
				default:
					who = "other"
				}
				switch port.Port.IntValue() {
				case 9106:
					ingestFrom = append(ingestFrom, who)
				case 9104:
					if strings.HasPrefix(who, "cidr:") {
						askFrom = append(askFrom, who)
					} else {
						scrapeFrom = append(scrapeFrom, who)
					}
				default:
					t.Errorf("port %d is open", port.Port.IntValue())
				}
			}
			if len(in.From) == 0 {
				t.Errorf("port %d is open to every source", port.Port.IntValue())
			}
		}
	}
	if !slices.Equal(ingestFrom, []string{"pod:ikhnos-agent"}) {
		t.Errorf("9106 is open to %v, want the agent only", ingestFrom)
	}
	if !slices.Equal(scrapeFrom, []string{"pod:ikhnos-telemetry-cluster"}) {
		t.Errorf("9104 is open by pod to %v, want the cluster collector only", scrapeFrom)
	}
	if !slices.Equal(askFrom, []string{"cidr:10.42.0.0/16"}) {
		t.Errorf("9104 is open by address to %v, want flowObserver.nodeCIDRs", askFrom)
	}
	// The agent's own policy no longer opens 9104.
	if ap, ok := r.policies["ikhnos-agent-probe"]; ok {
		for _, in := range ap.Spec.Ingress {
			for _, port := range in.Ports {
				if port.Port.IntValue() == 9104 {
					t.Errorf("the agent's policy still opens 9104: %+v", in)
				}
			}
		}
	}
	// The policy can be left out.
	if _, ok := render(t, hubArgs("telemetry.clusterLinks.hub.networkPolicy=false")...).policies["ikhnos-link-hub"]; ok {
		t.Error("the hub policy is rendered although networkPolicy is false")
	}
	// Without the address list the prober's rule is any source, as the agent's was.
	open := render(t, hubArgs()...).policies["ikhnos-link-hub"]
	found := false
	for _, in := range open.Spec.Ingress {
		for _, f := range in.From {
			if f.IPBlock != nil && f.IPBlock.CIDR == "0.0.0.0/0" {
				found = true
			}
		}
	}
	if !found {
		t.Error("with flowObserver.networkPolicy off the prober and the responders are not let through")
	}
}

func TestTheCollectorAndTheAgentMayReachTheHubWhenTheirEgressIsRestricted(t *testing.T) {
	r := render(t, hubArgs(
		"networkPolicy.telemetryEgress.enabled=true", "networkPolicy.telemetryEgress.allowedEgress[0].cidr=10.0.0.0/8", "networkPolicy.telemetryEgress.allowedEgress[0].ports[0]=4317",
		"networkPolicy.egress.enabled=true", "networkPolicy.egress.apiServerCIDRs[0]=10.43.0.1/32", "networkPolicy.egress.serverCIDRs[0]=192.0.2.10/32")...)
	scrape, ok := r.policies["ikhnos-telemetry-scrape-egress"]
	if !ok {
		t.Fatal("no scrape egress policy")
	}
	found := false
	for _, rule := range scrape.Spec.Egress {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntValue() == 9104 {
				found = true
				if len(rule.To) != 1 || rule.To[0].PodSelector == nil || rule.To[0].PodSelector.MatchLabels["app.kubernetes.io/name"] != "ikhnos-link-hub" {
					t.Errorf("the collector may reach %+v on 9104, want only the hub", rule.To)
				}
			}
		}
	}
	if !found {
		t.Error("the collector's egress policy does not allow the hub's port 9104")
	}
	eg, ok := r.policies["ikhnos-agent-egress"]
	if !ok {
		t.Fatal("no agent egress policy")
	}
	found = false
	for _, rule := range eg.Spec.Egress {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntValue() == 9106 {
				found = true
				if len(rule.To) != 1 || rule.To[0].PodSelector == nil || rule.To[0].PodSelector.MatchLabels["app.kubernetes.io/name"] != "ikhnos-link-hub" {
					t.Errorf("the agent may reach %+v on 9106, want only the hub", rule.To)
				}
			}
		}
	}
	if !found {
		t.Error("the agent's egress policy does not allow the hub's ingest port, so it could not send anything")
	}
	// And without the hub the egress policy is as it was.
	off := render(t, autoArgs("networkPolicy.egress.enabled=true", "networkPolicy.egress.apiServerCIDRs[0]=10.43.0.1/32", "networkPolicy.egress.serverCIDRs[0]=192.0.2.10/32")...)
	for _, rule := range off.policies["ikhnos-agent-egress"].Spec.Egress {
		for _, p := range rule.Ports {
			if p.Port != nil && p.Port.IntValue() == 9106 {
				t.Error("the agent's egress policy opens 9106 with the hub off")
			}
		}
	}
}

// With the metrics on and the prober off the hub serves the series only: no peers, no credentials, no secret of those APIs.
func TestTheHubWithoutTheProberServesTheSeriesOnly(t *testing.T) {
	r := render(t, clusterLinksArgs("telemetry.clusterLinks.hub.enabled=true")...)
	h := hubContainerOf(t, r)
	env := iperfPodEnv(h)
	for _, k := range []string{"IKHNOS_LINK_PEERS_SECRET_FILE", "IKHNOS_LINK_IPERF_SECRET_FILE", "IKHNOS_LINK_PROBER_PORTS"} {
		if v, ok := env[k]; ok {
			t.Errorf("the hub sets %s=%s without the prober", k, v)
		}
	}
	if got := mountedSecrets(r.deployments["ikhnos-link-hub"].Spec.Template.Spec); !slices.Equal(got, []string{"ikhnos-agent-link-hub"}) {
		t.Errorf("the hub mounts %v", got)
	}
	a := iperfPodEnv(agentContainerOf(t, r))
	if _, ok := a["IKHNOS_LINK_SERVE_PEERS"]; ok {
		t.Error("the agent says it serves peers without the prober")
	}
}

// With a Secret of your own for the iperf3 credentials the Ikhnos server issues none, so the hub is not given the request secret.
func TestTheHubIsNotGivenTheRequestSecretWhenTheCredentialsAreYourOwn(t *testing.T) {
	r := render(t, hubArgs("telemetry.clusterLinks.probe.capacity.auth.existingSecret=iperf-keys")...)
	if _, ok := iperfPodEnv(hubContainerOf(t, r))["IKHNOS_LINK_IPERF_SECRET_FILE"]; ok {
		t.Error("the hub serves server credentials although the user's own Secret is in use")
	}
	if slices.Contains(mountedSecrets(r.deployments["ikhnos-link-hub"].Spec.Template.Spec), "ikhnos-agent-link-iperf") {
		t.Error("the hub mounts the request secret")
	}
	if _, ok := iperfPodEnv(agentContainerOf(t, r))["IKHNOS_LINK_SERVE_IPERF"]; ok {
		t.Error("the agent passes on credentials although the user's own Secret is in use")
	}
}

// The environment variables the chart sets on the hub and on the agent in hub mode are ones the binaries read: a name that drifted would
// be set and ignored, and the pod would start without what it was told.
func TestTheNamesTheChartSetAreTheOnesTheHubAndTheAgentRead(t *testing.T) {
	read := func(path string) map[string]bool {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, m := range regexp.MustCompile(`cli\.Env\("(IKHNOS_[A-Z0-9_]+)"`).FindAllStringSubmatch(string(b), -1) {
			out[m[1]] = true
		}
		return out
	}
	hubReads := read("../cli/linkhub/linkhub.go")
	agentReads := read("../cli/agent/agent.go")
	r := render(t, hubArgs()...)
	for k := range iperfPodEnv(hubContainerOf(t, r)) {
		if k == "GOMEMLIMIT" {
			continue
		}
		if !hubReads[k] {
			t.Errorf("the chart sets %s on the hub, which does not read it", k)
		}
	}
	for k := range iperfPodEnv(agentContainerOf(t, r)) {
		if strings.HasPrefix(k, "IKHNOS_LINK_") && !agentReads[k] {
			t.Errorf("the chart sets %s on the agent, which does not read it", k)
		}
	}
	// The ports the chart opens are the defaults the hub binds to, and the paths the secrets are mounted at are the ones it is told.
	src, err := os.ReadFile("../cli/linkhub/linkhub.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"IKHNOS_LINK_METRICS_LISTEN", ":9104"`, `"IKHNOS_LINK_HUB_LISTEN", ":9106"`} {
		if !strings.Contains(string(src), want) {
			t.Errorf("the hub's defaults no longer include %s: the chart's ports and Services assume them", want)
		}
	}
}
