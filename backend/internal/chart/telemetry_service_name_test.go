package chart

import (
	"slices"
	"strings"
	"testing"
)

// pipelinesOf maps each pipeline of a collector config to its processors.
func pipelinesOf(t *testing.T, r rendered, cm string) (map[string][]string, map[string]any) {
	t.Helper()
	c, ok := r.configmaps[cm]
	if !ok {
		t.Fatalf("no %s", cm)
	}
	cfg := otelConfig(t, c.Data)
	svc, _ := cfg["service"].(map[string]any)
	pl, _ := svc["pipelines"].(map[string]any)
	out := map[string][]string{}
	for name, p := range pl {
		procs, _ := p.(map[string]any)["processors"].([]any)
		for _, x := range procs {
			out[name] = append(out[name], x.(string))
		}
	}
	procs, _ := cfg["processors"].(map[string]any)
	return out, procs
}

// Every pipeline that finds the pod (k8sattributes) also names its service from the pod's workload, after the scope
// filters and before anything else touches the record; turned off, it is nowhere.
func TestEveryPipelineThatFindsThePodNamesItsServiceFromTheWorkload(t *testing.T) {
	every := append(append([]string{}, agentTel...), allSignals...)
	every = append(every, "--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.applicationMetrics.metrics.enabled=true",
		"--set", "telemetry.applicationLogs.logs.enabled=true", "--set", "telemetry.scope.namespaces[0]=shop")
	r := render(t, every...)
	for _, cm := range []string{"ikhnos-telemetry-host-config", "ikhnos-telemetry-cluster-config"} {
		pl, procs := pipelinesOf(t, r, cm)
		seen := 0
		for name, list := range pl {
			k := slices.Index(list, "k8sattributes")
			if k < 0 {
				continue
			}
			seen++
			s := slices.Index(list, "transform/service_name")
			if s < k {
				t.Errorf("%s %s: transform/service_name must follow k8sattributes: %v", cm, name, list)
			}
			for i, p := range list {
				if strings.HasPrefix(p, "filter/scope") && i > s {
					t.Errorf("%s %s: the scope filter %s runs after transform/service_name: %v", cm, name, p, list)
				}
				if (p == "redaction" || p == "resourcedetection" || strings.HasPrefix(p, "transform/ikhnos_source")) && i < s {
					t.Errorf("%s %s: %s runs before transform/service_name: %v", cm, name, p, list)
				}
			}
		}
		if seen == 0 {
			t.Fatalf("%s: no pipeline with k8sattributes", cm)
		}
		def, _ := procs["transform/service_name"].(map[string]any)
		for _, k := range []string{"metric_statements", "log_statements", "trace_statements"} {
			if def[k] == nil {
				t.Errorf("%s: transform/service_name has no %s: %v", cm, k, def)
			}
		}
	}

	r = render(t, append(every, "--set", "telemetry.processors.serviceNameFromWorkload.enabled=false")...)
	for _, cm := range []string{"ikhnos-telemetry-host-config", "ikhnos-telemetry-cluster-config"} {
		pl, procs := pipelinesOf(t, r, cm)
		for name, list := range pl {
			if slices.Contains(list, "transform/service_name") {
				t.Errorf("turned off, %s %s still runs it: %v", cm, name, list)
			}
		}
		if _, ok := procs["transform/service_name"]; ok {
			t.Errorf("turned off, %s still defines it", cm)
		}
	}
}

// A scraped series is told from a pushed one by the receiver that produced it: before, the job name was quoted twice in
// the condition and never matched, so every scraped series was labelled otlp.
func TestAScrapedSeriesIsLabelledByItsReceiverNotByItsName(t *testing.T) {
	r := render(t, withTel("--set", "telemetry.applicationMetrics.metrics.enabled=true",
		"--set", "telemetry.applicationMetrics.metrics.scrapeTargets[0].jobName=web", "--set", "telemetry.applicationMetrics.metrics.scrapeTargets[0].namespace=shop",
		"--set", "telemetry.applicationMetrics.metrics.scrapeTargets[0].podLabelSelector=app=web", "--set", "telemetry.applicationMetrics.metrics.scrapeTargets[0].port=8080")...)
	cfg := r.configmaps["ikhnos-telemetry-cluster-config"].Data["otel-collector-config.yaml"]
	if !strings.Contains(cfg, `set(resource.attributes["ikhnos.source"], "prometheus-scrape") where IsMatch(scope.name, "prometheusreceiver")`) {
		t.Fatalf("no scope-based prometheus-scrape rule:\n%s", cfg)
	}
	if strings.Contains(cfg, `== "\"web\""`) {
		t.Fatal("the job name is quoted twice")
	}
}

// Kepler's resource is the exporter's own pod, and its workload names would put every node's energy in the exporter's
// DaemonSet: they are dropped, as dcgm-exporter's are.
func TestKeplersResourceDoesNotKeepTheExportersWorkload(t *testing.T) {
	r := render(t, withTel("--set", "telemetry.energy.metrics.enabled=true")...)
	cfg := r.configmaps["ikhnos-telemetry-cluster-config"].Data["otel-collector-config.yaml"]
	for _, a := range []string{"k8s.daemonset.name", "k8s.deployment.name", "k8s.statefulset.name"} {
		if !strings.Contains(cfg, `delete_key(attributes, "`+a+`") where attributes["service.name"] == "kepler"`) {
			t.Errorf("kepler keeps %s", a)
		}
	}
}
