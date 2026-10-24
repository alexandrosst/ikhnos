package controller

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const bootstrapName = "ikhnos-controller-bootstrap"

func (g *rig) bootstrapMap(spec string) {
	g.t.Helper()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: bootstrapName, Namespace: ns}, Data: map[string]string{BootstrapKey: spec}}
	if _, err := g.kube.CoreV1().ConfigMaps(ns).Create(context.Background(), cm, metav1.CreateOptions{}); err != nil {
		g.t.Fatal(err)
	}
	g.r.BootstrapConfigMap = bootstrapName
}

func (g *rig) setBootstrap(spec string) {
	g.t.Helper()
	cm, err := g.kube.CoreV1().ConfigMaps(ns).Get(context.Background(), bootstrapName, metav1.GetOptions{})
	if err != nil {
		g.t.Fatal(err)
	}
	cm.Data[BootstrapKey] = spec
	if _, err := g.kube.CoreV1().ConfigMaps(ns).Update(context.Background(), cm, metav1.UpdateOptions{}); err != nil {
		g.t.Fatal(err)
	}
}

const goodBootstrap = `{"server":{"address":"ikhnos.example.com:8443","caPin":"sha256/AbCd=="},"enrollment":{"tokenSecretRef":{"name":"ikhnos-enrollment"}},"accessTier":1}`

func TestTheBootstrapMakesTheResourceOnceAndSaysSo(t *testing.T) {
	g := newRig(t)
	g.bootstrapMap(goodBootstrap)
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	u := g.get("this-cluster")
	spec := u.Object["spec"].(map[string]any)
	if spec["accessTier"] != int64(1) && spec["accessTier"] != float64(1) {
		t.Errorf("accessTier = %v", spec["accessTier"])
	}
	if got := spec["server"].(map[string]any)["address"]; got != "ikhnos.example.com:8443" {
		t.Errorf("address = %v", got)
	}
	cm, _ := g.kube.CoreV1().ConfigMaps(ns).Get(context.Background(), bootstrapName, metav1.GetOptions{})
	if cm.Annotations[BootstrapApplied] == "" {
		t.Fatal("the ConfigMap does not say the agent was asked for")
	}

	// A person deletes the resource: it is not made again, because the same text has already been asked for.
	if err := g.dyn.Resource(GVR).Namespace(ns).Delete(context.Background(), "this-cluster", metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.dyn.Resource(GVR).Namespace(ns).Get(context.Background(), "this-cluster", metav1.GetOptions{}); err == nil {
		t.Fatal("a deleted resource was made again from a bootstrap that had been applied")
	}
}

func TestANewBootstrapChangesWhatItStatesAndKeepsTheRest(t *testing.T) {
	g := newRig(t)
	g.bootstrapMap(goodBootstrap)
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	// A person has since turned telemetry on in the resource.
	u := g.get("this-cluster")
	u.Object["spec"].(map[string]any)["telemetry"] = map[string]any{"signals": map[string]any{"resourceUsage": true}}
	if _, err := g.dyn.Resource(GVR).Namespace(ns).Update(context.Background(), u, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// The command is run again, for tier 0 and without the first one's accessTier line replaced by anything else.
	g.setBootstrap(`{"server":{"address":"ikhnos.example.com:8443","caPin":"sha256/AbCd=="},"enrollment":{"tokenSecretRef":{"name":"ikhnos-enrollment"}},"accessTier":0}`)
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	spec := g.get("this-cluster").Object["spec"].(map[string]any)
	if tier := spec["accessTier"]; tier != int64(0) && tier != float64(0) {
		t.Errorf("accessTier = %v, want 0", tier)
	}
	if _, ok := spec["telemetry"]; !ok {
		t.Error("the telemetry a person set was removed by a second bootstrap")
	}
	// And a field the second one no longer states is removed from the resource.
	g.setBootstrap(`{"server":{"address":"ikhnos.example.com:8443","caPin":"sha256/AbCd=="},"enrollment":{"tokenSecretRef":{"name":"ikhnos-enrollment"}}}`)
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.get("this-cluster").Object["spec"].(map[string]any)["accessTier"]; ok {
		t.Error("accessTier stayed after a bootstrap that does not state it")
	}
}

func TestABootstrapThatCannotBeInstalledFromIsAnErrorAndIsNotRecorded(t *testing.T) {
	g := newRig(t)
	g.bootstrapMap(`{"server":{"address":"no-port","caPin":"sha256/x"}}`)
	err := g.r.Bootstrap(context.Background())
	if err == nil || !strings.Contains(err.Error(), "host:port") {
		t.Fatalf("err = %v", err)
	}
	cm, _ := g.kube.CoreV1().ConfigMaps(ns).Get(context.Background(), bootstrapName, metav1.GetOptions{})
	if cm.Annotations[BootstrapApplied] != "" {
		t.Error("an invalid bootstrap was recorded as applied")
	}
	// A field the resource does not have is refused too, not dropped.
	g.setBootstrap(`{"server":{"address":"h:8443","caPin":"sha256/x"},"surprise":true}`)
	if err := g.r.Bootstrap(context.Background()); err == nil {
		t.Error("an unknown field was accepted")
	}
}

func TestWithNoBootstrapMapTheControllerAsksForNothing(t *testing.T) {
	g := newRig(t)
	g.r.BootstrapConfigMap = bootstrapName
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.r.BootstrapConfigMap = ""
	if err := g.r.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	list, err := g.dyn.Resource(GVR).Namespace(ns).List(context.Background(), metav1.ListOptions{})
	if err != nil || len(list.Items) != 0 {
		t.Fatalf("%v %v", list, err)
	}
}
