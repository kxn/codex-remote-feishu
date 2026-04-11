package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func unsetUnifiedConfigOverride(t *testing.T) {
	t.Helper()
	t.Setenv(UnifiedConfigEnvPath, "")
}

func TestLoadWrapperConfigRejectsLegacyEnvFiles(t *testing.T) {
	unsetUnifiedConfigOverride(t)
	xdgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgHome)

	legacyPath := filepath.Join(xdgHome, "codex-remote", "config.env")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatalf("mkdir legacy config dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("RELAY_SERVER_URL=ws://127.0.0.1:9600/ws/agent\n"), 0o600); err != nil {
		t.Fatalf("write legacy env: %v", err)
	}

	_, err := LoadWrapperConfig()
	if err == nil || !strings.Contains(err.Error(), "legacy env config files are no longer supported") {
		t.Fatalf("expected legacy env config rejection, got %v", err)
	}
}

func TestDefaultAppConfigIncludesPreviewRootFolderName(t *testing.T) {
	unsetUnifiedConfigOverride(t)
	cfg := DefaultAppConfig()
	if cfg.Storage.PreviewRootFolderName != "Codex Remote Previews" {
		t.Fatalf("PreviewRootFolderName = %q, want %q", cfg.Storage.PreviewRootFolderName, "Codex Remote Previews")
	}
}

func TestDefaultAppConfigIncludesTryCloudflareLaunchTimeout(t *testing.T) {
	unsetUnifiedConfigOverride(t)
	cfg := DefaultAppConfig()
	if cfg.ExternalAccess.Provider.TryCloudflare.LaunchTimeoutSeconds != defaultTryCloudflareLaunchTimeoutSeconds {
		t.Fatalf(
			"LaunchTimeoutSeconds = %d, want %d",
			cfg.ExternalAccess.Provider.TryCloudflare.LaunchTimeoutSeconds,
			defaultTryCloudflareLaunchTimeoutSeconds,
		)
	}
}

func TestLoadServicesConfigUsesUnifiedConfigEnvOverride(t *testing.T) {
	unsetUnifiedConfigOverride(t)
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
	unsetUnifiedConfigOverride(t)
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
	unsetUnifiedConfigOverride(t)
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
	if err := os.WriteFile(filepath.Join(xdgHome, "codex-remote", "wrapper.env"), []byte("RELAY_SERVER_URL=ws://127.0.0.1:9810/ws/agent\nCODEX_REAL_BINARY=/legacy/codex\n"), 0o600); err != nil {
		t.Fatalf("write wrapper.env: %v", err)
	}
	if err := os.WriteFile(filepath.Join(xdgHome, "codex-remote", "services.env"), []byte("RELAY_PORT=9810\nRELAY_API_PORT=9811\nFEISHU_APP_ID=cli_legacy\nFEISHU_APP_SECRET=secret_legacy\n"), 0o600); err != nil {
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

func TestLoadAppConfigAtPathRejectsLegacyEnvPath(t *testing.T) {
	unsetUnifiedConfigOverride(t)
	path := filepath.Join(t.TempDir(), "wrapper.env")
	if err := os.WriteFile(path, []byte("RELAY_SERVER_URL=ws://127.0.0.1:9900/ws/agent\n"), 0o600); err != nil {
		t.Fatalf("write wrapper env: %v", err)
	}

	_, err := LoadAppConfigAtPath(path)
	if err == nil || !strings.Contains(err.Error(), "legacy env config file") {
		t.Fatalf("expected legacy env path rejection, got %v", err)
	}
}

func TestLoadersRejectLegacySplitFilesWhenJSONMissing(t *testing.T) {
	unsetUnifiedConfigOverride(t)
	baseDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(baseDir, ".config"))

	configDir := filepath.Join(baseDir, ".config", "codex-remote")
	wrapperPath := filepath.Join(configDir, "wrapper.env")
	servicesPath := filepath.Join(configDir, "services.env")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(wrapperPath, []byte("RELAY_SERVER_URL=ws://127.0.0.1:9900/ws/agent\nCODEX_REAL_BINARY=/legacy/codex\n"), 0o600); err != nil {
		t.Fatalf("write wrapper env: %v", err)
	}
	if err := os.WriteFile(servicesPath, []byte("RELAY_PORT=9900\nRELAY_API_PORT=9901\nFEISHU_APP_ID=cli_legacy\nFEISHU_APP_SECRET=secret_legacy\n"), 0o600); err != nil {
		t.Fatalf("write services env: %v", err)
	}

	_, err := LoadServicesConfig()
	if err == nil || !strings.Contains(err.Error(), "legacy env config files are no longer supported") {
		t.Fatalf("expected legacy split file rejection, got %v", err)
	}
	for _, legacyPath := range []string{wrapperPath, servicesPath} {
		if !strings.Contains(err.Error(), legacyPath) {
			t.Fatalf("expected error to mention %s, got %v", legacyPath, err)
		}
	}
}

func TestLoadersIgnoreWorkingDirectoryDotEnv(t *testing.T) {
	unsetUnifiedConfigOverride(t)
	projectDir := t.TempDir()
	t.Chdir(projectDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), ".config"))

	dotEnvPath := filepath.Join(projectDir, ".env")
	if err := os.WriteFile(dotEnvPath, []byte("FEISHU_APP_ID=cli_dotenv\nFEISHU_APP_SECRET=secret_dotenv\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	cfg, err := LoadServicesConfig()
	if err != nil {
		t.Fatalf("LoadServicesConfig: %v", err)
	}
	if cfg.FeishuAppID != "" || cfg.FeishuAppSecret != "" {
		t.Fatalf("working directory .env should not be treated as legacy config, got %q/%q", cfg.FeishuAppID, cfg.FeishuAppSecret)
	}
	if _, err := os.Stat(dotEnvPath); err != nil {
		t.Fatalf("expected .env to remain untouched: %v", err)
	}
	backups, err := filepath.Glob(dotEnvPath + ".migrated-*.bak")
	if err != nil {
		t.Fatalf("glob backups: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("did not expect migration backup for .env, got %v", backups)
	}
}
