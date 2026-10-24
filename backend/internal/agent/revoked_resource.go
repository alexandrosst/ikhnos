package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"ikhnos/internal/install"
)

// MarkResourceRevoked is Config.MarkRevoked for an agent that a controller keeps: it puts install.RevokedAnnotation, with the reason, on the
// IkhnosAgent called name in namespace, and the controller removes the agent. It writes that one annotation of that one object (the chart
// gives the agent the right to patch that object and nothing else) and holds no credential beyond the agent's own.
func MarkResourceRevoked(kube kubernetes.Interface, namespace, name string) func(ctx context.Context, reason string) error {
	return func(ctx context.Context, reason string) error {
		if len(reason) > 200 {
			reason = reason[:200]
		}
		body, err := json.Marshal(map[string]any{"metadata": map[string]any{"annotations": map[string]string{install.RevokedAnnotation: reason}}})
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/apis/%s/namespaces/%s/ikhnosagents/%s", install.ResourceAPIVersion, namespace, name)
		if _, err := kube.CoreV1().RESTClient().Patch(types.MergePatchType).AbsPath(path).Body(body).DoRaw(ctx); err != nil {
			return fmt.Errorf("patching the IkhnosAgent %s/%s: %w", namespace, name, err)
		}
		return nil
	}
}
