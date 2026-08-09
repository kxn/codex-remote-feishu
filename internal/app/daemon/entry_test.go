package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type stubRunnableDaemon struct {
	bindErr    error
	bindCalled bool
	runCalled  bool
	pprofURL   string
}

func (s *stubRunnableDaemon) Bind() error {
	s.bindCalled = true
	return s.bindErr
}

func (s *stubRunnableDaemon) Run(context.Context) error {
	s.runCalled = true
	return nil
}

func (s *stubRunnableDaemon) PprofURL() string {
	return s.pprofURL
}

func TestRuntimeGatewayAppsUsesConfigApps(t *testing.T) {
	enabled := true
	disabled := false
	appConfig := config.DefaultAppConfig()
	appConfig.Storage.PreviewRootFolderName = "Codex Remote Tests"
	appConfig.Feishu.Apps = []config.FeishuAppConfig{
		{
			ID:        "app-1",
			Name:      "App 1",
			AppID:     "cli_app_1",
			AppSecret: "secret_app_1",
			Enabled:   &enabled,
		},
		{
			ID:        "app-2",
			Name:      "App 2",
			AppID:     "cli_app_2",
			AppSecret: "secret_app_2",
			Enabled:   &disabled,
		},
	}
	services := config.ServicesConfig{FeishuUseSystemProxy: true}
	paths := relayruntime.Paths{StateDir: "/tmp/state"}

	apps := runtimeGatewayApps(appConfig, services, paths)
	if len(apps) != 2 {
		t.Fatalf("expected two runtime apps, got %#v", apps)
	}
	if apps[0].GatewayID != "app-1" || !apps[0].Enabled || apps[0].PreviewRootFolderName != "Codex Remote Tests" {
		t.Fatalf("unexpected first runtime app: %#v", apps[0])
	}
	if apps[1].GatewayID != "app-2" || apps[1].Enabled {
		t.Fatalf("unexpected second runtime app: %#v", apps[1])
	}
	if apps[0].PreviewStatePath != filepath.Join(paths.StateDir, "feishu-md-preview-app-1.json") {
		t.Fatalf("unexpected preview state path: %s", apps[0].PreviewStatePath)
	}
}

func TestRuntimeGatewayAppsAppliesRuntimeOverrideCredentials(t *testing.T) {
	appConfig := config.DefaultAppConfig()
	services := config.ServicesConfig{
		FeishuGatewayID: "main",
		FeishuAppID:     "cli_env",
		FeishuAppSecret: "secret_env",
	}
	paths := relayruntime.Paths{StateDir: "/tmp/state"}

	apps := runtimeGatewayApps(appConfig, services, paths)
	if len(apps) != 1 {
		t.Fatalf("expected one runtime app, got %#v", apps)
	}
	if apps[0].GatewayID != "main" || apps[0].AppID != "cli_env" || apps[0].AppSecret != "secret_env" || !apps[0].Enabled {
		t.Fatalf("unexpected runtime override app: %#v", apps[0])
	}
}

func TestBuildRuntimeGatewayAppsIncludesPrimaryLookupHook(t *testing.T) {
	appConfig := config.DefaultAppConfig()
	appConfig.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "app-1",
		AppID:     "cli_app_1",
		AppSecret: "secret_app_1",
	}}
	services := config.ServicesConfig{}
	paths := relayruntime.Paths{StateDir: "/tmp/state"}
	lookupCalls := 0

	apps := buildRuntimeGatewayApps(appConfig, services, paths, gatewayRuntimeHooks{
		PrimaryGatewayForChat: func(chatID string) string {
			lookupCalls++
			if chatID != "oc_chat" {
				t.Fatalf("chat id = %q, want oc_chat", chatID)
			}
			return "app-1"
		},
	})
	if len(apps) != 1 {
		t.Fatalf("expected one runtime app, got %#v", apps)
	}
	if apps[0].PrimaryGatewayForChat == nil {
		t.Fatal("expected PrimaryGatewayForChat hook")
	}
	if got := apps[0].PrimaryGatewayForChat("oc_chat"); got != "app-1" {
		t.Fatalf("PrimaryGatewayForChat() = %q, want app-1", got)
	}
	if lookupCalls != 1 {
		t.Fatalf("unexpected hook call count: lookup=%d", lookupCalls)
	}
}

func TestRunConfiguredDaemonSkipsBrowserWhenBindFails(t *testing.T) {
	original := browserOpener
	defer func() { browserOpener = original }()

	called := 0
	browserOpener = func(string, map[string]string) error {
		called++
		return nil
	}

	runner := &stubRunnableDaemon{bindErr: errors.New("listen tcp 127.0.0.1:9501: bind: address already in use")}
	err := runConfiguredDaemon(context.Background(), runner, startupAccessPlan{
		SetupRequired:   true,
		AutoOpenBrowser: true,
		SetupURL:        "http://localhost:9501/setup",
	}, config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIPort: "9501",
	}, map[string]string{})
	if err == nil {
		t.Fatal("expected bind failure")
	}
	if !strings.Contains(err.Error(), "bind service listeners") {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 0 {
		t.Fatalf("expected browser opener to be skipped, called=%d", called)
	}
	if runner.runCalled {
		t.Fatal("did not expect run to be called after bind failure")
	}
}

func TestBuildDaemonHeadlessBaseEnvFreezesExplicitClaudeBinary(t *testing.T) {
	home := t.TempDir()
	claudePath := filepath.Join(home, executableName("claude"))
	writeExecutableFile(t, claudePath, "#!/bin/sh\nexit 0\n")

	env := buildDaemonHeadlessBaseEnv([]string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(claudePath),
		config.ClaudeBinaryEnv + "=" + claudePath,
	}, []string{
		"https_proxy=https://proxy.internal",
	})

	value, ok := lookupEnvEntryForTest(env, config.ClaudeBinaryEnv)
	if !ok || normalizeExecutablePathForDaemonTest(value) != normalizeExecutablePathForDaemonTest(claudePath) {
		t.Fatalf("CLAUDE_BIN = %q ok=%v, want %q", value, ok, claudePath)
	}
	if value, ok := lookupEnvEntryForTest(env, "https_proxy"); !ok || !strings.Contains(value, "proxy.internal") {
		t.Fatalf("https_proxy = %q ok=%v", value, ok)
	}
}

func TestBuildDaemonHeadlessBaseEnvRefreshesProcessPATHForCommandResolution(t *testing.T) {
	home := t.TempDir()
	shellBin := filepath.Join(home, "shell-bin")
	if err := os.MkdirAll(shellBin, 0o755); err != nil {
		t.Fatalf("MkdirAll shell bin: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("export PATH=\""+shellBin+":$PATH\"\n"), 0o644); err != nil {
		t.Fatalf("write .bashrc: %v", err)
	}
	currentPath := strings.Join([]string{"/usr/bin", "/bin"}, string(os.PathListSeparator))
	t.Setenv("HOME", home)
	t.Setenv("PATH", currentPath)

	env := buildDaemonHeadlessBaseEnv(os.Environ(), nil)

	processPath := os.Getenv("PATH")
	basePath, ok := lookupEnvEntryForTest(env, "PATH")
	if !ok {
		t.Fatal("base env PATH missing")
	}
	if processPath != basePath {
		t.Fatalf("process PATH = %q, base env PATH = %q", processPath, basePath)
	}
	parts := strings.Split(processPath, string(os.PathListSeparator))
	if len(parts) < 3 || parts[0] != "/usr/bin" || parts[1] != "/bin" {
		t.Fatalf("process PATH entries = %#v, want current PATH entries first", parts)
	}
	if !containsString(parts, shellBin) {
		t.Fatalf("process PATH entries = %#v, want interactive shell bin %q", parts, shellBin)
	}
}

func TestApplyDaemonStartupArgsSetsInstallOwnedRuntimeEnv(t *testing.T) {
	t.Setenv(config.UnifiedConfigEnvPath, "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_STATE_HOME", "")

	args := []string{
		"-config", filepath.Join("C:", "Users", "demo", "codex remote", "config.json"),
		"-xdg-config-home", filepath.Join("C:", "Users", "demo", ".config"),
		"-xdg-data-home", filepath.Join("C:", "Users", "demo", ".local", "share"),
		"-xdg-state-home", filepath.Join("C:", "Users", "demo", ".local", "state"),
	}
	if err := applyDaemonStartupArgs(args); err != nil {
		t.Fatalf("applyDaemonStartupArgs: %v", err)
	}
	if got := strings.TrimSpace(config.DefaultConfigPath()); got != args[1] {
		t.Fatalf("DefaultConfigPath = %q, want %q", got, args[1])
	}
	if got := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); got != args[3] {
		t.Fatalf("XDG_CONFIG_HOME = %q, want %q", got, args[3])
	}
	if got := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); got != args[5] {
		t.Fatalf("XDG_DATA_HOME = %q, want %q", got, args[5])
	}
	if got := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); got != args[7] {
		t.Fatalf("XDG_STATE_HOME = %q, want %q", got, args[7])
	}
}

func TestApplyDaemonStartupArgsRejectsUnknownFlag(t *testing.T) {
	err := applyDaemonStartupArgs([]string{"-not-a-daemon-flag"})
	if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("applyDaemonStartupArgs error = %v, want unknown flag", err)
	}
}

func TestResolveDesktopSessionInstanceIDUsesRuntimeEnv(t *testing.T) {
	t.Setenv("CODEX_REMOTE_INSTANCE_ID", "beta")
	if got := resolveDesktopSessionInstanceID(); got != "beta" {
		t.Fatalf("resolveDesktopSessionInstanceID() = %q, want beta", got)
	}
}

func normalizeExecutablePathForDaemonTest(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func lookupEnvEntryForTest(env []string, key string) (string, bool) {
	for _, item := range env {
		currentKey, value, ok := strings.Cut(item, "=")
		if ok && currentKey == key {
			return value, true
		}
	}
	return "", false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
