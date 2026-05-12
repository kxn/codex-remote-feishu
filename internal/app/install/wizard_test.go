package install

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInteractiveWizardManagedShimBlankCodexBinaryUsesAutoResolution(t *testing.T) {
	defaults := PlatformDefaults{
		GOOS:                       "linux",
		BaseDir:                    "/tmp/demo",
		InstallBinDir:              "/tmp/demo/bin",
		VSCodeSettingsPath:         "/tmp/demo/settings.json",
		CandidateBundleEntrypoints: []string{"/tmp/demo/.vscode-server/extensions/openai.chatgpt/bin/linux-x86_64/codex"},
		DefaultIntegrations:        []WrapperIntegrationMode{IntegrationManagedShim},
	}
	input := strings.Join([]string{
		"2",
		"",
		"",
		"cli_demo",
		"secret_demo",
		"n",
		"",
		"",
		"",
	}, "\n")
	var out bytes.Buffer
	opts, err := RunInteractiveWizard(strings.NewReader(input), &out, defaults, Options{})
	if err != nil {
		t.Fatalf("RunInteractiveWizard: %v", err)
	}
	if len(opts.Integrations) != 1 || opts.Integrations[0] != IntegrationManagedShim {
		t.Fatalf("unexpected integrations: %#v", opts.Integrations)
	}
	if !strings.Contains(out.String(), "Codex 路径覆盖（可选）") {
		t.Fatalf("wizard output missing optional Codex override copy: %q", out.String())
	}
	if strings.Contains(out.String(), "真实 Codex 配置说明") {
		t.Fatalf("wizard output kept stale Codex-only copy: %q", out.String())
	}
	if opts.CodexRealBinary != "" {
		t.Fatalf("expected blank codex binary for managed shim auto-resolution, got %q", opts.CodexRealBinary)
	}
	if opts.BundleEntrypoint != defaults.CandidateBundleEntrypoints[0] {
		t.Fatalf("unexpected bundle entrypoint: %q", opts.BundleEntrypoint)
	}
}

func TestRunInteractiveWizardHonorsExplicitIntegrationSeed(t *testing.T) {
	defaults := PlatformDefaults{
		GOOS:                "darwin",
		BaseDir:             "/tmp/demo-home",
		InstallBinDir:       "/tmp/demo-home/Library/Application Support/codex-remote/bin",
		VSCodeSettingsPath:  "/tmp/demo-home/Library/Application Support/Code/User/settings.json",
		DefaultIntegrations: []WrapperIntegrationMode{IntegrationManagedShim},
	}
	input := strings.Join([]string{
		"",
		"",
		"",
		"cli_demo",
		"secret_demo",
		"y",
		"",
		"",
	}, "\n")
	var out bytes.Buffer
	seed := Options{
		Integrations:       []WrapperIntegrationMode{IntegrationManagedShim},
		BinaryPath:         filepath.Join("/opt", "homebrew", "bin", "codex-remote"),
		CodexRealBinary:    filepath.Join("/opt", "homebrew", "bin", "codex"),
		VSCodeSettingsPath: defaults.VSCodeSettingsPath,
	}
	opts, err := RunInteractiveWizard(strings.NewReader(input), &out, defaults, seed)
	if err != nil {
		t.Fatalf("RunInteractiveWizard: %v", err)
	}
	if len(opts.Integrations) != 1 || opts.Integrations[0] != IntegrationManagedShim {
		t.Fatalf("unexpected integrations: %#v", opts.Integrations)
	}
	if opts.CodexRealBinary != filepath.Join("/opt", "homebrew", "bin", "codex") {
		t.Fatalf("unexpected codex binary: %q", opts.CodexRealBinary)
	}
}
