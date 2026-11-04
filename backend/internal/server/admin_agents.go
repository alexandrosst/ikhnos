package server

import (
	"ikhnos/internal/kernel"
	"ikhnos/internal/landscape"
)

// upgradeCommand is the command that changes the ceiling of an agent's install to a tier (landscape.UpgradeCommand), for the namespace and
// release the agent reported.
func (a *Admin) upgradeCommand(img kernel.ImageConfig, tier int, namespace, release string) (string, error) {
	return landscape.UpgradeCommand(&a.Settings, img, tier, namespace, release)
}
