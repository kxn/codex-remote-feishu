package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapWritesConfigsAndState(t *testing.T) {
	baseDir := t.TempDir()
	settingsPath := filepath.Join(baseDir, "Code", "User", "settings.json")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:            baseDir,
		WrapperBinary:      "/usr/local/bin/relay-wrapper",
		RelayServerURL:     "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary:    "/usr/local/bin/codex",
		IntegrationMode:    IntegrationEditorSettings,
		VSCodeSettingsPath: settingsPath,
		FeishuAppID:        "cli_xxx",
		FeishuAppSecret:    "secret",
		UseSystemProxy:     false,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	wrapperRaw, err := os.ReadFile(state.WrapperConfigPath)
	if err != nil {
		t.Fatalf("read wrapper config: %v", err)
	}
	if !strings.Contains(string(wrapperRaw), "RELAY_SERVER_URL=ws://127.0.0.1:9500/ws/agent") {
		t.Fatalf("unexpected wrapper config: %s", wrapperRaw)
	}

	serviceRaw, err := os.ReadFile(state.ServicesConfigPath)
	if err != nil {
		t.Fatalf("read services config: %v", err)
	}
	if !strings.Contains(string(serviceRaw), "FEISHU_APP_ID=cli_xxx") {
		t.Fatalf("unexpected services config: %s", serviceRaw)
	}

	settingsRaw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(settingsRaw), "relay-wrapper") {
		t.Fatalf("expected settings to contain wrapper path, got %s", settingsRaw)
	}
}

func TestBootstrapManagedShimWritesBundleEntrypoint(t *testing.T) {
	baseDir := t.TempDir()
	entrypoint := filepath.Join(baseDir, ".vscode-server", "extensions", "openai.chatgpt-test", "bin", "linux-x86_64", "codex")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:          baseDir,
		WrapperBinary:    "/usr/local/bin/relay-wrapper",
		RelayServerURL:   "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary:  filepath.Join(filepath.Dir(entrypoint), "codex.real"),
		IntegrationMode:  IntegrationManagedShim,
		BundleEntrypoint: entrypoint,
	})
	if err != nil {
		t.Fatalf("bootstrap managed shim: %v", err)
	}

	if state.BundleEntrypoint != entrypoint {
		t.Fatalf("unexpected bundle entrypoint in state: %s", state.BundleEntrypoint)
	}

	raw, err := os.ReadFile(entrypoint)
	if err != nil {
		t.Fatalf("read bundle entrypoint: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, "relay-wrapper") {
		t.Fatalf("expected bundle entrypoint to reference wrapper path, got %s", text)
	}
	if !strings.Contains(text, "CODEX_REAL_BINARY") {
		t.Fatalf("expected bundle entrypoint to export real binary, got %s", text)
	}
}

func TestBootstrapPreservesExistingFeishuSecretsWhenFlagsAreEmpty(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, ".config", "codex-relay")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	servicesPath := filepath.Join(configDir, "services.env")
	if err := os.WriteFile(servicesPath, []byte("RELAY_PORT=9500\nRELAY_API_PORT=9501\nFEISHU_APP_ID=cli_existing\nFEISHU_APP_SECRET=secret_existing\nFEISHU_USE_SYSTEM_PROXY=false\n"), 0o600); err != nil {
		t.Fatalf("seed services env: %v", err)
	}

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:         baseDir,
		WrapperBinary:   "/usr/local/bin/relay-wrapper",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		IntegrationMode: IntegrationEditorSettings,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	serviceRaw, err := os.ReadFile(state.ServicesConfigPath)
	if err != nil {
		t.Fatalf("read services config: %v", err)
	}
	text := string(serviceRaw)
	if !strings.Contains(text, "FEISHU_APP_ID=cli_existing") {
		t.Fatalf("expected app id to be preserved, got %s", text)
	}
	if !strings.Contains(text, "FEISHU_APP_SECRET=secret_existing") {
		t.Fatalf("expected app secret to be preserved, got %s", text)
	}
}
