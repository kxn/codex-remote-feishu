package daemon

import (
	"context"
	"errors"
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
	if !ok || value != claudePath {
		t.Fatalf("CLAUDE_BIN = %q ok=%v, want %q", value, ok, claudePath)
	}
	if value, ok := lookupEnvEntryForTest(env, "https_proxy"); !ok || !strings.Contains(value, "proxy.internal") {
		t.Fatalf("https_proxy = %q ok=%v", value, ok)
	}
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
