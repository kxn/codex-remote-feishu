package daemon

import (
	"encoding/json"
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
		ID:              "devseek",
		Name:            "DevSeek",
		AuthMode:        config.ClaudeAuthModeAuthToken,
		BaseURL:         "https://proxy.internal/v1",
		AuthToken:       "profile-token",
		Model:           "mimo-v2.5-pro",
		SmallModel:      "mimo-v2.5-haiku",
		ReasoningEffort: "medium",
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
			config.ClaudeEffortLevelEnv + "=old-effort",
			config.ClaudeDisableThinkingEnv + "=1",
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
	if !containsEnvEntry(captured.Env, config.ClaudeDisableAdaptiveEnv+"=1") {
		t.Fatalf("expected high reasoning override to disable adaptive thinking, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, config.ClaudeDisableThinkingEnv+"=1") {
		t.Fatalf("expected explicit reasoning to re-enable thinking, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, config.ClaudeAuthTokenEnv+"=old-token") ||
		containsEnvEntry(captured.Env, config.ClaudeModelEnv+"=old-model") {
		t.Fatalf("expected stale claude env to be replaced, got %#v", captured.Env)
	}
	settings := mustReadClaudeRuntimeSettingsEnv(t, captured.Env)
	if got := settings.Env[config.ClaudeBaseURLEnv]; got != "https://proxy.internal/v1" {
		t.Fatalf("expected runtime settings base url override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeAuthTokenEnv]; got != "profile-token" {
		t.Fatalf("expected runtime settings auth token override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeModelEnv]; got != "mimo-v2.5-pro" {
		t.Fatalf("expected runtime settings model override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeDefaultHaikuModelEnv]; got != "mimo-v2.5-haiku" {
		t.Fatalf("expected runtime settings small model override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeEffortLevelEnv]; got != "high" {
		t.Fatalf("expected runtime settings reasoning override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeDisableAdaptiveEnv]; got != "1" {
		t.Fatalf("expected runtime settings to disable adaptive thinking, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeDisableThinkingEnv]; got != "" {
		t.Fatalf("expected runtime settings to clear disable-thinking flag, got %#v", settings)
	}
}

func TestDaemonClaudeReasoningOverrideClearsProfileBudgetThinkingFlags(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{
		ID:              "devseek",
		Name:            "DevSeek",
		Model:           "mimo-v2.5-pro",
		ReasoningEffort: "high",
	}}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv: []string{
			"PATH=/usr/bin",
			config.ClaudeConfigDirEnv + "=/tmp/old-claude",
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
		InstanceID:            "inst-claude-profile-low",
		ThreadID:              "thread-claude",
		ThreadCWD:             "/data/dl/repo",
		Backend:               agentproto.BackendClaude,
		ClaudeProfileID:       "devseek",
		ClaudeReasoningEffort: "low",
	})

	if !containsEnvEntry(captured.Env, config.ClaudeEffortLevelEnv+"=low") {
		t.Fatalf("expected explicit low reasoning override, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, config.ClaudeDisableAdaptiveEnv+"=1") {
		t.Fatalf("expected low reasoning override to keep adaptive thinking enabled, got %#v", captured.Env)
	}
	if containsEnvEntry(captured.Env, config.ClaudeDisableThinkingEnv+"=1") {
		t.Fatalf("expected low reasoning override to remove thinking disable flag, got %#v", captured.Env)
	}
	settings := mustReadClaudeRuntimeSettingsEnv(t, captured.Env)
	if got := settings.Env[config.ClaudeModelEnv]; got != "mimo-v2.5-pro" {
		t.Fatalf("expected runtime settings to keep profile model override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeEffortLevelEnv]; got != "low" {
		t.Fatalf("expected runtime settings low reasoning override, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeDisableAdaptiveEnv]; got != "" {
		t.Fatalf("expected runtime settings to preserve adaptive thinking for low effort, got %#v", settings)
	}
	if got := settings.Env[config.ClaudeDisableThinkingEnv]; got != "" {
		t.Fatalf("expected runtime settings to clear disable-thinking flag, got %#v", settings)
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
	if _, ok := lookupEnvEntry(captured.Env, config.ClaudeRuntimeSettingsJSONEnv); ok {
		t.Fatalf("expected built-in default profile launch to avoid runtime settings overlay, got %#v", captured.Env)
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
	_, _, err := app.applyClaudeHeadlessProfileEnv([]string{"PATH=/usr/bin"}, agentproto.BackendClaude, "missing-profile")
	if err == nil || !strings.Contains(err.Error(), "missing-profile") {
		t.Fatalf("expected missing profile error, got %v", err)
	}
}

func mustReadClaudeRuntimeSettingsEnv(t *testing.T, env []string) config.ClaudeRuntimeSettings {
	t.Helper()
	raw, ok := lookupEnvEntry(env, config.ClaudeRuntimeSettingsJSONEnv)
	if !ok || strings.TrimSpace(raw) == "" {
		t.Fatalf("expected %s in env, got %#v", config.ClaudeRuntimeSettingsJSONEnv, env)
	}
	var settings config.ClaudeRuntimeSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatalf("decode %s: %v", config.ClaudeRuntimeSettingsJSONEnv, err)
	}
	return settings
}

func lookupEnvEntry(env []string, key string) (string, bool) {
	for _, item := range env {
		currentKey, value, ok := strings.Cut(item, "=")
		if ok && currentKey == key {
			return value, true
		}
	}
	return "", false
}
