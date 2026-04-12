package managedshim

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRealBinaryPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/codex":     "/tmp/codex.real",
		`C:\tmp\codex`:   `C:\tmp\codex.real`,
		"/tmp/codex.exe": "/tmp/codex.real.exe",
	}
	for input, want := range tests {
		if got := RealBinaryPath(input); got != want {
			t.Fatalf("RealBinaryPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSidecarPath(t *testing.T) {
	tests := map[string]string{
		"/tmp/codex":     "/tmp/codex.remote.json",
		`C:\tmp\codex`:   `C:\tmp\codex.remote.json`,
		"/tmp/codex.exe": "/tmp/codex.remote.json",
	}
	for input, want := range tests {
		if got := SidecarPath(input); got != want {
			t.Fatalf("SidecarPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWriteAndReadSidecar(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex.remote.json")
	want := Sidecar{
		InstallStatePath: filepath.Join(dir, "install-state.json"),
		ConfigPath:       filepath.Join(dir, "config.json"),
		InstanceID:       "stable",
	}
	if err := WriteSidecar(path, want); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}
	got, err := ReadSidecar(path)
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if got.SchemaVersion != SidecarSchemaVersion {
		t.Fatalf("SchemaVersion = %d", got.SchemaVersion)
	}
	if got.Manager != SidecarManager {
		t.Fatalf("Manager = %q", got.Manager)
	}
	if got.InstallStatePath != want.InstallStatePath {
		t.Fatalf("InstallStatePath = %q, want %q", got.InstallStatePath, want.InstallStatePath)
	}
	if got.ConfigPath != want.ConfigPath {
		t.Fatalf("ConfigPath = %q, want %q", got.ConfigPath, want.ConfigPath)
	}
	if got.InstanceID != want.InstanceID {
		t.Fatalf("InstanceID = %q, want %q", got.InstanceID, want.InstanceID)
	}
}

func TestWriteSidecarRejectsMissingBinding(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex.remote.json")
	err := WriteSidecar(path, Sidecar{InstallStatePath: filepath.Join(dir, "install-state.json")})
	if err == nil {
		t.Fatal("expected missing config path to fail")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no sidecar file, stat err=%v", statErr)
	}
}
