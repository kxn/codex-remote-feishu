package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/editor"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestBootstrapWritesConfigsAndState(t *testing.T) {
	baseDir := t.TempDir()
	settingsPath := filepath.Join(baseDir, "Code", "User", "settings.json")
	sourceDir := filepath.Join(baseDir, "source-bin")
	installBinDir := filepath.Join(baseDir, "installed-bin")
	binaryPath := seedBinary(t, filepath.Join(sourceDir, "codex-remote"), "unified-bin")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:            baseDir,
		InstallBinDir:      installBinDir,
		BinaryPath:         binaryPath,
		CurrentVersion:     "dev",
		RelayServerURL:     "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary:    "/usr/local/bin/codex",
		Integrations:       []WrapperIntegrationMode{IntegrationEditorSettings},
		VSCodeSettingsPath: settingsPath,
		FeishuAppID:        "cli_xxx",
		FeishuAppSecret:    "secret",
		UseSystemProxy:     false,
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	if state.ConfigPath != filepath.Join(baseDir, ".config", "codex-remote", "config.json") {
		t.Fatalf("unexpected config path: %s", state.ConfigPath)
	}
	if state.WrapperConfigPath != state.ConfigPath || state.ServicesConfigPath != state.ConfigPath {
		t.Fatalf("expected wrapper/services config paths to match unified config path")
	}

	cfg := loadAppConfigForTest(t, state.ConfigPath)
	if cfg.Relay.ServerURL != "ws://127.0.0.1:9500/ws/agent" {
		t.Fatalf("unexpected relay server url: %s", cfg.Relay.ServerURL)
	}
	if cfg.Wrapper.CodexRealBinary != "/usr/local/bin/codex" {
		t.Fatalf("unexpected codex real binary: %s", cfg.Wrapper.CodexRealBinary)
	}
	app := config.SelectRuntimeFeishuApp(cfg.Feishu.Apps)
	if app.AppID != "cli_xxx" || app.AppSecret != "secret" {
		t.Fatalf("unexpected feishu app: %#v", app)
	}

	settingsRaw, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if !strings.Contains(string(settingsRaw), state.InstalledWrapperBinary) {
		t.Fatalf("expected settings to contain wrapper path, got %s", settingsRaw)
	}
	wantBinary := filepath.Join(installBinDir, filepath.Base(binaryPath))
	if state.InstalledBinary != wantBinary {
		t.Fatalf("unexpected installed binary path: %s", state.InstalledBinary)
	}
	if state.InstalledWrapperBinary != wantBinary {
		t.Fatalf("unexpected installed wrapper alias path: %s", state.InstalledWrapperBinary)
	}
	if state.InstalledRelaydBinary != wantBinary {
		t.Fatalf("unexpected installed relayd alias path: %s", state.InstalledRelaydBinary)
	}
	if state.InstallSource != InstallSourceRepo {
		t.Fatalf("install source = %q, want repo", state.InstallSource)
	}
	if state.CurrentTrack != ReleaseTrackAlpha {
		t.Fatalf("current track = %q, want alpha", state.CurrentTrack)
	}
	if state.CurrentVersion != "dev" {
		t.Fatalf("current version = %q, want dev", state.CurrentVersion)
	}
	if state.CurrentBinaryPath != wantBinary {
		t.Fatalf("current binary path = %q, want %q", state.CurrentBinaryPath, wantBinary)
	}
	if state.VersionsRoot != filepath.Join(baseDir, ".local", "share", "codex-remote", "releases") {
		t.Fatalf("versions root = %q", state.VersionsRoot)
	}
	if state.CurrentSlot != "" {
		t.Fatalf("current slot = %q, want empty for repo bootstrap", state.CurrentSlot)
	}
	if state.BaseDir != baseDir {
		t.Fatalf("base dir = %q, want %q", state.BaseDir, baseDir)
	}
	if state.ServiceManager != ServiceManagerDetached {
		t.Fatalf("service manager = %q, want detached", state.ServiceManager)
	}
}

func TestBootstrapSystemdUserPersistsLinuxServiceMetadata(t *testing.T) {
	baseDir := t.TempDir()
	binaryPath := seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:        baseDir,
		BinaryPath:     binaryPath,
		ServiceManager: ServiceManagerSystemdUser,
		CurrentVersion: "dev",
		RelayServerURL: "ws://127.0.0.1:9500/ws/agent",
	})
	if err != nil {
		t.Fatalf("bootstrap systemd_user: %v", err)
	}
	if state.ServiceManager != ServiceManagerSystemdUser {
		t.Fatalf("ServiceManager = %q, want systemd_user", state.ServiceManager)
	}
	if state.ServiceUnitPath != filepath.Join(baseDir, ".config", "systemd", "user", "codex-remote.service") {
		t.Fatalf("ServiceUnitPath = %q", state.ServiceUnitPath)
	}
}

func TestBootstrapManagedShimCopiesWrapperAndPreservesRealBinary(t *testing.T) {
	baseDir := t.TempDir()
	entrypoint := filepath.Join(baseDir, ".vscode-server", "extensions", "openai.chatgpt-test", "bin", "linux-x86_64", "codex")
	sourceDir := filepath.Join(baseDir, "source-bin")
	installBinDir := filepath.Join(baseDir, "installed-bin")
	binaryPath := seedBinary(t, filepath.Join(sourceDir, "codex-remote"), "codex-remote")
	seedBinary(t, entrypoint, "original-codex")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:          baseDir,
		InstallBinDir:    installBinDir,
		BinaryPath:       binaryPath,
		CurrentVersion:   "dev",
		RelayServerURL:   "ws://127.0.0.1:9500/ws/agent",
		Integrations:     []WrapperIntegrationMode{IntegrationManagedShim},
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
	if string(raw) != "codex-remote" {
		t.Fatalf("expected unified binary content in entrypoint, got %q", string(raw))
	}

	realRaw, err := os.ReadFile(editor.ManagedShimRealBinaryPath(entrypoint))
	if err != nil {
		t.Fatalf("read real binary: %v", err)
	}
	if string(realRaw) != "original-codex" {
		t.Fatalf("expected preserved real binary content, got %q", string(realRaw))
	}

	cfg := loadAppConfigForTest(t, state.ConfigPath)
	if cfg.Wrapper.CodexRealBinary != editor.ManagedShimRealBinaryPath(entrypoint) {
		t.Fatalf("expected config to point to managed shim real binary, got %s", cfg.Wrapper.CodexRealBinary)
	}
}

func TestBootstrapOnlyWritesConfigWithoutTouchingVSCode(t *testing.T) {
	baseDir := t.TempDir()
	settingsPath := filepath.Join(baseDir, "Code", "User", "settings.json")
	entrypoint := filepath.Join(baseDir, ".vscode-server", "extensions", "openai.chatgpt-test", "bin", "linux-x86_64", "codex")
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "bootstrap-bin")
	seedBinary(t, entrypoint, "original-codex")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:            baseDir,
		InstallBinDir:      filepath.Join(baseDir, "installed-bin"),
		BinaryPath:         sourceBinary,
		CurrentVersion:     "dev",
		RelayServerURL:     "ws://127.0.0.1:9500/ws/agent",
		VSCodeSettingsPath: settingsPath,
		BundleEntrypoint:   entrypoint,
		BootstrapOnly:      true,
	})
	if err != nil {
		t.Fatalf("bootstrap only: %v", err)
	}

	cfg := loadAppConfigForTest(t, state.ConfigPath)
	if cfg.Wrapper.IntegrationMode != "none" {
		t.Fatalf("wrapper integration mode = %q, want none", cfg.Wrapper.IntegrationMode)
	}
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("expected settings.json to stay untouched, stat err=%v", err)
	}
	raw, err := os.ReadFile(entrypoint)
	if err != nil {
		t.Fatalf("read bundle entrypoint: %v", err)
	}
	if string(raw) != "original-codex" {
		t.Fatalf("expected bundle entrypoint to remain unchanged, got %q", string(raw))
	}
	if len(state.Integrations) != 0 {
		t.Fatalf("state integrations = %#v, want none", state.Integrations)
	}
}

func TestBootstrapPreservesExistingFeishuSecretsWhenFlagsAreEmpty(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, ".config", "codex-remote")
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
		BinaryPath:      seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin"),
		CurrentVersion:  "dev",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		Integrations:    []WrapperIntegrationMode{IntegrationEditorSettings},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	cfg := loadAppConfigForTest(t, state.ConfigPath)
	app := config.SelectRuntimeFeishuApp(cfg.Feishu.Apps)
	if app.AppID != "cli_existing" {
		t.Fatalf("expected app id to be preserved, got %#v", app)
	}
	if app.AppSecret != "secret_existing" {
		t.Fatalf("expected app secret to be preserved, got %#v", app)
	}
	assertMigratedBackupExists(t, servicesPath)
}

func TestBootstrapPreservesExistingDebugRelayFlowFlag(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, ".config", "codex-remote")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Debug.RelayFlow = true
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("seed unified config: %v", err)
	}

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:         baseDir,
		BinaryPath:      seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin"),
		CurrentVersion:  "dev",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		Integrations:    []WrapperIntegrationMode{IntegrationEditorSettings},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	loaded := loadAppConfigForTest(t, state.ConfigPath)
	if !loaded.Debug.RelayFlow {
		t.Fatalf("expected debug relay flow flag to be preserved, got %#v", loaded.Debug)
	}
}

func TestBootstrapPreservesExistingDebugRelayRawFlag(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, ".config", "codex-remote")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Debug.RelayRaw = true
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("seed unified config: %v", err)
	}

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:         baseDir,
		BinaryPath:      seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin"),
		CurrentVersion:  "dev",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		Integrations:    []WrapperIntegrationMode{IntegrationEditorSettings},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	loaded := loadAppConfigForTest(t, state.ConfigPath)
	if !loaded.Debug.RelayRaw {
		t.Fatalf("expected debug relay raw flag to be preserved, got %#v", loaded.Debug)
	}
}

func TestBootstrapPreservesExistingPprofConfig(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, ".config", "codex-remote")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Debug.Pprof = &config.PprofSettings{
		Enabled:    true,
		ListenHost: "127.0.0.1",
		ListenPort: 17601,
	}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("seed unified config: %v", err)
	}

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:         baseDir,
		BinaryPath:      seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin"),
		CurrentVersion:  "dev",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		Integrations:    []WrapperIntegrationMode{IntegrationEditorSettings},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	loaded := loadAppConfigForTest(t, state.ConfigPath)
	if loaded.Debug.Pprof == nil {
		t.Fatal("expected pprof config to be preserved")
	}
	if !loaded.Debug.Pprof.Enabled {
		t.Fatalf("expected pprof to stay enabled, got %#v", loaded.Debug.Pprof)
	}
	if loaded.Debug.Pprof.ListenHost != "127.0.0.1" || loaded.Debug.Pprof.ListenPort != 17601 {
		t.Fatalf("expected pprof config to be preserved, got %#v", loaded.Debug.Pprof)
	}
}

func TestBootstrapAcceptsMatchingDeprecatedBinaryFlags(t *testing.T) {
	baseDir := t.TempDir()
	sourceBinary := seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:        baseDir,
		WrapperBinary:  sourceBinary,
		RelaydBinary:   sourceBinary,
		CurrentVersion: "dev",
		RelayServerURL: "ws://127.0.0.1:9500/ws/agent",
	})
	if err != nil {
		t.Fatalf("bootstrap with deprecated flags: %v", err)
	}
	if state.InstalledBinary != sourceBinary {
		t.Fatalf("InstalledBinary = %q, want %q", state.InstalledBinary, sourceBinary)
	}
}

func TestBootstrapRejectsMismatchedDeprecatedBinaryFlags(t *testing.T) {
	baseDir := t.TempDir()
	service := NewService()
	_, err := service.Bootstrap(Options{
		BaseDir:        baseDir,
		WrapperBinary:  seedBinary(t, filepath.Join(baseDir, "source-bin", "wrapper"), "wrapper"),
		RelaydBinary:   seedBinary(t, filepath.Join(baseDir, "source-bin", "daemon"), "daemon"),
		RelayServerURL: "ws://127.0.0.1:9500/ws/agent",
	})
	if err == nil || !strings.Contains(err.Error(), "single-binary install requires -binary") {
		t.Fatalf("expected mismatched deprecated binary error, got %v", err)
	}
}

func TestBootstrapMergesLegacySplitConfigFiles(t *testing.T) {
	baseDir := t.TempDir()
	configDir := filepath.Join(baseDir, ".config", "codex-remote")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	wrapperPath := filepath.Join(configDir, "wrapper.env")
	servicesPath := filepath.Join(configDir, "services.env")
	if err := os.WriteFile(wrapperPath, []byte("RELAY_SERVER_URL=ws://127.0.0.1:9910/ws/agent\nCODEX_REAL_BINARY=/legacy/codex\n"), 0o600); err != nil {
		t.Fatalf("seed wrapper env: %v", err)
	}
	if err := os.WriteFile(servicesPath, []byte("RELAY_PORT=9910\nRELAY_API_PORT=9911\nFEISHU_APP_ID=cli_old\nFEISHU_APP_SECRET=secret_old\n"), 0o600); err != nil {
		t.Fatalf("seed services env: %v", err)
	}

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:         baseDir,
		BinaryPath:      seedBinary(t, filepath.Join(baseDir, "source-bin", "codex-remote"), "binary-bin"),
		CurrentVersion:  "dev",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		Integrations:    []WrapperIntegrationMode{IntegrationEditorSettings},
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	cfg := loadAppConfigForTest(t, state.ConfigPath)
	app := config.SelectRuntimeFeishuApp(cfg.Feishu.Apps)
	if app.AppID != "cli_old" || app.AppSecret != "secret_old" {
		t.Fatalf("expected legacy services values in unified config, got %#v", app)
	}
	assertMigratedBackupExists(t, wrapperPath)
	assertMigratedBackupExists(t, servicesPath)
}

func seedBinary(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func loadAppConfigForTest(t *testing.T, path string) config.AppConfig {
	t.Helper()
	loaded, err := config.LoadAppConfigAtPath(path)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(%s): %v", path, err)
	}
	return loaded.Config
}

func assertMigratedBackupExists(t *testing.T, legacyPath string) {
	t.Helper()
	backups, err := filepath.Glob(legacyPath + ".migrated-*.bak")
	if err != nil {
		t.Fatalf("glob backups for %s: %v", legacyPath, err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one migrated backup for %s, got %v", legacyPath, backups)
	}
}

func TestBootstrapPreservesReleaseInstallMetadata(t *testing.T) {
	baseDir := t.TempDir()
	releasesRoot := filepath.Join(baseDir, ".local", "share", "codex-remote", "releases")
	sourceBinary := seedBinary(t, filepath.Join(releasesRoot, "v1.2.3-beta.4", "codex-remote"), "release-bin")
	installBinDir := filepath.Join(baseDir, "installed-bin")

	service := NewService()
	state, err := service.Bootstrap(Options{
		BaseDir:         baseDir,
		InstallBinDir:   installBinDir,
		BinaryPath:      sourceBinary,
		CurrentVersion:  "v1.2.3-beta.4",
		InstallSource:   InstallSourceRelease,
		CurrentTrack:    ReleaseTrackBeta,
		VersionsRoot:    releasesRoot,
		CurrentSlot:     "v1.2.3-beta.4",
		RelayServerURL:  "ws://127.0.0.1:9500/ws/agent",
		CodexRealBinary: "/usr/local/bin/codex",
		Integrations:    []WrapperIntegrationMode{IntegrationEditorSettings},
	})
	if err != nil {
		t.Fatalf("bootstrap release metadata: %v", err)
	}

	wantBinary := filepath.Join(installBinDir, "codex-remote")
	if state.InstallSource != InstallSourceRelease {
		t.Fatalf("install source = %q, want release", state.InstallSource)
	}
	if state.CurrentTrack != ReleaseTrackBeta {
		t.Fatalf("current track = %q, want beta", state.CurrentTrack)
	}
	if state.CurrentVersion != "v1.2.3-beta.4" {
		t.Fatalf("current version = %q, want v1.2.3-beta.4", state.CurrentVersion)
	}
	if state.CurrentBinaryPath != wantBinary {
		t.Fatalf("current binary path = %q, want %q", state.CurrentBinaryPath, wantBinary)
	}
	if state.VersionsRoot != releasesRoot {
		t.Fatalf("versions root = %q, want %q", state.VersionsRoot, releasesRoot)
	}
	if state.CurrentSlot != "v1.2.3-beta.4" {
		t.Fatalf("current slot = %q, want v1.2.3-beta.4", state.CurrentSlot)
	}
}
