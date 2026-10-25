package chart

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	sigsyaml "sigs.k8s.io/yaml"
)

// telemetry.health: each collector serves its own export counters on its pod's address, the agent is told
// where to find every pod, and only the agent may reach that port.

func healthReader(t *testing.T, cfg map[string]any) (host string, port any, ok bool) {
	t.Helper()
	svc, _ := cfg["service"].(map[string]any)
	tel, _ := svc["telemetry"].(map[string]any)
	metrics, _ := tel["metrics"].(map[string]any)
	readers, _ := metrics["readers"].([]any)
	if len(readers) != 1 {
		return "", nil, false
	}
	pull, _ := readers[0].(map[string]any)["pull"].(map[string]any)
	prom, _ := pull["exporter"].(map[string]any)["prometheus"].(map[string]any)
	h, _ := prom["host"].(string)
	return h, prom["port"], true
}

func TestTelemetryHealthIsOnWithAnySignalAndReachesBothCollectors(t *testing.T) {
	args := append([]string{"--set", "telemetry.export.otlp.endpoint=gw:4317"}, allSignals...)
	r := render(t, args...)

	for name, cm := range map[string]string{"host": "ikhnos-telemetry-host-config", "cluster": "ikhnos-telemetry-cluster-config"} {
		h, port, ok := healthReader(t, otelConfig(t, r.configmaps[cm].Data))
		if !ok {
			t.Errorf("%s collector: no metrics reader for its own counters", name)
			continue
		}
		// Bound to the pod's own address (not 0.0.0.0, which exposes it on every interface, and not the
		// default localhost, which the agent could not reach).
		if h != "${env:POD_IP}" || port != float64(8888) {
			t.Errorf("%s collector: counters served at %v:%v, want ${env:POD_IP}:8888", name, h, port)
		}
	}
	for name, env := range map[string]map[string]string{
		"host":    podIPFieldPath(r.daemonsets["ikhnos-telemetry-host"].Spec.Template.Spec.Containers[0]),
		"cluster": podIPFieldPath(r.deployments["ikhnos-telemetry-cluster"].Spec.Template.Spec.Containers[0]),
	} {
		if env["POD_IP"] != "status.podIP" {
			t.Errorf("%s collector: POD_IP = %q, want the downward-API status.podIP", name, env["POD_IP"])
		}
	}

	agent := r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0]
	got, _ := env(agent, "IKHNOS_TELEMETRY_HEALTH_TARGETS")
	// One headless-Service name per collector; the namespace is whatever the test's helm run used.
	targets := strings.Split(got, ",")
	if len(targets) != 2 || !strings.HasPrefix(targets[0], "ikhnos-telemetry-host-metrics.") || !strings.HasPrefix(targets[1], "ikhnos-telemetry-cluster-metrics.") || !strings.HasSuffix(targets[1], ".svc:8888") {
		t.Errorf("IKHNOS_TELEMETRY_HEALTH_TARGETS = %q, want one headless-Service name per collector", got)
	}

	out, err := helmTemplate(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	for _, svc := range []string{"ikhnos-telemetry-host-metrics", "ikhnos-telemetry-cluster-metrics"} {
		if !strings.Contains(out, "name: "+svc+"\n") {
			t.Errorf("no headless Service %s rendered", svc)
		}
	}
	if strings.Count(out, "clusterIP: None") < 2 {
		t.Errorf("the metrics Services must be headless so the agent resolves every pod")
	}

	// The host collector has no receiver, so it is isolated: only the agent reaches the counters.
	p, ok := r.policies["ikhnos-telemetry-host-health"]
	if !ok {
		t.Fatal("no NetworkPolicy for the host collector's counters")
	}
	if len(p.Spec.Ingress) != 1 || len(p.Spec.Ingress[0].From) != 1 || len(p.Spec.Ingress[0].Ports) != 1 {
		t.Fatalf("host health policy ingress = %+v", p.Spec.Ingress)
	}
	if p.Spec.Ingress[0].From[0].PodSelector.MatchLabels["app.kubernetes.io/name"] != "ikhnos-agent" {
		t.Errorf("host health policy admits %+v, want only the agent", p.Spec.Ingress[0].From[0].PodSelector)
	}
	// ...but the cluster collector also takes OTLP from applications, so no policy of ours may isolate it.
	if _, ok := r.policies["ikhnos-telemetry-cluster-health"]; ok {
		t.Error("a NetworkPolicy on the cluster collector would cut off its OTLP receiver")
	}
}

// podIPFieldPath is the downward-API field each environment variable of c reads (POD_IP -> status.podIP).
func podIPFieldPath(c corev1.Container) map[string]string {
	out := map[string]string{}
	for _, e := range c.Env {
		if e.ValueFrom != nil && e.ValueFrom.FieldRef != nil {
			out[e.Name] = e.ValueFrom.FieldRef.FieldPath
		}
	}
	return out
}

func TestTelemetryHealthAbsentWithoutTelemetryOrWhenOff(t *testing.T) {
	for name, args := range map[string][]string{
		"no signal": {},
		"disabled":  append([]string{"--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.health.enabled=false"}, allSignals...),
	} {
		r := render(t, args...)
		agent := r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0]
		if _, ok := env(agent, "IKHNOS_TELEMETRY_HEALTH_TARGETS"); ok {
			t.Errorf("%s: the agent was told where to read counters", name)
		}
		if _, ok := r.policies["ikhnos-telemetry-host-health"]; ok {
			t.Errorf("%s: a health NetworkPolicy was rendered", name)
		}
		if cm, ok := r.configmaps["ikhnos-telemetry-host-config"]; ok {
			if _, _, has := healthReader(t, otelConfig(t, cm.Data)); has {
				t.Errorf("%s: the host collector serves counters", name)
			}
		}
		out, err := helmTemplate(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "self-metrics") {
			t.Errorf("%s: a metrics Service was rendered", name)
		}
	}
}

func TestTelemetryHealthOnlyNamesCollectorsThatExist(t *testing.T) {
	// Host signals only: there is no cluster collector to read.
	r := render(t, "--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.resourceUsage.metrics.enabled=true")
	agent := r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0]
	got, _ := env(agent, "IKHNOS_TELEMETRY_HEALTH_TARGETS")
	if strings.Contains(got, "cluster") || !strings.Contains(got, "ikhnos-telemetry-host-metrics") {
		t.Errorf("targets = %q, want just the host collector", got)
	}
}

func TestTelemetryHealthPortIsConfigurableAndSchemaChecked(t *testing.T) {
	r := render(t, "--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.resourceUsage.metrics.enabled=true", "--set", "telemetry.health.port=9464")
	if _, port, _ := healthReader(t, otelConfig(t, r.configmaps["ikhnos-telemetry-host-config"].Data)); port != float64(9464) {
		t.Errorf("port = %v, want 9464", port)
	}
	if _, err := helmTemplate(t, "--set", "telemetry.health.port=0"); err == nil {
		t.Error("port 0 must be rejected by the schema")
	}
}

// A namespace that already isolates the cluster collector (telemetry.receiver.networkPolicy) and an agent whose
// egress is locked down (networkPolicy.egress) each need one more rule, or the counters are unreachable.
func TestTelemetryHealthIsAllowedThroughTheOtherNetworkPolicies(t *testing.T) {
	r := render(t,
		"--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.applicationMetrics.metrics.enabled=true", "--set", "telemetry.resourceUsage.metrics.enabled=true",
		"--set", "telemetry.receiver.networkPolicy.enabled=true", "--set-string", "telemetry.receiver.networkPolicy.allowedIngress[0].namespaceSelector.matchLabels.team=shop",
		"--set", "networkPolicy.egress.enabled=true", "--set", "networkPolicy.egress.apiServerCIDRs={10.0.0.1/32}", "--set", "networkPolicy.egress.serverCIDRs={10.0.0.2/32}",
	)
	in := r.policies["ikhnos-telemetry-receiver-ingress"].Spec.Ingress
	if len(in) != 2 || len(in[1].Ports) != 1 || in[1].Ports[0].Port.IntValue() != 8888 || in[1].From[0].PodSelector.MatchLabels["app.kubernetes.io/name"] != "ikhnos-agent" {
		t.Errorf("receiver ingress = %+v, want the application rule and then the agent's access to port 8888", in)
	}
	var found bool
	for _, e := range r.policies["ikhnos-agent-egress"].Spec.Egress {
		for _, p := range e.Ports {
			if p.Port != nil && p.Port.IntValue() == 8888 {
				found = true
			}
		}
	}
	if !found {
		t.Error("the agent's egress policy does not allow reading the collectors' counters")
	}
}

// telemetry.health.sourcesPort: when a scrape of Kepler, dcgm-exporter or the cluster links is on, the cluster collector keeps the scrape's own
// up and scrape_samples_scraped of each target in a pipeline of its own and serves them on a second port, which the agent is told to read.
func TestTheCollectorServesWhetherTheToolsItScrapesAnswer(t *testing.T) {
	args := []string{"--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.resourceUsage.metrics.enabled=true"}
	r := render(t, args...)
	cfg := otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)

	pipes := pipelineProcessors(t, cfg)
	if got := pipes["metrics/source_health"]; len(got) != 2 || got[0] != "memory_limiter" || got[1] != "filter/source_health" {
		t.Errorf("source health pipeline processors = %v, want memory_limiter then the filter that keeps only up and the sample count", got)
	}
	svc, _ := cfg["service"].(map[string]any)
	p, _ := svc["pipelines"].(map[string]any)["metrics/source_health"].(map[string]any)
	if ex, _ := p["exporters"].([]any); len(ex) != 1 || ex[0] != "prometheus/source_health" {
		t.Errorf("source health exporters = %v: it must go to its own local exporter and nowhere else", p["exporters"])
	}
	if rc, _ := p["receivers"].([]any); len(rc) != 1 || rc[0] != "prometheus/infra" {
		t.Errorf("source health receivers = %v, want the scrape receiver", p["receivers"])
	}
	exp, _ := cfg["exporters"].(map[string]any)["prometheus/source_health"].(map[string]any)
	if exp["endpoint"] != "${env:POD_IP}:8889" {
		t.Errorf("served at %v, want the pod's own address on 8889", exp["endpoint"])
	}
	// It keeps the two series that say whether a target answered and drops every other, which the filter states as what to drop.
	flt, _ := cfg["processors"].(map[string]any)["filter/source_health"].(map[string]any)
	conds, _ := flt["metric_conditions"].([]any)
	if len(conds) != 1 || !strings.Contains(conds[0].(string), `metric.name == "up"`) || !strings.Contains(conds[0].(string), `"scrape_samples_scraped"`) || !strings.HasPrefix(conds[0].(string), "not (") {
		t.Errorf("filter = %v", flt["metric_conditions"])
	}

	got, _ := env(r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0], "IKHNOS_TELEMETRY_HEALTH_TARGETS")
	targets := strings.Split(got, ",")
	if len(targets) != 3 || !strings.HasSuffix(targets[1], "-cluster-metrics.default.svc:8888") || !strings.HasSuffix(targets[2], "-cluster-metrics.default.svc:8889") {
		t.Errorf("IKHNOS_TELEMETRY_HEALTH_TARGETS = %q, want the counters and the source health of the cluster collector", got)
	}
	out, err := helmTemplate(t, args...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "name: source-health") {
		t.Errorf("the headless Service does not name the source-health port")
	}
	// Only the agent reaches the port, as it is the only one that reaches the counters: where the agent's egress is restricted it names the port too.
	out, err = helmTemplate(t, append(args, "--set", "networkPolicy.egress.enabled=true", "--set", "networkPolicy.egress.apiServerCIDRs={10.0.0.1/32}", "--set", "networkPolicy.egress.serverCIDRs={10.0.0.2/32}")...)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(out, "port: 8889"); n < 2 {
		t.Errorf("port 8889 is named %d times (the Service and the agent's egress), want at least 2", n)
	}

	// Without a scrape of a tool there is nothing to tell: no pipeline, no exporter, no target.
	r = render(t, "--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.resourceUsage.metrics.enabled=true", "--set", "telemetry.kubernetesState.metrics.enabled=true")
	cfg = otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)
	if _, ok := pipelineProcessors(t, cfg)["metrics/source_health"]; ok {
		t.Errorf("a source health pipeline is rendered with no tool being scraped")
	}
	if got, _ := env(r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0], "IKHNOS_TELEMETRY_HEALTH_TARGETS"); strings.Contains(got, ":8889") {
		t.Errorf("targets = %q: nothing serves 8889 here", got)
	}

	// The port may not be one the collector already uses.
	if _, err := helmTemplate(t, append(args, "--set", "telemetry.health.sourcesPort=8888")...); err == nil {
		t.Errorf("sourcesPort equal to the counters' port was accepted")
	}
}

// The pipeline in the real collector: two scrape targets of the Kepler job, one answering and one not, with the same relabel rules the discovery
// would feed (a static target carries the __meta_kubernetes_* labels), and what the collector then serves on the source-health port. Proves the
// agent can tell, per target, that Kepler answered and gave series or did not answer, and that nothing else the scrape brought is on it.
func TestSourceHealthInTheRealCollector(t *testing.T) {
	bin := collectorBinary(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "# TYPE kepler_node_package_joules_total counter\nkepler_node_package_joules_total{zone=\"package-0\"} 123\nkepler_node_dram_joules_total{zone=\"dram-0\"} 4\n")
	}))
	defer srv.Close()
	dead := httptest.NewServer(http.NotFoundHandler())
	deadAddr := strings.TrimPrefix(dead.URL, "http://")
	dead.Close() // nothing listens there now
	hostport := strings.TrimPrefix(srv.URL, "http://")

	args := []string{"--set", "telemetry.export.otlp.endpoint=gw:4317", "--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.health.sourcesPort=18889"}
	cm := render(t, args...).configmaps["ikhnos-telemetry-cluster-config"]
	var cfg map[string]any
	if err := sigsyaml.Unmarshal([]byte(cm.Data["otel-collector-config.yaml"]), &cfg); err != nil {
		t.Fatal(err)
	}
	label := func(node string) map[string]any {
		return map[string]any{
			"__meta_kubernetes_pod_node_name": node, "__meta_kubernetes_pod_phase": "Running", "__meta_kubernetes_pod_container_port_name": "metrics",
			"__meta_kubernetes_pod_label_app_kubernetes_io_name": "ikhnos-telemetry-kepler", "__meta_kubernetes_pod_label_app_kubernetes_io_instance": "ct",
		}
	}
	rcv := cfg["receivers"].(map[string]any)["prometheus/infra"].(map[string]any)["config"].(map[string]any)
	for _, sc := range rcv["scrape_configs"].([]any) {
		m := sc.(map[string]any)
		delete(m, "kubernetes_sd_configs")
		m["scrape_interval"], m["scrape_timeout"] = "1s", "500ms"
		m["static_configs"] = []any{
			map[string]any{"targets": []any{hostport}, "labels": label("node-a")},
			map[string]any{"targets": []any{deadAddr}, "labels": label("node-b")},
		}
	}
	dir := t.TempDir()
	cfg["exporters"] = map[string]any{"file": map[string]any{"path": filepath.Join(dir, "out.json")}, "prometheus/source_health": cfg["exporters"].(map[string]any)["prometheus/source_health"]}
	delete(cfg["processors"].(map[string]any), "k8sattributes")
	pipes := cfg["service"].(map[string]any)["pipelines"].(map[string]any)
	for name, p := range pipes {
		pm := p.(map[string]any)
		if name != "metrics/source_health" {
			pm["exporters"] = []any{"file"}
		}
		var keep []any
		for _, x := range pm["processors"].([]any) {
			if s := x.(string); s == "memory_limiter" || s == "filter/source_health" {
				keep = append(keep, x)
			}
		}
		pm["processors"] = keep
	}
	cfg["extensions"] = map[string]any{}
	cfg["service"].(map[string]any)["extensions"] = []any{}
	cfg["service"].(map[string]any)["telemetry"] = map[string]any{"metrics": map[string]any{"level": "none"}}
	b, _ := sigsyaml.Marshal(cfg)
	cf := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(cf, b, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "--config="+cf)
	cmd.Env = append(os.Environ(), "POD_IP=127.0.0.1", "NODE_NAME=n")
	logf, _ := os.Create(filepath.Join(dir, "log"))
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Signal(os.Interrupt); _ = cmd.Wait() }()

	var body string
	deadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		resp, err := http.Get("http://127.0.0.1:18889/metrics")
		if err != nil {
			continue
		}
		bb, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		body = string(bb)
		n := 0
		for _, l := range strings.Split(body, "\n") {
			if strings.HasPrefix(l, "up{") {
				n++
			}
		}
		if n >= 2 {
			break
		}
	}
	t.Logf("served on the source-health port:\n%s", body)
	if !strings.Contains(body, "up{") {
		logs, _ := os.ReadFile(filepath.Join(dir, "log"))
		t.Fatalf("nothing served; collector log:\n%.2500s", logs)
	}
	for _, line := range strings.Split(body, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// target_info is the exporter's own description of the target (its labels), not something the tool measured.
		if !strings.HasPrefix(line, "up{") && !strings.HasPrefix(line, "scrape_samples_scraped{") && !strings.HasPrefix(line, "target_info{") {
			t.Errorf("something other than up and the sample count is served: %q", line)
		}
		if strings.Contains(line, "kepler_node") {
			t.Errorf("what the tool measured is served: %q", line)
		}
	}
}
