package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"ikhnos/internal/install"
)

// What the controller chart gives the controller to ask for an agent with, so that one `helm install` of the controller is all a person
// runs. The chart writes the resource's spec into a ConfigMap (and the token into the Secret the spec names); the controller makes the
// IkhnosAgent from it once. Everything after that is the resource's: it is edited and deleted like any other.
const (
	// BootstrapKey is the key of the ConfigMap that holds the spec, as JSON, in the resource's own field names.
	BootstrapKey = "spec.json"
	// BootstrapApplied is the annotation of the ConfigMap that holds the digest of the spec the controller last made the resource from.
	// It is why a resource a person deletes is not made again at the next start: the same text has already been asked for.
	BootstrapApplied = "ikhnos.io/bootstrap-applied"
	// BootstrapRetry is how long to wait before trying again when the resource could not be made yet (the definition of the resource is
	// still being established, say).
	BootstrapRetry = 10 * time.Second
)

// bootstrapOwned are the fields of a resource's spec that the bootstrap states. A second bootstrap changes these and leaves the rest
// (the telemetry a person has set since, above all) as it is.
var bootstrapOwned = []string{"server", "enrollment", "accessTier", "collectors", "scope", "purgeIdentityOnDelete", "image"}

// Bootstrap makes the IkhnosAgent that BootstrapConfigMap asks for, and says so by annotating the ConfigMap. It does nothing when there
// is no such ConfigMap, when it holds nothing, or when the spec in it is the one it already made the resource from. When the resource
// is there already (a second install with another token or other choices), the fields the bootstrap states are set to what it says.
// An invalid spec is an error and is not recorded, so that putting it right is enough.
func (r *Reconciler) Bootstrap(ctx context.Context) error {
	if r.BootstrapConfigMap == "" {
		return nil
	}
	cms := r.Kube.CoreV1().ConfigMaps(r.Namespace)
	cm, err := cms.Get(ctx, r.BootstrapConfigMap, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	raw := strings.TrimSpace(cm.Data[BootstrapKey])
	if raw == "" {
		return nil
	}
	var spec Spec
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&spec); err != nil {
		return fmt.Errorf("the bootstrap spec in %s is not an IkhnosAgent spec: %w", r.BootstrapConfigMap, err)
	}
	if p := spec.Problems(); len(p) > 0 {
		return fmt.Errorf("the bootstrap spec in %s cannot be installed from: %s", r.BootstrapConfigMap, strings.Join(p, "; "))
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return err
	}
	// What was asked for is the spec and the token together: the same spec with a new token is a new request. It is how a person connects a
	// cluster again after the server revoked its agent (the controller removed it): the same command with a new token asks again.
	digest, err := r.bootstrapDigest(ctx, raw, spec)
	if err != nil {
		return err
	}
	if cm.Annotations[BootstrapApplied] == digest {
		// Said out loud, because a person who sees a controller and no agent has nowhere else to learn why: the same request (this spec,
		// this token) was already carried out, and what a person has deleted since is not made again.
		if _, err := r.Dyn.Resource(GVR).Namespace(r.Namespace).Get(ctx, install.ControllerResourceName, metav1.GetOptions{}); apierrors.IsNotFound(err) {
			r.log().Warn("the agent this controller was installed to ask for is not there, and is not asked for again: this request (the same choices and the same token) was already carried out. To ask again, run the install command with a new token, or remove the annotation "+BootstrapApplied+" from the ConfigMap "+r.BootstrapConfigMap,
				"name", install.ControllerResourceName)
		}
		return nil
	}

	res := r.Dyn.Resource(GVR).Namespace(r.Namespace)
	_, err = res.Get(ctx, install.ControllerResourceName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		obj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": Group + "/" + Version,
			"kind":       Kind,
			"metadata":   map[string]any{"name": install.ControllerResourceName, "namespace": r.Namespace},
			"spec":       fields,
		}}
		if _, err := res.Create(ctx, obj, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
			return fmt.Errorf("making the IkhnosAgent: %w", err)
		}
		r.log().Info("asked for the agent the controller was installed with", "name", install.ControllerResourceName)
	case err != nil:
		return err
	default:
		// A merge patch: a field the bootstrap does not state is set to null, which removes it, and the rest of the spec is not touched.
		patch := map[string]any{}
		for _, k := range bootstrapOwned {
			if v, ok := fields[k]; ok {
				patch[k] = v
			} else {
				patch[k] = nil
			}
		}
		body, err := json.Marshal(map[string]any{"spec": patch})
		if err != nil {
			return err
		}
		if _, err := res.Patch(ctx, install.ControllerResourceName, types.MergePatchType, body, metav1.PatchOptions{}); err != nil {
			return fmt.Errorf("changing the IkhnosAgent: %w", err)
		}
		r.log().Info("changed the agent the controller was installed with", "name", install.ControllerResourceName)
	}

	mark, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{BootstrapApplied: digest}}})
	if err != nil {
		return err
	}
	if _, err := cms.Patch(ctx, r.BootstrapConfigMap, types.MergePatchType, mark, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("recording that the agent was asked for: %w", err)
	}
	return nil
}

// bootstrapDigest is the digest of the bootstrap spec and, when its Secret is there, of the token in it. Only the digest is kept (as an
// annotation on the ConfigMap), never the token.
func (r *Reconciler) bootstrapDigest(ctx context.Context, raw string, spec Spec) (string, error) {
	h := sha256.New()
	h.Write([]byte(raw))
	if ref := spec.Enrollment.TokenSecretRef; ref != nil {
		sec, err := r.Kube.CoreV1().Secrets(r.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
		case err != nil:
			return "", err
		default:
			h.Write([]byte{0})
			h.Write(sec.Data[ref.TokenKey()])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// bootstrapUntilDone tries Bootstrap until it succeeds or ctx ends.
func (r *Reconciler) bootstrapUntilDone(ctx context.Context) {
	for {
		err := r.Bootstrap(ctx)
		if err == nil {
			return
		}
		r.log().Warn("the bootstrap did not succeed; trying again", "err", err, "in", BootstrapRetry)
		select {
		case <-ctx.Done():
			return
		case <-time.After(BootstrapRetry):
		}
	}
}
