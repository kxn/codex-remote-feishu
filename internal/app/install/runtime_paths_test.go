package install

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRuntimeEnvForStateIncludesCodexHome(t *testing.T) {
	baseDir := t.TempDir()
	codexHome := filepath.Join(baseDir, "codex-home")
	env := RuntimeEnvForState(InstallState{
		BaseDir:   baseDir,
		CodexHome: codexHome,
	})

	if !slices.Contains(env, "CODEX_HOME="+codexHome) {
		t.Fatalf("RuntimeEnvForState() = %#v, want CODEX_HOME=%s", env, codexHome)
	}
}
