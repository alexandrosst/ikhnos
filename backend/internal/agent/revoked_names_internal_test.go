package agent

import (
	"testing"

	"ikhnos/internal/install"
)

func TestTheAgentAndTheInstallerNameTheRevokedAnnotationAlike(t *testing.T) {
	if revokedAnnotation != install.RevokedAnnotation || resourceAPIVersion != install.ResourceAPIVersion {
		t.Fatalf("the agent has %q on %q, the installer %q on %q", revokedAnnotation, resourceAPIVersion, install.RevokedAnnotation, install.ResourceAPIVersion)
	}
}
