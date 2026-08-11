package installshim

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/pathcanon"
	"github.com/kxn/codex-remote-feishu/internal/shim"
	shimembed "github.com/kxn/codex-remote-feishu/internal/shim/embed"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

// UpgradeShimEntrypointOptions describes where to materialize an upgrade shim
// binary and which install-state it must bind to.
type UpgradeShimEntrypointOptions struct {
	EntrypointPath   string
	InstallStatePath string
	InstanceID       string
}

// UpgradeShimSidecarPath returns the sidecar path for an upgrade shim entrypoint.
func UpgradeShimSidecarPath(entrypointPath string) string {
	return shim.SidecarPath(entrypointPath)
}

// WriteUpgradeShimEntrypoint releases the embedded upgrade shim binary to
// entrypointPath and writes the sidecar that binds it to InstallStatePath.
func WriteUpgradeShimEntrypoint(opts UpgradeShimEntrypointOptions) error {
	entrypointPath := pathcanon.Native(opts.EntrypointPath)
	if entrypointPath == "" {
		return fmt.Errorf("upgrade shim entrypoint path is required")
	}
	sidecar := shim.Sidecar{
		InstallStatePath: opts.InstallStatePath,
		InstanceID:       opts.InstanceID,
	}
	if !shim.SidecarValid(sidecar, shim.ModeUpgrade) {
		return fmt.Errorf("upgrade shim install requires install state path")
	}
	if err := os.MkdirAll(filepath.Dir(entrypointPath), 0o755); err != nil {
		return err
	}
	if err := shimembed.WriteExecutable(entrypointPath); err != nil {
		return err
	}
	return shim.WriteSidecar(UpgradeShimSidecarPath(entrypointPath), sidecar, shim.ModeUpgrade)
}

// PrepareUpgradeHelperShim releases a uniquely-named upgrade shim next to the
// install-state file (under <stateDir>/upgrade-helper/) and returns its path.
func PrepareUpgradeHelperShim(statePath, instanceID string) (string, error) {
	statePath = pathcanon.Native(statePath)
	if statePath == "" {
		return "", fmt.Errorf("state path is required")
	}
	helperDir := filepath.Join(filepath.Dir(statePath), "upgrade-helper")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		return "", err
	}
	name := "codex-remote-upgrade-shim"
	ext := filepath.Ext(xutil.ExecutableName(runtime.GOOS))
	entrypointPath := filepath.Join(helperDir, fmt.Sprintf("%s-%d%s", name, time.Now().UTC().UnixNano(), ext))
	if err := WriteUpgradeShimEntrypoint(UpgradeShimEntrypointOptions{
		EntrypointPath:   entrypointPath,
		InstallStatePath: statePath,
		InstanceID:       instanceID,
	}); err != nil {
		return "", err
	}
	return entrypointPath, nil
}
