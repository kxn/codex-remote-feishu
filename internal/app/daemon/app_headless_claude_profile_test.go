package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestDaemonStartsClaudeHeadlessWithCustomProfileLaunchEnv(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{
		ID:         "devseek",
		Name:       "DevSeek",
		AuthMode:   config.ClaudeAuthModeAuthToken,
		BaseURL:    "https://proxy.internal/v1",
		AuthToken:  "profile-token",
		Model:      "mimo-v2.5-pro",
		SmallModel: "mimo-v2.5-haiku",
	}}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv: []string{
			"PATH=/usr/bin",
			config.ClaudeConfigDirEnv + "=/tmp/old-claude",
			config.ClaudeBaseURLEnv + "=https://old.internal",
			config.ClaudeAuthTokenEnv + "=old-token",
			config.ClaudeModelEnv + "=old-model",
			config.ClaudeDefaultHaikuModelEnv + "=old-small-model",
		},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: stateDir,
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
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4322, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-claude-profile",
		ThreadID:         "thread-claude",
		ThreadCWD:        "/data/dl/repo",
		ClaudeProfileID:  "devseek",
	})

	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_BACKEND=claude") {
		t.Fatalf("expected claude backend env for managed headless launch, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.ResumeThreadIDEnv+"=thread-claude") {
		t.Fatalf("expected managed headless launch to carry resume thread id, got %#v", captured.Env)
	}
	if captured.LaunchMode != relayruntime.HeadlessLaunchModeClaudeAppServer {
		t.Fatalf("expected claude managed headless launch mode, got %#v", captured)
	}
	if !containsEnvEntry(captured.Env, config.ClaudeConfigDirEnv+"="+filepath.Join(stateDir, "claude", "profiles", "devseek")) {
		t.Fatalf("expected profile-scoped CLAUDE_CONFIG_DIR, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.ClaudeBaseURLEnv+"=https://proxy.internal/v1") ||
		!containsEnvEntry(captured.Env, config.ClaudeAuthTokenEnv+"=profile-token") ||
		!containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=mimo-v2.5-pro") ||
		!containsEnvEntry(captured.Env, config.ClaudeDefaultHaikuModelEnv+"=mimo-v2.5-haiku") {
		t.Fatalf("expected custom profile env overrides, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, config.ClaudeAuthTokenEnv+"=old-token") ||
		containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=old-model") {
		t.Fatalf("expected stale claude env to be replaced, got %#v", captured.Env)
	}

	info, err := os.Stat(filepath.Join(stateDir, "claude", "profiles", "devseek"))
	if err != nil {
		t.Fatalf("expected profile runtime config dir to exist: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("expected profile runtime config dir to be a directory, got %#v", info)
	}
}

func TestDaemonStartsClaudeHeadlessWithBuiltInDefaultProfileKeepsCurrentClaudeEnv(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv: []string{
			"PATH=/usr/bin",
			config.ClaudeConfigDirEnv + "=/tmp/current-claude",
			config.ClaudeModelEnv + "=current-model",
		},
		Paths: relayruntime.Paths{
			LogsDir:  t.TempDir(),
			StateDir: stateDir,
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
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4323, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:             control.DaemonCommandStartHeadless,
		SurfaceSessionID: "surface-1",
		InstanceID:       "inst-claude-default",
		ThreadID:         "thread-claude",
		ThreadCWD:        "/data/dl/repo",
		ClaudeProfileID:  config.ClaudeDefaultProfileID,
	})

	if !containsEnvEntry(captured.Env, config.ClaudeConfigDirEnv+"=/tmp/current-claude") ||
		!containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=current-model") {
		t.Fatalf("expected built-in default profile to preserve current claude env, got %#v", captured.Env)
	}
	if captured.LaunchMode != relayruntime.HeadlessLaunchModeClaudeAppServer {
		t.Fatalf("expected built-in default Claude launch mode, got %#v", captured)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "claude", "profiles", "default")); !os.IsNotExist(err) {
		t.Fatalf("did not expect built-in default profile dir to be created, err=%v", err)
	}
}

func TestApplyClaudeHeadlessProfileEnvReturnsMissingProfileError(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv:    []string{"PATH=/usr/bin"},
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
	_, err := app.applyClaudeHeadlessProfileEnv([]string{"PATH=/usr/bin"}, agentproto.BackendClaude, "missing-profile", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "missing-profile") {
		t.Fatalf("expected missing profile error, got %v", err)
	}
}
