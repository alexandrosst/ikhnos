package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"
)

var releaseLabels = func(component string) map[string]string {
	l := map[string]string{"app.kubernetes.io/instance": ReleaseName, "app.kubernetes.io/part-of": "ikhnos"}
	if component != "" {
		l["app.kubernetes.io/component"] = component
	}
	return l
}

func (g *rig) daemonSet(name, component string, desired, ready int32) {
	g.t.Helper()
	_, err := g.kube.AppsV1().DaemonSets(ns).Create(context.Background(), &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: releaseLabels(component), Generation: 1},
		Status:     appsv1.DaemonSetStatus{ObservedGeneration: 1, DesiredNumberScheduled: desired, UpdatedNumberScheduled: ready, NumberReady: ready},
	}, metav1.CreateOptions{})
	if err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) setDaemonSetReady(name string, ready int32) {
	g.t.Helper()
	d, err := g.kube.AppsV1().DaemonSets(ns).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		g.t.Fatal(err)
	}
	d.Status.NumberReady, d.Status.UpdatedNumberScheduled = ready, ready
	if _, err := g.kube.AppsV1().DaemonSets(ns).Update(context.Background(), d, metav1.UpdateOptions{}); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) deployment(name string, replicas, ready int32) {
	g.t.Helper()
	_, err := g.kube.AppsV1().Deployments(ns).Create(context.Background(), &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: releaseLabels(""), Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
		Status:     appsv1.DeploymentStatus{ObservedGeneration: 1, Replicas: replicas, UpdatedReplicas: ready, AvailableReplicas: ready},
	}, metav1.CreateOptions{})
	if err != nil {
		g.t.Fatal(err)
	}
}

func condition(st Status, typ string) *Condition {
	for i := range st.Conditions {
		if st.Conditions[i].Type == typ {
			return &st.Conditions[i]
		}
	}
	return nil
}

func TestAReleaseWithAPodHeldBackSaysWhichWorkloadAndStaysInstalled(t *testing.T) {
	g := newRig(t)
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.deployment("ikhnos-agent", 1, 1)
	g.daemonSet("ikhnos-telemetry-kepler", "telemetry-kepler", 3, 2)
	g.daemonSet("ikhnos-telemetry-host", "telemetry-host", 3, 3)

	again := g.must("a")
	st := g.status("a")
	// The release is installed as the spec asks: a pod held back on one node does not change that.
	if st.Phase != PhaseReady || condition(st, "Ready").Status != "True" {
		t.Fatalf("status %+v", st)
	}
	c := condition(st, ConditionWorkloadsReady)
	if c == nil || c.Status != "False" || c.Reason != "PodsNotReady" || c.Message != "ikhnos-telemetry-kepler: 2 of 3 pods ready" {
		t.Fatalf("WorkloadsReady %+v", c)
	}
	if len(st.Workloads) != 3 || st.Workloads[0].Name != "ikhnos-agent" || st.Workloads[0].Kind != "Deployment" {
		t.Fatalf("workloads %+v", st.Workloads)
	}
	var kepler Workload
	for _, w := range st.Workloads {
		if w.Name == "ikhnos-telemetry-kepler" {
			kepler = w
		}
	}
	if kepler.Kind != "DaemonSet" || kepler.Component != "telemetry-kepler" || kepler.Wanted != 3 || kepler.Ready != 2 {
		t.Fatalf("kepler %+v", kepler)
	}
	// It is looked at again soon while something is not ready.
	if again != observeWhileWaiting {
		t.Fatalf("looked at again after %v", again)
	}

	// The pod comes up: the condition turns true, the time of the change is the time of the change, and the next look is the slow one.
	g.advance(time.Minute)
	g.setDaemonSetReady("ikhnos-telemetry-kepler", 3)
	again = g.must("a")
	st = g.status("a")
	c = condition(st, ConditionWorkloadsReady)
	if c.Status != "True" || c.Reason != "AllPodsReady" {
		t.Fatalf("WorkloadsReady %+v", c)
	}
	if again != DefaultObserveEvery {
		t.Fatalf("looked at again after %v", again)
	}
	if ready := condition(st, "Ready"); ready.LastTransitionTime == c.LastTransitionTime {
		t.Fatalf("both conditions changed at %s: the later one must have its own time", c.LastTransitionTime)
	}

	// Nothing changed: nothing is written.
	writes := g.statusWrites()
	g.must("a")
	g.must("a")
	if g.statusWrites() != writes {
		t.Fatalf("an unchanged release was written to %d more times", g.statusWrites()-writes)
	}
}

func TestAChangeNotYetRolledOutIsSaidAsThatAndNotAsAMissingPod(t *testing.T) {
	g := newRig(t)
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.deployment("ikhnos-agent", 1, 1)
	d, _ := g.kube.AppsV1().DaemonSets(ns).Create(context.Background(), &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "ikhnos-telemetry-host", Namespace: ns, Labels: releaseLabels("telemetry-host"), Generation: 2},
		Status:     appsv1.DaemonSetStatus{ObservedGeneration: 1, DesiredNumberScheduled: 3, UpdatedNumberScheduled: 3, NumberReady: 3},
	}, metav1.CreateOptions{})
	_ = d
	g.must("a")
	c := condition(g.status("a"), ConditionWorkloadsReady)
	if c == nil || c.Status != "False" || !strings.Contains(c.Message, "ikhnos-telemetry-host: a change is being rolled out (3 of 3 pods ready)") {
		t.Fatalf("WorkloadsReady %+v", c)
	}
}

func TestWithNothingToObserveOrObservingOffNoWorkloadsAreWrittenAndNoLookIsAsked(t *testing.T) {
	g := newRig(t)
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	// Nothing labelled as the release's is there (as in a cluster that has not made the pods yet): nothing is said, which is not "ready".
	if again := g.must("a"); again != DefaultObserveEvery && again != observeWhileWaiting {
		t.Fatalf("looked at again after %v", again)
	}
	st := g.status("a")
	if len(st.Workloads) != 0 || condition(st, ConditionWorkloadsReady) != nil || len(st.Conditions) != 1 {
		t.Fatalf("status %+v", st)
	}

	g.r.ObserveEvery = -1
	g.deployment("ikhnos-agent", 1, 0)
	if again := g.must("a"); again != 0 {
		t.Fatalf("looked at again after %v with observing off", again)
	}
	if st := g.status("a"); len(st.Workloads) != 0 || condition(st, ConditionWorkloadsReady) != nil {
		t.Fatalf("status %+v", st)
	}
}

func TestWhatIsNotTheReleasesIsNotObserved(t *testing.T) {
	g := newRig(t)
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.deployment("ikhnos-agent", 1, 1)
	_, err := g.kube.AppsV1().DaemonSets(ns).Create(context.Background(), &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "someone-elses", Namespace: ns, Labels: map[string]string{"app": "x"}},
		Status:     appsv1.DaemonSetStatus{DesiredNumberScheduled: 5},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g.must("a")
	for _, w := range g.status("a").Workloads {
		if w.Name == "someone-elses" {
			t.Fatalf("observed %+v", w)
		}
	}
}

// An API server that does not answer for a moment must not take the account of the pods away from the status.
func TestWhenThePodsCannotBeListedTheLastAccountStands(t *testing.T) {
	g := newRig(t)
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.deployment("ikhnos-agent", 1, 1)
	g.daemonSet("ikhnos-telemetry-host", "telemetry-host", 3, 2)
	g.must("a")
	before := g.status("a")
	if len(before.Workloads) != 2 || condition(before, ConditionWorkloadsReady) == nil {
		t.Fatalf("the starting point: %+v", before)
	}
	g.kube.PrependReactor("list", "daemonsets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("etcdserver: request timed out")
	})
	again := g.must("a")
	after := g.status("a")
	if len(after.Workloads) != 2 || condition(after, ConditionWorkloadsReady) == nil || condition(after, ConditionWorkloadsReady).Message != condition(before, ConditionWorkloadsReady).Message {
		t.Fatalf("the account was lost when the list failed: %+v", after)
	}
	if again == 0 {
		t.Fatal("the pods are not looked at again after a failed look")
	}
}

func TestTheWorkloadsMessageIsCutByCharacters(t *testing.T) {
	var ws []Workload
	for i := 0; i < 40; i++ {
		ws = append(ws, Workload{Name: strings.Repeat("é", 20) + string(rune('a'+i%26)), Wanted: 3, Ready: 1})
	}
	c := workloadsCondition(1, ws)
	if c == nil || !utf8.ValidString(c.Message) || utf8.RuneCountInString(c.Message) > maxWorkloadMessage {
		t.Fatalf("condition %+v", c)
	}
}
