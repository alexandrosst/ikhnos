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
	want, err := r.bootstrapAsked(ctx, raw, spec)
	if err != nil {
		return err
	}
	if done, upgrade := want.alreadyCarriedOut(cm.Annotations[BootstrapApplied]); done {
		if upgrade {
			// An annotation written by an earlier controller says the same in one digest; say it in the form that survives the token's Secret
			// being removed. Best effort: it is only a better way of remembering what was already known.
			_ = r.markBootstrap(ctx, want)
		}
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

	return r.markBootstrap(ctx, want)
}

// markBootstrap records on the ConfigMap what was asked for, as digests: never the token.
func (r *Reconciler) markBootstrap(ctx context.Context, want bootstrapRequest) error {
	mark, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{BootstrapApplied: want.annotation()}}})
	if err != nil {
		return err
	}
	if _, err := r.Kube.CoreV1().ConfigMaps(r.Namespace).Patch(ctx, r.BootstrapConfigMap, types.MergePatchType, mark, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("recording that the agent was asked for: %w", err)
	}
	return nil
}

// bootstrapRequest is what a bootstrap asks for, as digests: the spec, and the token in the Secret it names when that Secret is there.
type bootstrapRequest struct {
	spec  string // digest of the spec text
	token string // digest of the token; empty when the Secret is not there

	// hasRef is whether the spec names a Secret for the token at all.
	hasRef bool

	// Kept only to recognise what an earlier controller wrote (see legacyDigest); never stored.
	raw         string
	legacyToken []byte
}

// annotation is what is kept on the ConfigMap: both digests, so that the spec can be compared when the Secret is gone. The token is spent
// at its first use and the chart's own notes say the Secret may be removed then; a controller that forgot what it had asked for when that
// happened would make an agent a person had deleted, or put back what a person had edited.
func (q bootstrapRequest) annotation() string { return "v2:" + q.spec + ":" + q.token }

// alreadyCarriedOut says whether `kept`, the annotation of the ConfigMap, is this request, and whether it should be rewritten in the
// current form. The same spec with a new token is a new request. The same spec with no Secret to look at is not: nothing says the token
// changed, and a request that was carried out is not carried out again because its spent token was cleaned up.
func (q bootstrapRequest) alreadyCarriedOut(kept string) (done, upgrade bool) {
	if kept == "" {
		return false, false
	}
	if rest, ok := strings.CutPrefix(kept, "v2:"); ok {
		spec, token, _ := strings.Cut(rest, ":")
		if spec != q.spec {
			return false, false
		}
		return q.token == "" || q.token == token, false
	}
	// Written by a controller that kept one digest of the spec and the token together (and of the spec alone when there was no Secret).
	// With the Secret there, or with no Secret named, the old digest can be compared; with a Secret that is gone, an annotation was made
	// with a token and nothing says it changed.
	if q.token == "" && q.hasRef {
		return true, false
	}
	same := kept == legacyDigest(q)
	return same, same && q.hasRef
}

func (r *Reconciler) bootstrapAsked(ctx context.Context, raw string, spec Spec) (bootstrapRequest, error) {
	q := bootstrapRequest{spec: hex.EncodeToString(sha256Sum(raw))}
	if ref := spec.Enrollment.TokenSecretRef; ref != nil {
		q.hasRef = true
		sec, err := r.Kube.CoreV1().Secrets(r.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
		case err != nil:
			return q, err
		default:
			q.token = hex.EncodeToString(sha256Sum(string(sec.Data[ref.TokenKey()])))
			q.legacyToken = sec.Data[ref.TokenKey()]
		}
	}
	q.raw = raw
	return q, nil
}

func sha256Sum(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }

// legacyDigest is the single digest an earlier controller kept: the spec text, then (when the Secret was there) a zero byte and the token.
func legacyDigest(q bootstrapRequest) string {
	h := sha256.New()
	h.Write([]byte(q.raw))
	if len(q.legacyToken) > 0 || q.token != "" {
		h.Write([]byte{0})
		h.Write(q.legacyToken)
	}
	return hex.EncodeToString(h.Sum(nil))
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
