package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

// resourceAPIVersion is the install package's ResourceAPIVersion, kept here so the agent does not import the installer for one string (a
// test holds them equal).
const resourceAPIVersion = "ikhnos.io/v1alpha1"

// MarkResourceRevoked is Config.MarkRevoked for an agent that a controller keeps: it writes status.revoked.reason on the IkhnosAgent
// called name in namespace, and the controller removes the agent. It is one merge patch of the resource's status subresource, which the
// API server will not let change its spec (the chart gives the agent the right to patch that subresource of that one object and nothing
// else), and it holds no credential beyond the agent's own.
//
// The patch is checked against what the API server answers with. A definition of the resource from before status.revoked existed drops the
// field without saying so (Helm never updates a CRD it has installed), and an agent that took that for success would never be removed.
func MarkResourceRevoked(kube kubernetes.Interface, namespace, name string) func(ctx context.Context, reason string) error {
	return func(ctx context.Context, reason string) error {
		if utf8.RuneCountInString(reason) > 200 { // the definition counts characters; a cut inside one would be written back as something else
			reason = string([]rune(reason)[:200])
		}
		body, err := json.Marshal(map[string]any{"status": map[string]any{"revoked": map[string]string{"reason": reason}}})
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/apis/%s/namespaces/%s/ikhnosagents/%s/status", resourceAPIVersion, namespace, name)
		raw, err := kube.CoreV1().RESTClient().Patch(types.MergePatchType).AbsPath(path).Body(body).DoRaw(ctx)
		if err != nil {
			return fmt.Errorf("patching the status of the IkhnosAgent %s/%s: %w", namespace, name, err)
		}
		var got struct {
			Status struct {
				Revoked struct {
					Reason string `json:"reason"`
				} `json:"revoked"`
			} `json:"status"`
		}
		if json.Unmarshal(raw, &got) != nil || got.Status.Revoked.Reason != reason {
			return fmt.Errorf("the IkhnosAgent %s/%s did not keep status.revoked: the definition of the resource in this cluster is older than the controller; apply the one in the controller's chart (kubectl apply -f crds/ikhnosagents.yaml)", namespace, name)
		}
		return nil
	}
}
