package shim

import (
	"path/filepath"
	"strings"
)

// DispatchMode decides which shim role the unified binary should take based on
// how it was invoked:
//
//  1. If the invoked name contains "upgrade" (the upgrade-helper installs the
//     shim as codex-remote-upgrade-shim-<timestamp>[.exe]), the binary is an
//     upgrade shim.
//  2. Otherwise it consults the sidecar next to the entrypoint: a sidecar
//     written by an upgrade shim (manager "codex-remote-upgrade") selects the
//     upgrade role; anything else falls back to the managed (VS Code) role,
//     which preserves the managed shim's no-sidecar -> .real fallback
//     semantics.
func DispatchMode(entrypointPath string) Mode {
	if isUpgradeInvocation(entrypointPath) {
		return ModeUpgrade
	}
	sidecar, err := ReadSidecar(SidecarPath(entrypointPath))
	if err == nil && sidecar.Manager == UpgradeSidecarManager {
		return ModeUpgrade
	}
	return ModeManaged
}

func isUpgradeInvocation(entrypointPath string) bool {
	base := strings.ToLower(filepath.Base(entrypointPath))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.Contains(base, "upgrade")
}
