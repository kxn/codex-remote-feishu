package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/pathscope"
)

func TestLoadStateCollapsesLegacyConfigPaths(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}
	legacyWrapperPath := filepath.Join(baseDir, ".config", "codex-remote", "wrapper.env")
	legacyServicesPath := filepath.Join(baseDir, ".config", "codex-remote", "services.env")
	rawBytes, err := json.Marshal(map[string]string{
		"statePath":          statePath,
		"wrapperConfigPath":  legacyWrapperPath,
		"servicesConfigPath": legacyServicesPath,
	})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := os.WriteFile(statePath, rawBytes, 0o644); err != nil {
		t.Fatalf("write state: %v", err)
	}

	loaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	wantConfigPath := filepath.Join(baseDir, ".config", "codex-remote", "config.json")
	if loaded.ConfigPath != wantConfigPath {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, wantConfigPath)
	}
	if loaded.StatePath != statePath {
		t.Fatalf("StatePath = %q, want %q", loaded.StatePath, statePath)
	}
}

func TestWriteStateOmitsLegacyConfigPathFields(t *testing.T) {
	baseDir := t.TempDir()
	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	state := InstallState{
		BaseDir:    baseDir,
		ConfigPath: filepath.Join(baseDir, ".config", "codex-remote", "config.json"),
		StatePath:  statePath,
	}
	if err := WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	raw, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	for _, field := range []string{"wrapperConfigPath", "servicesConfigPath"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("did not expect %s in written state: %s", field, raw)
		}
	}
}

func TestWriteStateRespectsStrictFSPrefix(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv(pathscope.EnvFSPrefix, prefix)
	t.Setenv(pathscope.EnvFSStrict, "1")

	state := InstallState{
		BaseDir:    prefix,
		ConfigPath: filepath.Join(prefix, ".config", "codex-remote", "config.json"),
		StatePath:  filepath.Join(prefix, ".local", "share", "codex-remote", "install-state.json"),
	}
	outsidePath := filepath.Join(t.TempDir(), "install-state.json")
	if err := WriteState(outsidePath, state); err == nil {
		t.Fatal("WriteState(outside) expected strict-prefix error")
	}

	insidePath := filepath.Join(prefix, ".local", "share", "codex-remote", "install-state.json")
	if err := WriteState(insidePath, state); err != nil {
		t.Fatalf("WriteState(inside): %v", err)
	}
}
