package chart

import (
	"strings"
	"testing"
)

func toolsEnv(t *testing.T, extra ...string) (string, bool) {
	t.Helper()
	r := render(t, extra...)
	return env(r.deployments["ikhnos-agent"].Spec.Template.Spec.Containers[0], "IKHNOS_TELEMETRY_TOOLS")
}

// The agent reports which version of each telemetry tool the release deploys, from the same image values the pods are rendered with.
func TestTelemetryToolsNameOnlyWhatTheReleaseDeploys(t *testing.T) {
	base := []string{"--set", "telemetry.export.otlp.endpoint=otel.example:4317", "--set", "telemetry.resourceUsage.metrics.enabled=true"}

	if got, ok := toolsEnv(t, base...); !ok || got != "collector=0.160.0" {
		t.Errorf("IKHNOS_TELEMETRY_TOOLS = %q (set %v), want only the collector", got, ok)
	}

	energy := append(append([]string{}, base...), "--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.energy.metrics.source=bundle-kepler")
	if got, _ := toolsEnv(t, energy...); got != "collector=0.160.0,kepler=release-0.7.12/ebpf" {
		t.Errorf("with Kepler: %q", got)
	}
	if got, _ := toolsEnv(t, append(append([]string{}, energy...), "--set", "telemetry.energy.metrics.engine=powercap")...); got != "collector=0.160.0,kepler=v0.12.0/powercap" {
		t.Errorf("with Kepler on the powercap engine: %q", got)
	}

	both := append(append([]string{}, energy...), "--set", "telemetry.accelerators.metrics.enabled=true", "--set", "telemetry.accelerators.metrics.source=bundle-dcgm")
	if got, _ := toolsEnv(t, both...); !strings.HasSuffix(got, ",dcgm=4.6.0-4.8.3-distroless") {
		t.Errorf("with the DCGM exporter: %q", got)
	}

	// Energy from an existing exporter deploys no Kepler, so none is named.
	existing := append(append([]string{}, base...), "--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.energy.metrics.source=existing", "--set", "telemetry.energy.metrics.existing.prometheusEndpoint=kepler.example:9102")
	if got, _ := toolsEnv(t, existing...); got != "collector=0.160.0" {
		t.Errorf("energy from an existing exporter: %q, want no Kepler", got)
	}
}

// A digest wins over the tag in the image reference, so the tag is not offered as the version.
func TestTelemetryToolsSaysWhenADigestPinsTheImage(t *testing.T) {
	d := "sha256:" + strings.Repeat("a", 64)
	got, _ := toolsEnv(t, "--set", "telemetry.export.otlp.endpoint=otel.example:4317", "--set", "telemetry.resourceUsage.metrics.enabled=true",
		"--set", "telemetry.collectorImage.digest="+d,
		"--set", "telemetry.energy.metrics.enabled=true", "--set", "telemetry.energy.metrics.source=bundle-kepler", "--set", "telemetry.energy.metrics.keplerImage.digest="+d)
	if got != "collector=@,kepler=@/ebpf" {
		t.Errorf("IKHNOS_TELEMETRY_TOOLS = %q, want the digest-pinned tools marked @", got)
	}
}

// No telemetry at all: no collector is deployed, and the variable is absent rather than empty.
func TestTelemetryToolsAbsentWithoutTelemetry(t *testing.T) {
	if got, ok := toolsEnv(t); ok {
		t.Errorf("IKHNOS_TELEMETRY_TOOLS = %q, want it absent when no telemetry is on", got)
	}
}
