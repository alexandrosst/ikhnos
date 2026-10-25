package controller

import (
	"context"
	"fmt"
	"strconv"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	// SchemaRevisionAnnotation is the annotation of the IkhnosAgent definition that says which revision of it that is, and SchemaRevision is
	// the revision this controller is built for. Helm installs the definition once and never upgrades it, so a controller that was upgraded
	// can run against an older one: the API server then drops, without a word, the fields of the resource it does not know (a destination
	// over mutual TLS, a status the controller writes). A test holds the number to the file and to a hash of its schema.
	SchemaRevisionAnnotation = "ikhnos.io/schema-revision"
	SchemaRevision           = 1

	// ConditionDefinitionCurrent is true when the definition in this cluster is at least as new as the controller. It is not part of Ready:
	// an old definition does not stop the release from being installed as the spec asks, it stops the spec from saying everything.
	ConditionDefinitionCurrent = "DefinitionCurrent"
)

var definitionGVR = schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}

// definitionRevision is the revision of the definition in this cluster: 0 for one from before revisions (no annotation), and ok false when
// it cannot be read (not there, or this controller is not allowed to read it), which says nothing either way.
func (r *Reconciler) definitionRevision(ctx context.Context) (rev int, ok bool) {
	crd, err := r.Dyn.Resource(definitionGVR).Get(ctx, Resource+"."+Group, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) && !apierrors.IsForbidden(err) {
			r.log().Debug("the definition of IkhnosAgent could not be read", "err", err)
		}
		return 0, false
	}
	n, _ := strconv.Atoi(crd.GetAnnotations()[SchemaRevisionAnnotation])
	return n, true
}

// definitionCondition says whether the definition of the resource in this cluster is as new as the controller; nil when that is not known.
func (r *Reconciler) definitionCondition(ctx context.Context, generation int64) *Condition {
	rev, ok := r.definitionRevision(ctx)
	if !ok {
		return nil
	}
	if rev >= SchemaRevision {
		r.warnedRevision = 0
		return &Condition{Type: ConditionDefinitionCurrent, Status: "True", Reason: "Current", Message: fmt.Sprintf("the definition of IkhnosAgent is revision %d, as new as this controller needs", rev), ObservedGeneration: generation}
	}
	msg := fmt.Sprintf("the definition of IkhnosAgent in this cluster is revision %d and this controller is built for %d: the API server drops the fields it does not know from this resource without saying so (a destination over mutual TLS, for one). Helm never upgrades a definition: apply the one in the controller's chart, `helm show crds <the controller's chart> | kubectl apply --server-side --force-conflicts -f -`", rev, SchemaRevision)
	if r.warnedRevision != rev+1 {
		r.warnedRevision = rev + 1
		r.log().Warn(msg)
	}
	return &Condition{Type: ConditionDefinitionCurrent, Status: "False", Reason: "DefinitionOlderThanController", Message: msg, ObservedGeneration: generation}
}
