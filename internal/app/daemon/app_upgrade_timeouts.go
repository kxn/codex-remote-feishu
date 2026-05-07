package daemon

import "time"

const (
	upgradeMetadataTimeout     = 60 * time.Second
	upgradePrepareTimeout      = 10 * time.Minute
	codexUpgradeCheckTimeout   = 60 * time.Second
	codexUpgradeInstallTimeout = 10 * time.Minute
)
