package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestDaemonStartsCodexHeadlessWithCustomProviderLaunchOverrides(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID:      "team-proxy",
		Name:    "Team Proxy",
		BaseURL: "https://proxy.example/v1",
		APIKey:  "provider-secret",
	}}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv:    []string{"PATH=/usr/bin"},
		LaunchArgs: []string{"app-server"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: t.TempDir(),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:      configPath,
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	app.service.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendCodex, "team-proxy", "", "", "")

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4324, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-codex-provider",
		ThreadCWD:        "/data/dl/repo",
		CodexProviderID:  "team-proxy",
	})

	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_BACKEND=codex") {
		t.Fatalf("expected codex backend env, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.CodexRuntimeProviderIDEnv+"=team-proxy") {
		t.Fatalf("expected runtime provider id env, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.CodexProviderAPIKeyEnv+"=provider-secret") {
		t.Fatalf("expected provider api key env, got %#v", captured.Env)
	}
	args := strings.Join(captured.Args, "\n")
	for _, want := range []string{
		`model_provider="team-proxy"`,
		`model_providers.team-proxy.name="Team Proxy"`,
		`model_providers.team-proxy.base_url="https://proxy.example/v1"`,
		`model_providers.team-proxy.env_key="CODEX_REMOTE_CODEX_PROVIDER_API_KEY"`,
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("expected launch args to contain %q, got %#v", want, captured.Args)
		}
	}
}

func TestDaemonStartsCodexHeadlessWithDefaultProviderKeepsLaunchArgsClean(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv:    []string{"PATH=/usr/bin"},
		LaunchArgs: []string{"app-server"},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: t.TempDir(),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:      configPath,
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4325, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-codex-default",
		ThreadCWD:        "/data/dl/repo",
		CodexProviderID:  config.CodexDefaultProviderID,
	})

	if strings.Contains(strings.Join(captured.Args, "\n"), "model_provider=") {
		t.Fatalf("expected built-in default provider to avoid provider overrides, got %#v", captured.Args)
	}
	if containsEnvEntry(captured.Env, config.CodexProviderAPIKeyEnv+"=") {
		t.Fatalf("expected built-in default provider to avoid provider key env, got %#v", captured.Env)
	}
}
