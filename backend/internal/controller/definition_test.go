package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strconv"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// schemaHash is what the revision is held to. When this test fails the schema changed: raise SchemaRevision, the annotation of the file
// (crds/ikhnosagents.yaml) and this hash together, so that a cluster that kept an older definition is told so.
const schemaHash = "ee5ce31836ebb06ea1778c8e66bbb1a1236b428905cc2fa0574fe4912e3299bd"

func TestTheRevisionOfTheDefinitionIsTheControllersAndChangesWithItsSchema(t *testing.T) {
	_, _, _, crd := loadCRD(t)
	got := dig(t, crd, "metadata", "annotations").(map[string]any)[SchemaRevisionAnnotation]
	if got != strconv.Itoa(SchemaRevision) {
		t.Fatalf("the definition says it is revision %v and the controller is built for %d", got, SchemaRevision)
	}
	b, err := json.Marshal(dig(t, crd, "spec"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	if h := hex.EncodeToString(sum[:]); h != schemaHash {
		t.Fatalf("the schema of the definition changed (hash %s, not %s): raise SchemaRevision (now %d), the %s annotation of crds/ikhnosagents.yaml and schemaHash together", h, schemaHash, SchemaRevision, SchemaRevisionAnnotation)
	}
}

func (g *rig) definition(annotations map[string]any) {
	g.t.Helper()
	meta := map[string]any{"name": Resource + "." + Group}
	if annotations != nil {
		meta["annotations"] = annotations
	}
	u := &unstructured.Unstructured{Object: map[string]any{"apiVersion": "apiextensions.k8s.io/v1", "kind": "CustomResourceDefinition", "metadata": meta}}
	if _, err := g.dyn.Resource(definitionGVR).Create(context.Background(), u, metav1.CreateOptions{}); err != nil {
		g.t.Fatal(err)
	}
}

func (g *rig) definitionCondition(name string) *Condition {
	for _, c := range g.status(name).Conditions {
		if c.Type == ConditionDefinitionCurrent {
			return &c
		}
	}
	return nil
}

func TestAnOlderDefinitionOfTheResourceIsSaidInItsStatusAndOnceInTheLog(t *testing.T) {
	var log strings.Builder
	g := newRig(t)
	g.r.Log = slog.New(slog.NewTextHandler(&log, nil))
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.definition(nil) // one from before revisions: no annotation
	g.must("a")
	c := g.definitionCondition("a")
	if c == nil || c.Status != "False" || c.Reason != "DefinitionOlderThanController" || !strings.Contains(c.Message, "revision 0") || !strings.Contains(c.Message, "kubectl apply --server-side") {
		t.Fatalf("condition %+v", c)
	}
	if got := g.status("a").Conditions[0]; got.Type != "Ready" || got.Status != "True" {
		t.Fatalf("an old definition does not stop the release from being installed: %+v", got)
	}
	g.must("a")
	g.must("a")
	if n := strings.Count(log.String(), "built for"); n != 1 {
		t.Fatalf("the log said it %d times, and should once:\n%s", n, log.String())
	}
}

func TestACurrentDefinitionIsSaidToBeAndOneThatCannotBeReadIsNotSaidAnything(t *testing.T) {
	g := newRig(t)
	g.secret("enroll", "token", "ikh_abc")
	g.agent("a", goodSpec())
	g.must("a")
	if c := g.definitionCondition("a"); c != nil {
		t.Fatalf("with no definition to read nothing is said: %+v", c)
	}
	g.definition(map[string]any{SchemaRevisionAnnotation: strconv.Itoa(SchemaRevision)})
	g.must("a")
	if c := g.definitionCondition("a"); c == nil || c.Status != "True" {
		t.Fatalf("condition %+v", c)
	}
}
