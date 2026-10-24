package agent

import (
	"testing"

	"ikhnos/internal/install"
)

func TestTheAgentAndTheInstallerNameTheResourceAlike(t *testing.T) {
	if resourceAPIVersion != install.ResourceAPIVersion {
		t.Fatalf("the agent has %q, the installer %q", resourceAPIVersion, install.ResourceAPIVersion)
	}
}
