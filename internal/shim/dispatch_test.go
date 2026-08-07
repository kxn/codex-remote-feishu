package shim

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDispatchModeByInvocationName(t *testing.T) {
	cases := map[string]Mode{
		"/state/upgrade-helper/codex-remote-upgrade-shim-1750000000":     ModeUpgrade,
		"/state/upgrade-helper/codex-remote-upgrade-shim-1750000000.exe": ModeUpgrade,
		"/state/upgrade-helper/upgrade-shim":                             ModeUpgrade,
		"/bundle/codex":                                                  ModeManaged,
		"/bundle/codex.exe":                                              ModeManaged,
		"/custom/codex-remote":                                           ModeManaged,
	}
	for input, want := range cases {
		if got := DispatchMode(input); got != want {
			t.Fatalf("DispatchMode(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestDispatchModeBySidecarManager(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "unexpected-name")
	if err := WriteSidecar(SidecarPath(entrypoint), Sidecar{
		InstallStatePath: filepath.Join(dir, "install-state.json"),
	}, ModeUpgrade); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	if got := DispatchMode(entrypoint); got != ModeUpgrade {
		t.Fatalf("DispatchMode = %v, want ModeUpgrade (via sidecar manager)", got)
	}
}

func TestDispatchModeManagedSidecarFallback(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "codex")
	if err := WriteSidecar(SidecarPath(entrypoint), Sidecar{
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
	}, ModeManaged); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	if got := DispatchMode(entrypoint); got != ModeManaged {
		t.Fatalf("DispatchMode = %v, want ModeManaged", got)
	}
}

func TestDispatchModeNoSidecarDefaultsManaged(t *testing.T) {
	dir := t.TempDir()
	entrypoint := filepath.Join(dir, "codex")
	if got := DispatchMode(entrypoint); got != ModeManaged {
		t.Fatalf("DispatchMode = %v, want ModeManaged (no sidecar)", got)
	}
}

func TestDispatchModeIgnoreMissingSidecarFile(t *testing.T) {
	// A sidecar path that does not exist must not error; dispatch falls back
	// to managed, preserving the vscode shim's .real fallback semantics.
	entrypoint := filepath.Join(t.TempDir(), "codex")
	if _, err := os.Stat(SidecarPath(entrypoint)); !os.IsNotExist(err) {
		t.Fatalf("expected no sidecar, stat err=%v", err)
	}
	if got := DispatchMode(entrypoint); got != ModeManaged {
		t.Fatalf("DispatchMode = %v, want ModeManaged", got)
	}
}
