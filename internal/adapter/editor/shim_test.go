package editor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	shimembed "github.com/kxn/codex-remote-feishu/internal/shim/embed"
)

func TestPatchBundleEntrypoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin", "linux-x86_64", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir entrypoint dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("original-codex"), 0o755); err != nil {
		t.Fatalf("seed bundle entrypoint: %v", err)
	}
	if err := PatchBundleEntrypoint(PatchBundleEntrypointOptions{
		EntrypointPath:   path,
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		InstanceID:       "stable",
	}); err != nil {
		t.Fatalf("patch bundle entrypoint: %v", err)
	}

	realPath := ManagedShimRealBinaryPath(path)
	realRaw, err := os.ReadFile(realPath)
	if err != nil {
		t.Fatalf("read preserved real binary: %v", err)
	}
	if string(realRaw) != "original-codex" {
		t.Fatalf("expected preserved real binary content, got %q", string(realRaw))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat bundle entrypoint: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o755 {
		t.Fatalf("unexpected mode: %o", info.Mode().Perm())
	}

	status, err := DetectManagedShim(path, "")
	if err != nil {
		t.Fatalf("DetectManagedShim: %v", err)
	}
	if status.Kind != ManagedShimKindTiny || !status.Installed || !status.SidecarValid || !status.MatchesBinary {
		t.Fatalf("unexpected shim status: %#v", status)
	}
	if _, ok := shimembed.Current(); !ok {
		t.Fatal("expected embedded managed shim asset for host platform")
	}
}

func TestManagedShimRealBinaryPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/codex":     filepath.Join(string(filepath.Separator), "tmp", "codex.real"),
		`C:\tmp\codex`:   `C:\tmp\codex.real`,
		"/tmp/codex.exe": filepath.Join(string(filepath.Separator), "tmp", "codex.real.exe"),
	}
	for input, want := range tests {
		if got := ManagedShimRealBinaryPath(input); got != want {
			t.Fatalf("ManagedShimRealBinaryPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestUninstallManagedShimRestoresOriginalBinary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin", "linux-x86_64", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir entrypoint dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("original-codex"), 0o755); err != nil {
		t.Fatalf("seed bundle entrypoint: %v", err)
	}
	settingsPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{\n  \"editor.fontSize\": 14\n}\n"), 0o644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if err := PatchBundleEntrypoint(PatchBundleEntrypointOptions{
		EntrypointPath:   path,
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		InstanceID:       "stable",
	}); err != nil {
		t.Fatalf("patch bundle entrypoint: %v", err)
	}

	if err := UninstallManagedShim(path, settingsPath); err != nil {
		t.Fatalf("uninstall managed shim: %v", err)
	}

	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read restored entrypoint: %v", err)
	}
	if string(restored) != "original-codex" {
		t.Fatalf("expected original binary to be restored, got %q", string(restored))
	}
	if _, err := os.Stat(ManagedShimRealBinaryPath(path)); !os.IsNotExist(err) {
		t.Fatalf("expected real binary placeholder to be gone, err=%v", err)
	}
	if _, err := os.Stat(ManagedShimSidecarPath(path)); !os.IsNotExist(err) {
		t.Fatalf("expected sidecar to be removed, err=%v", err)
	}
	settings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(settings, &decoded); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if got, ok := decoded["chatgpt.cliExecutable"].(string); !ok || got != path {
		t.Fatalf("expected settings to point at restored binary, got %q", got)
	}
}

func TestUninstallManagedShimRemovesOrphanedShimWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin", "linux-x86_64", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir entrypoint dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("original-codex"), 0o755); err != nil {
		t.Fatalf("seed bundle entrypoint: %v", err)
	}
	if err := PatchBundleEntrypoint(PatchBundleEntrypointOptions{
		EntrypointPath:   path,
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		InstanceID:       "stable",
	}); err != nil {
		t.Fatalf("patch bundle entrypoint: %v", err)
	}

	// Simulate a lost original-binary backup (e.g. install-state.json and the
	// .real backup both gone). Uninstall must still remove the orphaned shim.
	if err := os.Remove(ManagedShimRealBinaryPath(path)); err != nil {
		t.Fatalf("remove real binary backup: %v", err)
	}
	if err := UninstallManagedShim(path, filepath.Join(dir, "settings.json")); err != nil {
		t.Fatalf("uninstall orphaned shim: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected orphaned shim entrypoint to be removed, err=%v", err)
	}
	if _, err := os.Stat(ManagedShimSidecarPath(path)); !os.IsNotExist(err) {
		t.Fatalf("expected orphaned sidecar to be removed, err=%v", err)
	}
}

func TestUninstallManagedShimLeavesPlainBinaryAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir entrypoint dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("plain-binary"), 0o755); err != nil {
		t.Fatalf("seed plain binary: %v", err)
	}
	// A plain binary with no sidecar and no .real backup must be left untouched.
	if err := UninstallManagedShim(path, filepath.Join(dir, "settings.json")); err != nil {
		t.Fatalf("uninstall on plain binary: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plain binary: %v", err)
	}
	if string(raw) != "plain-binary" {
		t.Fatalf("expected plain binary untouched, got %q", string(raw))
	}
}
