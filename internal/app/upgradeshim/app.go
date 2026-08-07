package upgradeshim

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/shim"
)

var runUpgradeHelperWithStatePath = install.RunUpgradeHelperWithStatePath
var osExecutable = os.Executable

func RunMain(args []string) int {
	if len(args) != 0 {
		_, _ = fmt.Fprintf(os.Stderr, "upgrade shim: unexpected arguments: %s\n", strings.Join(args, " "))
		return 1
	}
	executable, err := osExecutable()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "upgrade shim: resolve executable: %v\n", err)
		return 1
	}
	statePath, err := resolveStatePath(executable)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "upgrade shim: %v\n", err)
		return 1
	}
	if err := runUpgradeHelperWithStatePath(context.Background(), statePath); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "upgrade shim: %v\n", err)
		return 1
	}
	return 0
}

func resolveStatePath(entrypointPath string) (string, error) {
	entrypointPath = filepath.Clean(strings.TrimSpace(entrypointPath))
	if entrypointPath == "" {
		return "", fmt.Errorf("entrypoint path is empty")
	}
	sidecarPath := shim.SidecarPath(entrypointPath)
	sidecar, err := shim.ReadSidecar(sidecarPath)
	if err != nil {
		return "", err
	}
	if !shim.SidecarValid(sidecar, shim.ModeUpgrade) {
		return "", fmt.Errorf("upgrade shim sidecar is invalid")
	}
	return filepath.Clean(strings.TrimSpace(sidecar.InstallStatePath)), nil
}
