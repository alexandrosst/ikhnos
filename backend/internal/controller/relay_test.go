package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"ikhnos/internal/relay"
)

// asked is what the agent writes under status.requested.
func asked(rev uint64, signals map[string]any, pct any) map[string]any {
	m := map[string]any{"revision": int64(rev)}
	if signals != nil {
		m["signals"] = signals
	}
	if pct != nil {
		m["tracesSamplingPercentage"] = int64(pct.(int))
	}
	return m
}

// request is the agent's write: it replaces status.requested and leaves the rest of the status alone.
func (g *rig) request(name string, req map[string]any) {
	g.t.Helper()
	u := g.get(name)
	st, _ := u.Object["status"].(map[string]any)
	if st == nil {
		st = map[string]any{}
	}
	st["requested"] = req
	u.Object["status"] = st
	if _, err := g.dyn.Resource(GVR).Namespace(ns).Update(context.Background(), u, metav1.UpdateOptions{}); err != nil {
		g.t.Fatal(err)
	}
}

// agentDeployment is the agent's Deployment, rolled out or not.
func (g *rig) agentDeployment(ready bool) {
	g.t.Helper()
	one := int32(1)
	lbl := map[string]string{"app.kubernetes.io/instance": ReleaseName, "app.kubernetes.io/part-of": "ikhnos"}
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: AgentDeployment, Namespace: ns, Labels: lbl}, Spec: appsv1.DeploymentSpec{Replicas: &one},
		Status: appsv1.DeploymentStatus{Replicas: 1, UpdatedReplicas: 1}}
	if ready {
		d.Status.AvailableReplicas = 1
	}
	ctx := context.Background()
	if _, err := g.kube.AppsV1().Deployments(ns).Get(ctx, AgentDeployment, metav1.GetOptions{}); err == nil {
		if _, err := g.kube.AppsV1().Deployments(ns).Update(ctx, d, metav1.UpdateOptions{}); err != nil {
			g.t.Fatal(err)
		}
		return
	}
	if _, err := g.kube.AppsV1().Deployments(ns).Create(ctx, d, metav1.CreateOptions{}); err != nil {
		g.t.Fatal(err)
	}
}

// relayRig is a controller that was given telemetry and the relay, with an agent installed from a resource that has telemetry of its own.
func relayRig(t *testing.T) *rig {
	t.Helper()
	g := newRig(t)
	g.r.AllowTelemetry, g.r.RelayTelemetry = true, true
	g.r.RelayDeadline, g.r.RelayPoll = 80*time.Millisecond, 5*time.Millisecond
	g.secret("enroll", "token", "ikh_abc")
	g.agentDeployment(true)
	g.agent("a", specWithTelemetry(someTelemetry))
	g.must("a")
	if g.status("a").Phase != PhaseReady || g.inst.applies() != 1 {
		t.Fatalf("the starting point: %+v, applies %d", g.status("a"), g.inst.applies())
	}
	return g
}

func telemetryOf(g *rig) map[string]any { return g.inst.lastApplied()["telemetry"].(map[string]any) }

func enabled(tel map[string]any, signal, modality string) bool {
	s, _ := tel[signal].(map[string]any)
	m, _ := s[modality].(map[string]any)
	b, _ := m["enabled"].(bool)
	return b
}

func TestARequestIsAppliedOverTheOwnersSpecAndTheDestinationStaysTheOwners(t *testing.T) {
	g := relayRig(t)
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, 20))
	g.must("a")
	st := g.status("a")
	if st.Phase != PhaseReady || st.Relay == nil || st.Relay.State != relay.Applied || st.Relay.Revision != 1 || g.inst.applies() != 2 {
		t.Fatalf("status %+v relay %+v, applies %d", st, st.Relay, g.inst.applies())
	}
	tel := telemetryOf(g)
	if !enabled(tel, "resourceUsage", "metrics") || !enabled(tel, "traces", "traces") {
		t.Fatalf("the signals asked for are not on: %v", tel)
	}
	if enabled(tel, "kubernetesEvents", "logs") {
		t.Fatalf("a signal the request does not name is still on: %v", tel)
	}
	otlp := tel["export"].(map[string]any)["otlp"].(map[string]any)
	if otlp["endpoint"] != "otel.example.com:4317" || otlp["auth"].(map[string]any)["secretName"] != "otel-token" {
		t.Fatalf("the destination changed: %v", otlp)
	}
	if got := tel["processors"].(map[string]any)["tracesSampling"].(map[string]any)["percentage"]; fmt.Sprint(got) != "20" {
		t.Fatalf("the share of the traces is not 20: %v", got)
	}

	// It stays in force: looking again, or the owner changing something else, does not undo it or install it twice.
	g.must("a")
	if g.inst.applies() != 2 || g.status("a").Relay.State != relay.Applied {
		t.Fatalf("applies %d, relay %+v", g.inst.applies(), g.status("a").Relay)
	}
	// The spec on the resource is the owner's still.
	if sig := g.get("a").Object["spec"].(map[string]any)["telemetry"].(map[string]any)["signals"].(map[string]any); sig["traces"] != nil {
		t.Fatalf("the request was written into the spec: %v", sig)
	}
}

func TestAnotherRequestReplacesTheLastAndTakingItBackReturnsTheClusterToItsOwnSettings(t *testing.T) {
	g := relayRig(t)
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	g.request("a", asked(2, map[string]any{"kubernetesEvents": true}, nil))
	g.must("a")
	tel := telemetryOf(g)
	if enabled(tel, "traces", "traces") || enabled(tel, "resourceUsage", "metrics") || !enabled(tel, "kubernetesEvents", "logs") {
		t.Fatalf("the second request did not replace the first: %v", tel)
	}
	g.request("a", asked(3, nil, nil))
	g.must("a")
	st := g.status("a")
	if st.Relay.Revision != 3 || st.Relay.State != relay.Applied || !strings.Contains(st.Relay.Reason, "taken back") {
		t.Fatalf("relay %+v", st.Relay)
	}
	tel = telemetryOf(g)
	if !enabled(tel, "resourceUsage", "metrics") || !enabled(tel, "kubernetesEvents", "logs") || enabled(tel, "traces", "traces") {
		t.Fatalf("the owner's own settings are not back: %v", tel)
	}
}

func TestARequestTheClusterCannotTakeIsRefusedInWordsAndIsNotTriedAgain(t *testing.T) {
	g := newRig(t)
	g.r.RelayTelemetry = true // the relay is on; telemetry (allowTelemetry) is not
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.must("a")
	g.request("a", asked(1, map[string]any{"traces": true}, nil))
	g.must("a")
	st := g.status("a")
	if st.Relay == nil || st.Relay.State != relay.Refused || !strings.Contains(st.Relay.Reason, "allowTelemetry") || g.inst.applies() != 1 {
		t.Fatalf("relay %+v, applies %d", st.Relay, g.inst.applies())
	}
	g.must("a")
	g.must("a")
	if g.inst.applies() != 1 || g.status("a").Relay.Revision != 1 {
		t.Fatalf("a refused request was tried again: applies %d", g.inst.applies())
	}
	// A request that turns nothing on is fine in a cluster that cannot take telemetry.
	g.request("a", asked(2, map[string]any{}, nil))
	g.must("a")
	if r := g.status("a").Relay; r.Revision != 2 || r.State != relay.Applied {
		t.Fatalf("relay %+v", r)
	}
}

// A cluster whose own resource cannot be installed (here: the owner asked for telemetry from a controller that was not given it) answers a
// request that it cannot change anything, rather than leaving it waiting.
func TestARequestToAResourceThatCannotBeInstalledIsRefusedAsSuch(t *testing.T) {
	g := relayRig(t)
	g.r.AllowTelemetry = false
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	st := g.status("a")
	if st.Phase != PhaseInvalid || st.Relay == nil || st.Relay.State != relay.Refused || !strings.Contains(st.Relay.Reason, "cannot be installed as it is") {
		t.Fatalf("status %+v relay %+v", st, st.Relay)
	}
}

func TestARequestWithNowhereToSendIsRefusedAndOnesTheTierCannotRunAre(t *testing.T) {
	g := newRig(t)
	g.r.AllowTelemetry, g.r.RelayTelemetry = true, true
	g.secret("enroll", "token", "ikh_abc")
	spec := goodSpec()
	spec["accessTier"] = int64(1)
	g.agent("a", spec) // no telemetry of its own: no destination
	g.must("a")
	g.request("a", asked(1, map[string]any{"resourceUsage": true}, nil))
	g.must("a")
	if r := g.status("a").Relay; r == nil || r.State != relay.Refused || !strings.Contains(r.Reason, "cannot take it") || g.inst.applies() != 1 {
		t.Fatalf("relay %+v, applies %d", r, g.inst.applies())
	}

	g.setSpec("a", func() map[string]any {
		s := specWithTelemetry(map[string]any{"signals": map[string]any{"resourceUsage": true}, "destination": map[string]any{"endpoint": "x:1"}})
		s["accessTier"] = int64(1)
		return s
	}())
	g.must("a")
	g.request("a", asked(2, map[string]any{"resourceUsage": true, "clusterLinks": true}, nil))
	g.must("a")
	if r := g.status("a").Relay; r.State != relay.Refused || !strings.Contains(r.Reason, "tier 2") {
		t.Fatalf("relay %+v", r)
	}
}

func TestARequestThatLeavesTheAgentNotReadyIsPutBackOnceAndWaitsForAnewerOne(t *testing.T) {
	g := relayRig(t)
	g.agentDeployment(false) // whatever is installed next does not come up
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	st := g.status("a")
	if st.Relay == nil || st.Relay.State != relay.Reverted || !strings.Contains(st.Relay.Reason, "not ready") || st.Phase != PhaseReady {
		t.Fatalf("relay %+v, phase %s", st.Relay, st.Phase)
	}
	if g.inst.applies() != 3 {
		t.Fatalf("applies %d: the change and the putting back", g.inst.applies())
	}
	if enabled(telemetryOf(g), "traces", "traces") {
		t.Fatalf("the cluster was not put back to the owner's settings: %v", telemetryOf(g))
	}
	// Not tried again, not by looking again and not by the agent writing the same request again.
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	g.must("a")
	if g.inst.applies() != 3 {
		t.Fatalf("a request that was put back was tried again: %d applies", g.inst.applies())
	}
	// A newer one is, and with the agent running it stays.
	g.agentDeployment(true)
	g.request("a", asked(2, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	if r := g.status("a").Relay; r.Revision != 2 || r.State != relay.Applied || !enabled(telemetryOf(g), "traces", "traces") {
		t.Fatalf("relay %+v", r)
	}
}

// The collectors are what a telemetry change is aimed at: an agent that is up beside collectors that are not is not a change that worked.
func TestARequestThatLeavesACollectorNotReadyIsPutBackEvenIfTheAgentIsUp(t *testing.T) {
	g := relayRig(t)
	lbl := map[string]string{"app.kubernetes.io/instance": ReleaseName, "app.kubernetes.io/part-of": "ikhnos"}
	if _, err := g.kube.AppsV1().DaemonSets(ns).Create(context.Background(), &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "ikhnos-telemetry-host", Namespace: ns, Labels: lbl},
		Status: appsv1.DaemonSetStatus{DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberReady: 2}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	if r := g.status("a").Relay; r == nil || r.State != relay.Reverted || !strings.Contains(r.Reason, "ikhnos-telemetry-host: 2 of 3 pods updated and ready") {
		t.Fatalf("relay %+v", r)
	}
}

func TestARequestHelmCannotInstallIsPutBackToo(t *testing.T) {
	g := relayRig(t)
	n := 0
	g.inst.mu.Lock()
	g.inst.applyErr = errors.New("rendered manifests contain a resource that already exists")
	g.inst.onApply = func() {
		if n++; n == 2 { // the first try fails; putting back works
			g.inst.applyErr = nil
		}
	}
	g.inst.mu.Unlock()
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	st := g.status("a")
	if st.Relay == nil || st.Relay.State != relay.Reverted || !strings.Contains(st.Relay.Reason, "already exists") || st.Phase != PhaseReady {
		t.Fatalf("relay %+v, phase %s", st.Relay, st.Phase)
	}
	if enabled(telemetryOf(g), "traces", "traces") {
		t.Fatal("the cluster was not put back")
	}
}

func TestWhenPuttingBackFailsToTheAnswerIsKeptAndTheRequestIsNotTriedAgain(t *testing.T) {
	g := relayRig(t)
	g.inst.mu.Lock()
	g.inst.applyErr = errors.New("the API server is down")
	g.inst.mu.Unlock()
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	if _, err := g.reconcile("a"); err == nil {
		t.Fatal("a release that could not be put back was not an error")
	}
	if r := g.status("a").Relay; r == nil || r.State != relay.Reverted || !strings.Contains(r.Reason, "failed") {
		t.Fatalf("relay %+v", r)
	}
	// The next pass goes on with the spec, as it does with any release that fails; it does not apply the request again.
	g.inst.mu.Lock()
	g.inst.applyErr = nil
	g.inst.mu.Unlock()
	g.must("a")
	if r := g.status("a").Relay; r.State != relay.Reverted || r.Revision != 1 || g.status("a").Phase != PhaseReady || enabled(telemetryOf(g), "traces", "traces") {
		t.Fatalf("relay %+v, phase %s", r, g.status("a").Phase)
	}
}

// A controller that stopped between installing a request and saying so finds it installed, and says so, without installing it again.
func TestAControllerThatStoppedBeforeSayingItAppliedARequestSaysSoWhenItStarts(t *testing.T) {
	g := relayRig(t)
	g.request("a", asked(1, map[string]any{"resourceUsage": true, "traces": true}, nil))
	g.must("a")
	// The answer is lost, as if the status write never happened.
	u := g.get("a")
	delete(u.Object["status"].(map[string]any), "relay")
	if _, err := g.dyn.Resource(GVR).Namespace(ns).Update(context.Background(), u, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	g.must("a")
	if r := g.status("a").Relay; r == nil || r.State != relay.Applied || r.Revision != 1 || g.inst.applies() != 2 {
		t.Fatalf("relay %+v, applies %d", r, g.inst.applies())
	}
}

func TestAControllerThatWasNotAskedToRelayReadsNoRequestAndLeavesTheAgentNotToPassOneOn(t *testing.T) {
	g := newRig(t)
	g.r.AllowTelemetry = true
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", specWithTelemetry(someTelemetry))
	g.must("a")
	if _, ok := g.inst.lastApplied()["controller"].(map[string]any)["relayTelemetry"]; ok {
		t.Fatalf("the agent was told to relay by a controller that does not: %v", g.inst.lastApplied()["controller"])
	}
	g.request("a", asked(1, map[string]any{"traces": true}, nil))
	g.must("a")
	if g.status("a").Relay != nil || g.inst.applies() != 1 {
		t.Fatalf("a controller that does not relay acted on a request: %+v, applies %d", g.status("a").Relay, g.inst.applies())
	}
	// One that does tells the agent so (and then reads the request that was waiting).
	g.r.RelayTelemetry = true
	g.r.RelayDeadline, g.r.RelayPoll = 80*time.Millisecond, 5*time.Millisecond
	g.agentDeployment(true)
	g.setSpec("a", specWithTelemetry(someTelemetry))
	g.must("a")
	if g.inst.lastApplied()["controller"].(map[string]any)["relayTelemetry"] != true {
		t.Fatalf("the agent was not told to relay: %v", g.inst.lastApplied()["controller"])
	}
}

var _ = unstructured.Unstructured{}
