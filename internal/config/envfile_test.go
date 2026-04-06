package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWrapperConfigMigratesLegacyUnifiedEnvToJSON(t *testing.T) {
	xdgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgHome)

	legacyPath := filepath.Join(xdgHome, "codex-remote", "config.env")
	if err := WriteEnvFile(legacyPath, map[string]string{
		"RELAY_SERVER_URL":               "ws://127.0.0.1:9600/ws/agent",
		"CODEX_REAL_BINARY":              "/opt/codex",
		"CODEX_REMOTE_WRAPPER_NAME_MODE": "workspace_basename",
		DebugRelayFlowEnv:                "true",
		DebugRelayRawEnv:                 "true",
	}); err != nil {
		t.Fatalf("write legacy env: %v", err)
	}

	cfg, err := LoadWrapperConfig()
	if err != nil {
		t.Fatalf("LoadWrapperConfig: %v", err)
	}

	configPath := filepath.Join(xdgHome, "codex-remote", "config.json")
	if cfg.ConfigPath != configPath {
		t.Fatalf("ConfigPath = %q, want %q", cfg.ConfigPath, configPath)
	}
	if cfg.RelayServerURL != "ws://127.0.0.1:9600/ws/agent" {
		t.Fatalf("RelayServerURL = %q", cfg.RelayServerURL)
	}
	if cfg.CodexRealBinary != "/opt/codex" {
		t.Fatalf("CodexRealBinary = %q", cfg.CodexRealBinary)
	}
	if !cfg.DebugRelayFlow {
		t.Fatal("expected DebugRelayFlow to be true")
	}
	if !cfg.DebugRelayRaw {
		t.Fatal("expected DebugRelayRaw to be true")
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if !strings.Contains(string(raw), "\"serverURL\": \"ws://127.0.0.1:9600/ws/agent\"") {
		t.Fatalf("unexpected migrated config: %s", raw)
	}
	backups, err := filepath.Glob(legacyPath + ".migrated-*.bak")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one migrated backup, got %v", backups)
	}
}

func TestLoadServicesConfigUsesUnifiedConfigEnvOverride(t *testing.T) {
	xdgHome := t.TempDir()
	overrideDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgHome)

	overridePath := filepath.Join(overrideDir, "custom.json")
	cfg := DefaultAppConfig()
	cfg.Relay.ListenHost = "0.0.0.0"
	cfg.Relay.ListenPort = 9700
	cfg.Relay.ServerURL = "ws://127.0.0.1:9700/ws/agent"
	cfg.Admin.ListenHost = "0.0.0.0"
	cfg.Admin.ListenPort = 9701
	cfg.Feishu.UseSystemProxy = true
	cfg.Feishu.Apps = []FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_override",
		AppSecret: "secret_override",
		Enabled:   boolPtr(true),
	}}
	cfg.Debug.RelayFlow = true
	cfg.Debug.RelayRaw = true
	if err := WriteAppConfig(overridePath, cfg); err != nil {
		t.Fatalf("write override json: %v", err)
	}
	t.Setenv(UnifiedConfigEnvPath, overridePath)

	loaded, err := LoadServicesConfig()
	if err != nil {
		t.Fatalf("LoadServicesConfig: %v", err)
	}
	if loaded.ConfigPath != overridePath {
		t.Fatalf("ConfigPath = %q, want %q", loaded.ConfigPath, overridePath)
	}
	if loaded.FeishuGatewayID != "main" {
		t.Fatalf("FeishuGatewayID = %q, want main", loaded.FeishuGatewayID)
	}
	if loaded.RelayHost != "0.0.0.0" || loaded.RelayAPIHost != "0.0.0.0" {
		t.Fatalf("hosts = %q/%q", loaded.RelayHost, loaded.RelayAPIHost)
	}
	if loaded.RelayPort != "9700" || loaded.RelayAPIPort != "9701" {
		t.Fatalf("ports = %q/%q", loaded.RelayPort, loaded.RelayAPIPort)
	}
	if loaded.FeishuAppID != "cli_override" || loaded.FeishuAppSecret != "secret_override" {
		t.Fatalf("feishu = %q/%q", loaded.FeishuAppID, loaded.FeishuAppSecret)
	}
	if !loaded.FeishuUseSystemProxy {
		t.Fatal("expected FeishuUseSystemProxy to be true")
	}
	if !loaded.DebugRelayFlow {
		t.Fatal("expected DebugRelayFlow to be true")
	}
	if !loaded.DebugRelayRaw {
		t.Fatal("expected DebugRelayRaw to be true")
	}
}

func TestLoadServicesConfigAllowsHostEnvOverrides(t *testing.T) {
	xdgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	t.Setenv("RELAY_HOST", "0.0.0.0")
	t.Setenv("RELAY_API_HOST", "0.0.0.0")

	cfg, err := LoadServicesConfig()
	if err != nil {
		t.Fatalf("LoadServicesConfig: %v", err)
	}
	if cfg.RelayHost != "0.0.0.0" || cfg.RelayAPIHost != "0.0.0.0" {
		t.Fatalf("hosts = %q/%q", cfg.RelayHost, cfg.RelayAPIHost)
	}
	if cfg.RelayPort != "9500" || cfg.RelayAPIPort != "9501" {
		t.Fatalf("ports = %q/%q", cfg.RelayPort, cfg.RelayAPIPort)
	}
}

func TestLoadersPreferJSONOverLegacySplitFiles(t *testing.T) {
	xdgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgHome)

	configPath := filepath.Join(xdgHome, "codex-remote", "config.json")
	cfg := DefaultAppConfig()
	cfg.Relay.ServerURL = "ws://127.0.0.1:9800/ws/agent"
	cfg.Relay.ListenPort = 9800
	cfg.Admin.ListenPort = 9801
	cfg.Wrapper.CodexRealBinary = "/json/codex"
	cfg.Feishu.Apps = []FeishuAppConfig{{
		ID:        "json",
		Name:      "JSON",
		AppID:     "cli_json",
		AppSecret: "secret_json",
		Enabled:   boolPtr(true),
	}}
	if err := WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	if err := WriteEnvFile(filepath.Join(xdgHome, "codex-remote", "wrapper.env"), map[string]string{
		"RELAY_SERVER_URL":  "ws://127.0.0.1:9810/ws/agent",
		"CODEX_REAL_BINARY": "/legacy/codex",
	}); err != nil {
		t.Fatalf("write wrapper.env: %v", err)
	}
	if err := WriteEnvFile(filepath.Join(xdgHome, "codex-remote", "services.env"), map[string]string{
		"RELAY_PORT":        "9810",
		"RELAY_API_PORT":    "9811",
		"FEISHU_APP_ID":     "cli_legacy",
		"FEISHU_APP_SECRET": "secret_legacy",
	}); err != nil {
		t.Fatalf("write services.env: %v", err)
	}

	wrapperCfg, err := LoadWrapperConfig()
	if err != nil {
		t.Fatalf("LoadWrapperConfig: %v", err)
	}
	if wrapperCfg.ConfigPath != configPath {
		t.Fatalf("wrapper ConfigPath = %q, want %q", wrapperCfg.ConfigPath, configPath)
	}
	if wrapperCfg.CodexRealBinary != "/json/codex" {
		t.Fatalf("wrapper CodexRealBinary = %q", wrapperCfg.CodexRealBinary)
	}

	servicesCfg, err := LoadServicesConfig()
	if err != nil {
		t.Fatalf("LoadServicesConfig: %v", err)
	}
	if servicesCfg.ConfigPath != configPath {
		t.Fatalf("services ConfigPath = %q, want %q", servicesCfg.ConfigPath, configPath)
	}
	if servicesCfg.FeishuAppID != "cli_json" || servicesCfg.FeishuAppSecret != "secret_json" {
		t.Fatalf("services feishu = %q/%q", servicesCfg.FeishuAppID, servicesCfg.FeishuAppSecret)
	}
}

func TestLoadersMigrateLegacySplitFilesAndUpdateInstallState(t *testing.T) {
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(baseDir, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(baseDir, ".local", "share"))

	configDir := filepath.Join(baseDir, ".config", "codex-remote")
	wrapperPath := filepath.Join(configDir, "wrapper.env")
	servicesPath := filepath.Join(configDir, "services.env")
	if err := WriteEnvFile(wrapperPath, map[string]string{
		"RELAY_SERVER_URL":  "ws://127.0.0.1:9900/ws/agent",
		"CODEX_REAL_BINARY": "/legacy/codex",
	}); err != nil {
		t.Fatalf("write wrapper env: %v", err)
	}
	if err := WriteEnvFile(servicesPath, map[string]string{
		"RELAY_PORT":        "9900",
		"RELAY_API_PORT":    "9901",
		"FEISHU_APP_ID":     "cli_legacy",
		"FEISHU_APP_SECRET": "secret_legacy",
	}); err != nil {
		t.Fatalf("write services env: %v", err)
	}

	statePath := filepath.Join(baseDir, ".local", "share", "codex-remote", "install-state.json")
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		t.Fatalf("mkdir install state dir: %v", err)
	}
	stateRaw := `{"configPath":"` + filepath.Join(configDir, "config.env") + `","wrapperConfigPath":"` + wrapperPath + `","servicesConfigPath":"` + servicesPath + `"}`
	if err := os.WriteFile(statePath, []byte(stateRaw), 0o644); err != nil {
		t.Fatalf("write install-state.json: %v", err)
	}

	wrapperCfg, err := LoadWrapperConfig()
	if err != nil {
		t.Fatalf("LoadWrapperConfig: %v", err)
	}
	servicesCfg, err := LoadServicesConfig()
	if err != nil {
		t.Fatalf("LoadServicesConfig: %v", err)
	}

	configPath := filepath.Join(configDir, "config.json")
	if wrapperCfg.ConfigPath != configPath || servicesCfg.ConfigPath != configPath {
		t.Fatalf("unexpected config paths: wrapper=%q services=%q want=%q", wrapperCfg.ConfigPath, servicesCfg.ConfigPath, configPath)
	}
	if servicesCfg.FeishuAppID != "cli_legacy" || servicesCfg.FeishuAppSecret != "secret_legacy" {
		t.Fatalf("unexpected migrated feishu values: %q/%q", servicesCfg.FeishuAppID, servicesCfg.FeishuAppSecret)
	}

	updatedState, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read install-state.json: %v", err)
	}
	if strings.Count(string(updatedState), configPath) != 3 {
		t.Fatalf("expected all state paths updated to %q, got %s", configPath, updatedState)
	}

	for _, legacyPath := range []string{wrapperPath, servicesPath} {
		backups, err := filepath.Glob(legacyPath + ".migrated-*.bak")
		if err != nil {
			t.Fatalf("glob backups for %s: %v", legacyPath, err)
		}
		if len(backups) != 1 {
			t.Fatalf("expected one backup for %s, got %v", legacyPath, backups)
		}
	}
}
