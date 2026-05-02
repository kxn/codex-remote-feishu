package daemon

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4322, nil
	}

	app.startManagedHeadless(control.DaemonCommand{
		Kind:                  control.DaemonCommandStartHeadless,
		SurfaceSessionID:      "surface-1",
		InstanceID:            "inst-claude-profile",
		ThreadID:              "thread-claude",
		ThreadCWD:             "/data/dl/repo",
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       "devseek",
		ClaudeReasoningEffort: "high",
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
	if !containsEnvEntry(captured.Env, config.ClaudeConfigDirEnv+"=/tmp/old-claude") {
		t.Fatalf("expected custom profile to preserve shared CLAUDE_CONFIG_DIR, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.ClaudeBaseURLEnv+"=https://proxy.internal/v1") ||
		!containsEnvEntry(captured.Env, config.ClaudeAuthTokenEnv+"=profile-token") ||
		!containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=mimo-v2.5-pro") ||
		!containsEnvEntry(captured.Env, config.ClaudeDefaultHaikuModelEnv+"=mimo-v2.5-haiku") ||
		!containsEnvEntry(captured.Env, config.ClaudeEffortLevelEnv+"=high") {
		t.Fatalf("expected custom profile env overrides, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, config.ClaudeAuthTokenEnv+"=old-token") ||
		containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=old-model") {
		t.Fatalf("expected stale claude env to be replaced, got %#v", captured.Env)
	}
}

func TestDaemonStartsClaudeHeadlessWithBuiltInDefaultProfileKeepsCurrentClaudeEnv(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
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
		Backend:          agentproto.BackendClaude,
		ClaudeProfileID:  config.ClaudeDefaultProfileID,
	})

	if !containsEnvEntry(captured.Env, config.ClaudeConfigDirEnv+"=/tmp/current-claude") ||
		!containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=current-model") {
		t.Fatalf("expected built-in default profile to preserve current claude env, got %#v", captured.Env)
	}
	if captured.LaunchMode != relayruntime.HeadlessLaunchModeClaudeAppServer {
		t.Fatalf("expected built-in default Claude launch mode, got %#v", captured)
	}
}

func TestDaemonRejectsHeadlessStartWithoutFrozenBackendContract(t *testing.T) {
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
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "devseek", "", "")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-missing-backend",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendClaude,
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	startEvents := app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-claude",
	})
	if len(startEvents) != 2 || startEvents[1].DaemonCommand == nil || startEvents[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected use-thread flow to prepare pending headless launch, got %#v", startEvents)
	}
	command := *startEvents[1].DaemonCommand
	command.Backend = ""

	var startCalled bool
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		startCalled = true
		return 0, nil
	}

	events := app.startManagedHeadless(command)

	if startCalled {
		t.Fatal("expected daemon to reject missing frozen backend contract before launch")
	}
	if len(events) == 0 || events[0].Notice == nil || events[0].Notice.Code != "headless_start_failed" {
		t.Fatalf("expected headless_start_failed notice, got %#v", events)
	}
	if snapshot := app.service.SurfaceSnapshot("surface-1"); snapshot == nil || snapshot.PendingHeadless.InstanceID != "" {
		t.Fatalf("expected failed launch to clear pending headless snapshot, got %#v", snapshot)
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
	_, err := app.applyClaudeHeadlessProfileEnv([]string{"PATH=/usr/bin"}, agentproto.BackendClaude, "missing-profile")
	if err == nil || !strings.Contains(err.Error(), "missing-profile") {
		t.Fatalf("expected missing profile error, got %v", err)
	}
}
