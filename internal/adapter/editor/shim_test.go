package editor

import (
	"os"
	"path/filepath"
	"testing"

	managedshimembed "github.com/kxn/codex-remote-feishu/internal/managedshim/embed"
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
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("unexpected mode: %o", info.Mode().Perm())
	}

	status, err := DetectManagedShim(path, "")
	if err != nil {
		t.Fatalf("DetectManagedShim: %v", err)
	}
	if status.Kind != ManagedShimKindTiny || !status.Installed || !status.SidecarValid || !status.MatchesBinary {
		t.Fatalf("unexpected shim status: %#v", status)
	}
	if _, ok := managedshimembed.Current(); !ok {
		t.Fatal("expected embedded managed shim asset for host platform")
	}
}

func TestManagedShimRealBinaryPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/codex":     "/tmp/codex.real",
		`C:\tmp\codex`:   `C:\tmp\codex.real`,
		"/tmp/codex.exe": "/tmp/codex.real.exe",
	}
	for input, want := range tests {
		if got := ManagedShimRealBinaryPath(input); got != want {
			t.Fatalf("ManagedShimRealBinaryPath(%q) = %q, want %q", input, got, want)
		}
	}
}
