package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/app/installshim"
)

func TestMain(m *testing.M) {
	tempRoot, err := os.MkdirTemp("", "codex-remote-daemon-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "daemon TestMain temp dir: %v\n", err)
		os.Exit(1)
	}
	homeDir := filepath.Join(tempRoot, "home")
	configHome := filepath.Join(tempRoot, "xdg-config")
	dataHome := filepath.Join(tempRoot, "xdg-data")
	stateHome := filepath.Join(tempRoot, "xdg-state")
	repoRoot := filepath.Join(tempRoot, "repo")
	for _, dir := range []string{homeDir, configHome, dataHome, stateHome, repoRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "daemon TestMain mkdir %s: %v\n", dir, err)
			os.Exit(1)
		}
	}

	setenvOrExit := func(key, value string) {
		if err := os.Setenv(key, value); err != nil {
			fmt.Fprintf(os.Stderr, "daemon TestMain setenv %s: %v\n", key, err)
			os.Exit(1)
		}
	}
	setenvOrExit("HOME", homeDir)
	setenvOrExit("XDG_CONFIG_HOME", configHome)
	setenvOrExit("XDG_DATA_HOME", dataHome)
	setenvOrExit("XDG_STATE_HOME", stateHome)
	setenvOrExit("CODEX_REMOTE_REPO_ROOT", repoRoot)

	// Mirror the main-process registration in launcher.Main: install flows
	// reach editor detection/patching and upgrade-shim release through hooks,
	// and the daemon test binary must register them like the real daemon does.
	install.RegisterEditorHooks(editor.DetectBundleEntrypoints, func(p install.BundleEntrypointPatchOptions) error {
		return editor.PatchBundleEntrypoint(editor.PatchBundleEntrypointOptions{
			EntrypointPath:   p.EntrypointPath,
			InstallStatePath: p.InstallStatePath,
			ConfigPath:       p.ConfigPath,
			InstanceID:       p.InstanceID,
		})
	})
	install.RegisterUpgradeHelperShimHook(installshim.PrepareUpgradeHelperShim)

	code := m.Run()
	if err := os.RemoveAll(tempRoot); err != nil {
		fmt.Fprintf(os.Stderr, "daemon TestMain cleanup %s: %v\n", tempRoot, err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
