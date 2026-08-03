package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/managedshim"
	managedshimembed "github.com/kxn/codex-remote-feishu/internal/managedshim/embed"
)

type PatchBundleEntrypointOptions struct {
	EntrypointPath   string
	InstallStatePath string
	ConfigPath       string
	InstanceID       string
}

func ManagedShimRealBinaryPath(entrypointPath string) string {
	return managedshim.RealBinaryPath(entrypointPath)
}

func ManagedShimSidecarPath(entrypointPath string) string {
	return managedshim.SidecarPath(entrypointPath)
}

func PatchBundleEntrypoint(opts PatchBundleEntrypointOptions) error {
	entrypointPath := strings.TrimSpace(opts.EntrypointPath)
	if entrypointPath == "" {
		return fmt.Errorf("bundle entrypoint path is required")
	}
	sidecar := managedshim.Sidecar{
		InstallStatePath: opts.InstallStatePath,
		ConfigPath:       opts.ConfigPath,
		InstanceID:       opts.InstanceID,
	}
	if !managedshim.SidecarValid(sidecar) {
		return fmt.Errorf("managed shim install requires install state path and config path")
	}
	if err := os.MkdirAll(filepath.Dir(entrypointPath), 0o755); err != nil {
		return err
	}

	realBinaryPath := ManagedShimRealBinaryPath(entrypointPath)
	renamedOriginal := false
	if _, err := os.Stat(realBinaryPath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if _, statErr := os.Stat(entrypointPath); statErr != nil {
			return statErr
		}
		if err := os.Rename(entrypointPath, realBinaryPath); err != nil {
			return err
		}
		renamedOriginal = true
	}

	restoreOriginal := func() {
		if !renamedOriginal {
			return
		}
		_ = os.Remove(entrypointPath)
		_ = os.Rename(realBinaryPath, entrypointPath)
	}

	if err := managedshimembed.WriteExecutable(entrypointPath); err != nil {
		restoreOriginal()
		return err
	}
	if err := managedshim.WriteSidecar(ManagedShimSidecarPath(entrypointPath), sidecar); err != nil {
		restoreOriginal()
		return err
	}
	return nil
}

// UninstallManagedShim reverses PatchBundleEntrypoint: it removes the shim
// executable and sidecar, restores the original binary to the entrypoint path,
// and points VS Code settings back at the restored binary.
func UninstallManagedShim(entrypointPath, settingsPath string) error {
	entrypointPath = strings.TrimSpace(entrypointPath)
	if entrypointPath == "" {
		return nil
	}
	realBinaryPath := ManagedShimRealBinaryPath(entrypointPath)
	if _, err := os.Stat(realBinaryPath); err != nil {
		if os.IsNotExist(err) {
			// No renamed original binary, so nothing was installed to undo.
			return nil
		}
		return err
	}
	if err := os.Remove(ManagedShimSidecarPath(entrypointPath)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(entrypointPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(realBinaryPath, entrypointPath); err != nil {
		return err
	}
	return SetVSCodeSettingsExecutable(settingsPath, entrypointPath)
}
