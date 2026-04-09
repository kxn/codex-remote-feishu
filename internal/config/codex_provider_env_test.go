package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveCodexProviderEnvUsesConfiguredProfile(t *testing.T) {
	homeDir := t.TempDir()
	writeCodexConfigForTest(t, filepath.Join(homeDir, ".codex"), `
profile = "team"
model_provider = "fallback"

[profiles.team]
model_provider = "custom"

[model_providers.custom]
name = "Custom"
env_key = "CUSTOM_API_KEY"
`)

	info, err := ResolveCodexProviderEnv([]string{"app-server"}, []string{"HOME=" + homeDir})
	if err != nil {
		t.Fatalf("ResolveCodexProviderEnv: %v", err)
	}
	if got, want := info.ConfigPath, filepath.Join(homeDir, ".codex", codexConfigFileName); got != want {
		t.Fatalf("ConfigPath = %q, want %q", got, want)
	}
	if got, want := info.ActiveProfile, "team"; got != want {
		t.Fatalf("ActiveProfile = %q, want %q", got, want)
	}
	if got, want := info.ActiveProvider, "custom"; got != want {
		t.Fatalf("ActiveProvider = %q, want %q", got, want)
	}
	if got, want := info.RequiredEnvKey, "CUSTOM_API_KEY"; got != want {
		t.Fatalf("RequiredEnvKey = %q, want %q", got, want)
	}
}

func TestResolveCodexProviderEnvHonorsCLIOverrides(t *testing.T) {
	codexHome := t.TempDir()
	writeCodexConfigForTest(t, codexHome, `
profile = "team"
model_provider = "fallback"

[profiles.team]
model_provider = "custom"

[model_providers.custom]
name = "Custom"
env_key = "CUSTOM_API_KEY"
`)

	args := []string{
		"app-server",
		"--profile=beta",
		"-c", `model_provider="cli-provider"`,
		"-c", `profiles.beta.model_provider="beta-provider"`,
		"--config=model_providers.cli-provider.env_key=\"CLI_KEY\"",
	}
	info, err := ResolveCodexProviderEnv(args, []string{"CODEX_HOME=" + codexHome})
	if err != nil {
		t.Fatalf("ResolveCodexProviderEnv: %v", err)
	}
	if got, want := info.ActiveProfile, "beta"; got != want {
		t.Fatalf("ActiveProfile = %q, want %q", got, want)
	}
	if got, want := info.ActiveProvider, "cli-provider"; got != want {
		t.Fatalf("ActiveProvider = %q, want %q", got, want)
	}
	if got, want := info.RequiredEnvKey, "CLI_KEY"; got != want {
		t.Fatalf("RequiredEnvKey = %q, want %q", got, want)
	}
}

func TestResolveCodexProviderEnvReturnsErrorForMissingProfile(t *testing.T) {
	homeDir := t.TempDir()
	writeCodexConfigForTest(t, filepath.Join(homeDir, ".codex"), `
profile = "missing"
model_provider = "custom"

[model_providers.custom]
name = "Custom"
env_key = "CUSTOM_API_KEY"
`)

	_, err := ResolveCodexProviderEnv([]string{"app-server"}, []string{"HOME=" + homeDir})
	if err == nil {
		t.Fatal("expected missing profile error")
	}
}

func TestSupplementCodexProviderEnvInjectsMissingKeyFromShell(t *testing.T) {
	homeDir := t.TempDir()
	writeCodexConfigForTest(t, filepath.Join(homeDir, ".codex"), `
model_provider = "custom"

[model_providers.custom]
name = "Custom"
env_key = "CUSTOM_API_KEY"
`)

	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		if got, want := key, "CUSTOM_API_KEY"; got != want {
			t.Fatalf("lookup key = %q, want %q", got, want)
		}
		return "from-shell", nil
	}

	env := []string{"HOME=" + homeDir, "PATH=/usr/bin"}
	got, err := supplementCodexProviderEnv(env, []string{"app-server"})
	if err != nil {
		t.Fatalf("supplementCodexProviderEnv: %v", err)
	}
	if value, ok := lookupEnvValue(got, "CUSTOM_API_KEY"); !ok || value != "from-shell" {
		t.Fatalf("CUSTOM_API_KEY = %q ok=%v, want from-shell", value, ok)
	}
}

func TestSupplementCodexProviderEnvKeepsExistingValue(t *testing.T) {
	homeDir := t.TempDir()
	writeCodexConfigForTest(t, filepath.Join(homeDir, ".codex"), `
model_provider = "custom"

[model_providers.custom]
name = "Custom"
env_key = "CUSTOM_API_KEY"
`)

	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()
	lookupCalled := false
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		lookupCalled = true
		return "", nil
	}

	env := []string{"HOME=" + homeDir, "CUSTOM_API_KEY=already-set"}
	got, err := supplementCodexProviderEnv(env, []string{"app-server"})
	if err != nil {
		t.Fatalf("supplementCodexProviderEnv: %v", err)
	}
	if lookupCalled {
		t.Fatal("expected shell lookup to be skipped")
	}
	if value, ok := lookupEnvValue(got, "CUSTOM_API_KEY"); !ok || value != "already-set" {
		t.Fatalf("CUSTOM_API_KEY = %q ok=%v, want already-set", value, ok)
	}
}

func TestBuildCodexChildEnvPreservesProxyRulesAndInjectsCodexKey(t *testing.T) {
	homeDir := t.TempDir()
	writeCodexConfigForTest(t, filepath.Join(homeDir, ".codex"), `
model_provider = "custom"

[model_providers.custom]
name = "Custom"
env_key = "CUSTOM_API_KEY"
`)

	originalLookup := lookupUserShellEnvValue
	defer func() { lookupUserShellEnvValue = originalLookup }()
	lookupUserShellEnvValue = func(env []string, key string) (string, error) {
		return "from-shell", nil
	}

	currentEnv := []string{
		"HOME=" + homeDir,
		"PATH=/usr/bin",
		"http_proxy=http://old-proxy",
		"KEEP_ME=1",
	}
	proxyEnv := []string{
		"http_proxy=http://restored-proxy",
		"HTTPS_PROXY=https://restored-proxy",
	}

	got := BuildCodexChildEnv(currentEnv, proxyEnv, []string{"app-server"})
	if value, ok := lookupEnvValue(got, "CUSTOM_API_KEY"); !ok || value != "from-shell" {
		t.Fatalf("CUSTOM_API_KEY = %q ok=%v, want from-shell", value, ok)
	}
	if value, ok := lookupEnvValue(got, "http_proxy"); !ok || value != "http://restored-proxy" {
		t.Fatalf("http_proxy = %q ok=%v, want restored proxy", value, ok)
	}
	if value, ok := lookupEnvValue(got, "HTTPS_PROXY"); !ok || value != "https://restored-proxy" {
		t.Fatalf("HTTPS_PROXY = %q ok=%v, want restored proxy", value, ok)
	}
	if value, ok := lookupEnvValue(got, "KEEP_ME"); !ok || value != "1" {
		t.Fatalf("KEEP_ME = %q ok=%v, want 1", value, ok)
	}
	if count := countEnvKey(got, "http_proxy"); count != 1 {
		t.Fatalf("http_proxy count = %d, want 1", count)
	}
}

func writeCodexConfigForTest(t *testing.T, codexHome string, body string) {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", codexHome, err)
	}
	path := filepath.Join(codexHome, codexConfigFileName)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func countEnvKey(env []string, key string) int {
	count := 0
	for _, entry := range env {
		currentKey, _, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			count++
		}
	}
	return count
}
