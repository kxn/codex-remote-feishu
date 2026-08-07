package upgradeshim

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/shim"
)

func TestResolveStatePathUsesSidecar(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "upgrade-helper", "codex-remote-upgrade-shim")
	statePath := filepath.Join(dir, "install-state.json")
	if err := os.MkdirAll(filepath.Dir(entrypoint), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := shim.WriteSidecar(shim.SidecarPath(entrypoint), shim.Sidecar{
		InstallStatePath: statePath,
		InstanceID:       "stable",
	}, shim.ModeUpgrade); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	got, err := resolveStatePath(entrypoint)
	if err != nil {
		t.Fatalf("resolveStatePath: %v", err)
	}
	if got != statePath {
		t.Fatalf("state path = %q, want %q", got, statePath)
	}
}

func TestRunMainInvokesUpgradeHelperWithResolvedState(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "upgrade-helper", "codex-remote-upgrade-shim")
	statePath := filepath.Join(dir, "install-state.json")
	if err := os.MkdirAll(filepath.Dir(entrypoint), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(entrypoint, []byte("shim"), 0o755); err != nil {
		t.Fatalf("WriteFile entrypoint: %v", err)
	}
	if err := os.WriteFile(statePath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("WriteFile state: %v", err)
	}
	if err := shim.WriteSidecar(shim.SidecarPath(entrypoint), shim.Sidecar{
		InstallStatePath: statePath,
	}, shim.ModeUpgrade); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	originalExecutable := osExecutable
	originalRun := runUpgradeHelperWithStatePath
	defer func() {
		osExecutable = originalExecutable
		runUpgradeHelperWithStatePath = originalRun
	}()
	osExecutable = func() (string, error) { return entrypoint, nil }
	var gotStatePath string
	runUpgradeHelperWithStatePath = func(_ context.Context, path string) error {
		gotStatePath = path
		return nil
	}

	if code := RunMain(nil); code != 0 {
		t.Fatalf("RunMain() = %d, want 0", code)
	}
	if gotStatePath != statePath {
		t.Fatalf("state path = %q, want %q", gotStatePath, statePath)
	}
}
