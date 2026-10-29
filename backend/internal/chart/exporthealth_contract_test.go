package chart

import (
	"encoding/json"
	"io"
	"sort"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	"ikhnos/internal/agent/exporthealth"
)

// The agent reads the local operator's counters by names it carries in Go: the receivers and scrape jobs it reports on, the ports it reads
// them from and the pods and labels it finds them by. The chart renders the other side of every one of those. Nothing tied the two together,
// so a rename in the chart (the collector's deprecated component aliases are due to be renamed) would have emptied rows on the Pipeline
// page without any error. These tests render the chart in the configurations that turn each part on and fail when the two sides differ.

// healthEverything turns on every source and tool the agent reports on, bundled where there is a bundled one.
var healthEverything = append(append([]string{"--set", "telemetry.export.otlp.endpoint=gw:4317"}, allSignals...),
	"--set", "telemetry.systemLogs.logs.journaling=journald",
	"--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.energy.metrics.source=bundle-kepler",
	"--set", "telemetry.accelerators.metrics.enabled=true", "--set", "telemetry.accelerators.metrics.source=bundle-dcgm",
	"--set", "flowObserver.enabled=true", "--set", "telemetry.clusterLinks.metrics.enabled=true", "--set", "telemetry.clusterLinks.probe.enabled=true")

// healthExisting reads energy and accelerators from exporters the cluster already has, which are different scrape jobs.
var healthExisting = append(append([]string{"--set", "telemetry.export.otlp.endpoint=gw:4317"}, allSignals...),
	"--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.energy.metrics.source=existing", "--set", "telemetry.energy.metrics.existing.prometheusEndpoint=k:9102",
	"--set", "telemetry.accelerators.metrics.enabled=true", "--set", "telemetry.accelerators.metrics.source=existing", "--set", "telemetry.accelerators.metrics.existing.prometheusEndpoint=d:9400")

func receiverIDs(cfg map[string]any) []string {
	rc, _ := cfg["receivers"].(map[string]any)
	var out []string
	for k := range rc {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func isOTLP(id string) bool { return id == "otlp" || strings.HasPrefix(id, "otlp/") }

// The receivers that read a source of the cluster are the ones the agent names, and no other receiver reads one.
func TestExportHealthKnowsEverySourceReceiverTheChartRenders(t *testing.T) {
	r := render(t, healthEverything...)
	rendered := map[string]bool{}
	for _, cm := range []string{"ikhnos-telemetry-host-config", "ikhnos-telemetry-cluster-config"} {
		for _, id := range receiverIDs(otelConfig(t, r.configmaps[cm].Data)) {
			rendered[id] = true
			switch {
			case isOTLP(id), strings.HasPrefix(id, "prometheus/"):
				// OTLP is counted as the applications' door, and the scrapes are the tool jobs below.
			default:
				if _, ok := exporthealth.SourceReceivers[id]; !ok {
					t.Errorf("the chart renders the receiver %q and the agent does not report on it: add it to exporthealth.SourceReceivers (or to this test's exceptions, if it reads no source of the cluster)", id)
				}
			}
		}
	}
	for id := range exporthealth.SourceReceivers {
		if !rendered[id] {
			t.Errorf("the agent reports on the receiver %q and the chart does not render it with every source on: renamed or removed? Receivers rendered: %v", id, rendered)
		}
	}
}

// The agent counts the applications' door by the receiver id otlp.
func TestExportHealthFindsTheOTLPReceiver(t *testing.T) {
	r := render(t, healthEverything...)
	var found bool
	for _, id := range receiverIDs(otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)) {
		found = found || isOTLP(id)
	}
	if !found {
		t.Errorf("no receiver named otlp or otlp/* in the cluster collector: the agent's receiver rows would be empty")
	}
}

// The scrape jobs the agent reports as tools are the jobs the chart renders, and every tool job the chart renders is one it reports.
func TestExportHealthKnowsEveryToolJobTheChartRenders(t *testing.T) {
	rendered := map[string]bool{}
	for _, args := range [][]string{healthEverything, healthExisting} {
		for job := range scrapeConfigs(t, render(t, args...)) {
			rendered[job] = true
			if _, ok := exporthealth.ToolJobs[job]; !ok {
				t.Errorf("the chart renders the scrape job %q and the agent does not report on it: add it to exporthealth.ToolJobs", job)
			}
		}
	}
	for job := range exporthealth.ToolJobs {
		if !rendered[job] {
			t.Errorf("the agent reports on the scrape job %q and the chart renders no such job_name", job)
		}
	}
}

// What the agent keeps of the scrapes is what its parser reads, and the label it names nodes by survives.
func TestExportHealthSourceHealthPipelineKeepsWhatTheAgentReads(t *testing.T) {
	r := render(t, healthEverything...)
	cfg := otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)

	proc, _ := cfg["processors"].(map[string]any)["filter/source_health"].(map[string]any)
	raw, _ := json.Marshal(proc["metric_conditions"])
	for _, name := range []string{"up", "scrape_samples_scraped"} {
		if !strings.Contains(string(raw), `metric.name == \"`+name+`\"`) {
			t.Errorf("filter/source_health does not keep %q, which the agent reads: %s", name, raw)
		}
	}
	exp, _ := cfg["exporters"].(map[string]any)["prometheus/source_health"].(map[string]any)
	conv, _ := exp["resource_to_telemetry_conversion"].(map[string]any)
	if conv["enabled"] != true {
		t.Errorf("prometheus/source_health does not convert resource attributes to labels, so the node label the agent names a down target by is lost: %v", exp)
	}
	pipelines, _ := cfg["service"].(map[string]any)["pipelines"].(map[string]any)
	p, _ := pipelines["metrics/source_health"].(map[string]any)
	if got := p["exporters"]; len(asStrings(got)) != 1 || asStrings(got)[0] != "prometheus/source_health" {
		t.Errorf("metrics/source_health exports to %v, want prometheus/source_health", got)
	}
}

func asStrings(v any) []string {
	var out []string
	l, _ := v.([]any)
	for _, e := range l {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// The ports the agent is told to read are the ports the collectors serve, and the source-health port is told only where it is served.
func TestExportHealthTargetsAreThePortsTheCollectorsServe(t *testing.T) {
	r := render(t, healthEverything...)
	targets := exporthealth.ParseTargets(healthTargetsEnv(t, r))

	counters := map[string]string{}
	for name, cm := range map[string]string{"host": "ikhnos-telemetry-host-config", "cluster": "ikhnos-telemetry-cluster-config"} {
		_, port, ok := healthReader(t, otelConfig(t, r.configmaps[cm].Data))
		if !ok {
			t.Fatalf("%s collector serves no counters", name)
		}
		counters[name] = portString(port)
	}
	cluster := otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)
	exp, _ := cluster["exporters"].(map[string]any)["prometheus/source_health"].(map[string]any)
	endpoint, _ := exp["endpoint"].(string)
	sources := endpoint[strings.LastIndex(endpoint, ":")+1:]

	want := map[string]bool{
		"ikhnos-telemetry-host-metrics":    false,
		"ikhnos-telemetry-cluster-metrics": false,
	}
	var sawSources bool
	for _, tg := range targets {
		host, port, _ := strings.Cut(tg, ":")
		name, _, _ := strings.Cut(host, ".")
		switch {
		case name == "ikhnos-telemetry-host-metrics" && port == counters["host"]:
			want[name] = true
		case name == "ikhnos-telemetry-cluster-metrics" && port == counters["cluster"]:
			want[name] = true
		case name == "ikhnos-telemetry-cluster-metrics" && port == sources:
			sawSources = true
		default:
			t.Errorf("target %q is not a port a collector serves (counters %v, sources %s)", tg, counters, sources)
		}
	}
	for name, ok := range want {
		if !ok {
			t.Errorf("no target for %s's counters in %v", name, targets)
		}
	}
	if !sawSources {
		t.Errorf("no target for the cluster collector's source health (port %s) in %v", sources, targets)
	}

	// With no tool and no link scrape there is no source-health pipeline, and nothing to read on its port.
	plain := render(t, append([]string{"--set", "telemetry.export.otlp.endpoint=gw:4317"}, allSignals...)...)
	for _, tg := range exporthealth.ParseTargets(healthTargetsEnv(t, plain)) {
		if strings.HasSuffix(tg, ":"+sources) {
			t.Errorf("a release with no scrape is told to read source health at %q, where nothing is served", tg)
		}
	}
}

func healthTargetsEnv(t *testing.T, r rendered) string {
	t.Helper()
	v, _ := env(r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0], "IKHNOS_TELEMETRY_HEALTH_TARGETS")
	return v
}

func portString(p any) string {
	switch v := p.(type) {
	case float64:
		return strconv.Itoa(int(v))
	case string:
		return v
	}
	return ""
}

// The headless Services the agent resolves select the collectors' pods, and the policy that lets the agent in selects the agent's pods.
func TestExportHealthFindsThePodsByTheLabelsTheyCarry(t *testing.T) {
	r := render(t, append(append([]string{}, healthEverything...), "--set", "telemetry.health.networkPolicy.enabled=true")...)
	out, err := helmTemplate(t, append(append([]string{}, healthEverything...), "--set", "telemetry.health.networkPolicy.enabled=true")...)
	if err != nil {
		t.Fatal(err)
	}
	services := map[string]corev1.Service{}
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(out), 4096)
	for {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		var s corev1.Service
		if json.Unmarshal(raw, &s) == nil && s.Kind == "Service" {
			services[s.Name] = s
		}
	}
	podLabels := map[string]map[string]string{
		"ikhnos-telemetry-host-metrics":    r.daemonsets["ikhnos-telemetry-host"].Spec.Template.Labels,
		"ikhnos-telemetry-cluster-metrics": r.deployments["ikhnos-telemetry-cluster"].Spec.Template.Labels,
	}
	for name, labels := range podLabels {
		svc, ok := services[name]
		if !ok {
			t.Errorf("no Service %s", name)
			continue
		}
		if svc.Spec.ClusterIP != "None" {
			t.Errorf("%s is not headless, so the agent would reach one pod, not every pod", name)
		}
		if len(svc.Spec.Selector) == 0 {
			t.Errorf("%s selects nothing", name)
		}
		for k, v := range svc.Spec.Selector {
			if labels[k] != v {
				t.Errorf("%s selects %s=%s and the collector pods carry %s=%q", name, k, v, k, labels[k])
			}
		}
	}

	agentLabels := r.deployments["ikhnos-agent"].Spec.Template.Labels
	policy, ok := r.policies["ikhnos-telemetry-host-health"]
	if !ok {
		t.Fatalf("no ikhnos-telemetry-host-health NetworkPolicy with telemetry.health.networkPolicy.enabled")
	}
	for _, in := range policy.Spec.Ingress {
		for _, from := range in.From {
			if from.PodSelector == nil {
				continue
			}
			for k, v := range from.PodSelector.MatchLabels {
				if agentLabels[k] != v {
					t.Errorf("the policy lets pods with %s=%s in and the agent's pods carry %s=%q", k, v, k, agentLabels[k])
				}
			}
		}
	}
}
