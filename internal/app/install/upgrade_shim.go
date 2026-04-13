package install

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/upgradeshim"
	upgradeshimembed "github.com/kxn/codex-remote-feishu/internal/upgradeshim/embed"
)

type UpgradeShimEntrypointOptions struct {
	EntrypointPath   string
	InstallStatePath string
	InstanceID       string
}

func UpgradeShimSidecarPath(entrypointPath string) string {
	return upgradeshim.SidecarPath(entrypointPath)
}

func WriteUpgradeShimEntrypoint(opts UpgradeShimEntrypointOptions) error {
	entrypointPath := strings.TrimSpace(opts.EntrypointPath)
	if entrypointPath == "" {
		return fmt.Errorf("upgrade shim entrypoint path is required")
	}
	sidecar := upgradeshim.Sidecar{
		InstallStatePath: opts.InstallStatePath,
		InstanceID:       opts.InstanceID,
	}
	if !upgradeshim.SidecarValid(sidecar) {
		return fmt.Errorf("upgrade shim install requires install state path")
	}
	if err := os.MkdirAll(filepath.Dir(entrypointPath), 0o755); err != nil {
		return err
	}
	if err := upgradeshimembed.WriteExecutable(entrypointPath); err != nil {
		return err
	}
	return upgradeshim.WriteSidecar(UpgradeShimSidecarPath(entrypointPath), sidecar)
}

func PrepareUpgradeHelperShim(statePath, instanceID string) (string, error) {
	statePath = filepath.Clean(strings.TrimSpace(statePath))
	if statePath == "" {
		return "", fmt.Errorf("state path is required")
	}
	helperDir := filepath.Join(filepath.Dir(statePath), "upgrade-helper")
	if err := os.MkdirAll(helperDir, 0o755); err != nil {
		return "", err
	}
	name := "codex-remote-upgrade-shim"
	ext := filepath.Ext(executableName(runtime.GOOS))
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
