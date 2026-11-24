package chart

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// A series of a whole-node exporter (Kepler's energy, a GPU exporter's) is the node's: the pod a GPU is allocated to, or
// whose energy a series measures, is on the data point, and a namespace on the one resource per node is the scraped
// exporter pod's or a data point's copied up. transform/node_series takes every namespace, pod and workload attribute off
// those records, whichever way the exporter is reached, last in metrics/infra (after the person's own processors), so
// FUSION never shows a node's series to a token limited to a namespace they do not belong to. The node stays.
func TestWholeNodeExporterSeriesCarryNoNamespaceOrPod(t *testing.T) {
	gone := []string{"k8s.namespace.name", "k8s.pod.name", "k8s.pod.uid", "k8s.container.name", "k8s.daemonset.name",
		"k8s.deployment.name", "k8s.replicaset.name", "k8s.statefulset.name", "k8s.job.name", "k8s.cronjob.name"}
	otlp := []string{"--set", "telemetry.export.otlp.endpoint=x:4317"}
	energyExisting := append(slices.Clone(otlp), "--set", "telemetry.energy.metrics.enabled=true",
		"--set", "telemetry.energy.metrics.source=existing",
		"--set", "telemetry.energy.metrics.existing.prometheusEndpoint=k:9102")
	accelAddress := append(slices.Clone(otlp), "--set", "telemetry.accelerators.metrics.enabled=true",
		"--set", "telemetry.accelerators.metrics.source=existing",
		"--set", "telemetry.accelerators.metrics.existing.prometheusEndpoint=gpu:9400")
	extra := []string{"--set-json", `telemetry.processors.extraProcessors={"transform/mine":{"metric_statements":[]}}`,
		"--set", "telemetry.processors.extraProcessorNames[0]=transform/mine"}
	for name, c := range map[string]struct {
		args []string
		jobs []string
	}{
		"bundled Kepler":            {energyOn, []string{"kepler"}},
		"an existing energy source": {energyExisting, []string{"energy-existing"}},
		"bundled dcgm-exporter":     {accelOn, []string{"dcgm-exporter"}},
		"existing exporter pods":    {accelOperator, []string{"dcgm-exporter"}},
		"one exporter address":      {accelAddress, []string{"accelerators-existing"}},
		"energy and accelerators":   {both(), []string{"kepler", "dcgm-exporter"}},
	} {
		r := render(t, append(slices.Clone(c.args), extra...)...)
		cfg := otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)
		tr, ok := cfg["processors"].(map[string]any)["transform/node_series"]
		if !ok {
			t.Errorf("%s: no transform/node_series", name)
			continue
		}
		b, _ := json.Marshal(tr)
		var gate []string
		for _, j := range c.jobs {
			gate = append(gate, fmt.Sprintf(`attributes[\"service.name\"] == \"%s\"`, j))
		}
		for _, a := range gone {
			want := `delete_key(attributes, \"` + a + `\") where ` + strings.Join(gate, " or ")
			if !strings.Contains(string(b), want) {
				t.Errorf("%s: transform/node_series does not have %s: %s", name, want, b)
			}
		}
		if strings.Contains(string(b), "k8s.node.name") {
			t.Errorf("%s: the node is taken off as well: %s", name, b)
		}
		pipes, _ := cfg["service"].(map[string]any)["pipelines"].(map[string]any)
		pipe, _ := pipes["metrics/infra"].(map[string]any)["processors"].([]any)
		at := func(p string) int { return slices.Index(pipe, any(p)) }
		if at("transform/node_series") < 0 || at("transform/node_series") < at("transform/mine") ||
			at("transform/node_series") > at("transform/ikhnos_source_infra") {
			t.Errorf("%s: transform/node_series is not last before the source tag, after the person's own: %v", name, pipe)
		}
	}
	// With no whole-node exporter there is nothing to take off.
	r := render(t, append(slices.Clone(otlp), "--set", "telemetry.kubernetesState.metrics.enabled=true")...)
	cfg := otelConfig(t, r.configmaps["ikhnos-telemetry-cluster-config"].Data)
	if _, has := cfg["processors"].(map[string]any)["transform/node_series"]; has {
		t.Error("transform/node_series rendered with neither energy nor accelerators")
	}
}
