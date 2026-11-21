package install

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInteractiveWizardConfiguresFeishuWithoutEditorIntegration(t *testing.T) {
	baseDir := t.TempDir()
	codex := filepath.Join(baseDir, "bin", "codex")
	defaults := PlatformDefaults{
		GOOS:                       "linux",
		BaseDir:                    baseDir,
		InstallBinDir:              filepath.Join(baseDir, "installed"),
		CandidateBundleEntrypoints: []string{filepath.Join(baseDir, "extensions", "codex")},
	}
	input := strings.Join([]string{"", "", "main", "cli_demo", "secret_demo", "n", "", "", ""}, "\n")
	var out bytes.Buffer
	opts, err := RunInteractiveWizard(strings.NewReader(input), &out, defaults, Options{CodexRealBinary: codex})
	if err != nil {
		t.Fatalf("RunInteractiveWizard: %v", err)
	}
	if opts.FeishuGatewayID != "main" || opts.FeishuAppID != "cli_demo" || opts.FeishuAppSecret != "secret_demo" {
		t.Fatal("wizard did not retain Feishu inputs")
	}
	if opts.CodexRealBinary != codex {
		t.Fatalf("wizard Codex path = %q, want %q", opts.CodexRealBinary, codex)
	}
	if strings.Contains(out.String(), "VS Code") || !strings.Contains(out.String(), "Codex 路径覆盖（可选）") {
		t.Fatalf("unexpected wizard prompts: %s", out.String())
	}
}
