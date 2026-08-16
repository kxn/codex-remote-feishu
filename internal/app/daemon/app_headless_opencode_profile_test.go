package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestDaemonStartsOpenCodeHeadlessWithProfileOverlayAndACPLaunchMode(t *testing.T) {
	record, err := config.PrepareOpenCodeAPIProfileCreate(nil, config.OpenCodeAPIProfileInput{
		Name:              "Team OpenCode",
		BaseURL:           "https://proxy.example/v1",
		APIKey:            "opencode-secret",
		Model:             "kimi-k2",
		SmallModel:        "kimi-small",
		ProjectConfigMode: config.OpenCodeProjectConfigDisable,
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileCreate: %v", err)
	}
	profile, ok := config.CurrentOpenCodeAPIProfile(record)
	if !ok {
		t.Fatal("CurrentOpenCodeAPIProfile() did not return created profile")
	}
	cfg := config.DefaultAppConfig()
	cfg.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{record}
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	stateDir := t.TempDir()
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv: []string{
			"PATH=/usr/bin",
			config.OpenCodeConfigContentEnv + "=old-config",
			config.OpenCodeAuthContentEnv + "=old-auth",
		},
		LaunchArgs: []string{"app-server"},
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
	app.service.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendOpenCode, "", "", "")
	surface := app.service.Surface("surface-1")
	surface.OpenCodeProfileID = profile.ID
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: profile.ID, Revision: profile.Revision}}

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4325, nil
	}
	staleWorkspace := evalSymlinkForTest(t, t.TempDir())
	threadCWD := evalSymlinkForTest(t, t.TempDir())
	command := control.DaemonCommand{
		Kind:                 control.DaemonCommandStartHeadless,
		SurfaceSessionID:     "surface-1",
		InstanceID:           "inst-opencode",
		ThreadCWD:            threadCWD,
		WorkspaceKey:         staleWorkspace,
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    profile.ID,
		OpenCodeAdmissionRef: surface.OpenCodeAdmissionRef,
	}
	authorizePendingHeadlessForTest(t, app, command)
	app.startManagedHeadless(command)

	if captured.LaunchMode != relayruntime.HeadlessLaunchModeOpenCodeACP {
		t.Fatalf("expected opencode launch mode, got %#v", captured)
	}
	if strings.Join(captured.Args, "\x00") != strings.Join([]string{"acp", "--cwd", threadCWD}, "\x00") {
		t.Fatalf("unexpected opencode child args: %#v", captured.Args)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_BACKEND=opencode") {
		t.Fatalf("expected opencode backend env, got %#v", captured.Env)
	}
	if !containsEnvEntry(captured.Env, config.OpenCodeRuntimeProfileIDEnv+"="+profile.ID) {
		t.Fatalf("expected opencode profile env, got %#v", captured.Env)
	}
	configRaw := envValueForTest(captured.Env, config.OpenCodeConfigContentEnv)
	authRaw := envValueForTest(captured.Env, config.OpenCodeAuthContentEnv)
	if configRaw == "" || authRaw == "" {
		t.Fatalf("expected opencode config/auth overlay env, got %#v", captured.Env)
	}
	if strings.Contains(configRaw, "opencode-secret") {
		t.Fatalf("config overlay leaked API key: %s", configRaw)
	}
	if !strings.Contains(authRaw, "opencode-secret") {
		t.Fatalf("auth overlay did not carry profile secret: %s", authRaw)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v", err)
	}
	if configDoc["model"] != "codex_remote_opencode_"+profile.ID+"/kimi-k2" {
		t.Fatalf("unexpected opencode model overlay: %#v", configDoc)
	}
	pending := app.service.Surface("surface-1").PendingHeadless
	if pending == nil || pending.OpenCodeAdmissionRef == nil || pending.OpenCodeAdmissionRef.ProfileRef.Revision != profile.Revision {
		t.Fatalf("expected pending headless to carry opencode admission ref, got %#v", pending)
	}
}

func TestDaemonStartsDefaultOpenCodeHeadlessWithRecentSystemModel(t *testing.T) {
	cfg := config.DefaultAppConfig()
	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	runtimeRoot := t.TempDir()
	configDir := filepath.Join(runtimeRoot, ".config", "codex-remote")
	stateDir := filepath.Join(runtimeRoot, ".local", "state", "codex-remote")
	configHome := filepath.Dir(configDir)
	stateHome := filepath.Dir(stateDir)
	if err := os.MkdirAll(filepath.Join(configHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll opencode config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(stateHome, "opencode"), 0o755); err != nil {
		t.Fatalf("MkdirAll opencode state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configHome, "opencode", "opencode.jsonc"), []byte(`{"provider":{"mimo":{"models":{"mimo-v2.5-pro":{"name":"mimo-v2.5-pro"}}}}}`), 0o644); err != nil {
		t.Fatalf("WriteFile opencode config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stateHome, "opencode", "model.json"), []byte(`{"recent":[{"providerID":"mimo","modelID":"mimo-v2.5-pro"}]}`), 0o644); err != nil {
		t.Fatalf("WriteFile opencode state: %v", err)
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: configPath,
		BaseEnv:    []string{"PATH=/usr/bin"},
		LaunchArgs: []string{"app-server"},
		Paths: relayruntime.Paths{
			ConfigDir: configDir,
			LogsDir:   t.TempDir(),
			StateDir:  stateDir,
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
	app.service.MaterializeSurfaceResume("surface-default", "", "chat-1", "user-1", "normal", agentproto.BackendOpenCode, "", "", "")
	surface := app.service.Surface("surface-default")
	surface.OpenCodeProfileID = state.DefaultOpenCodeProfileID

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4326, nil
	}
	workspaceDir := evalSymlinkForTest(t, t.TempDir())
	command := control.DaemonCommand{
		Kind:              control.DaemonCommandStartHeadless,
		SurfaceSessionID:  "surface-default",
		InstanceID:        "inst-opencode-default",
		ThreadCWD:         workspaceDir,
		WorkspaceKey:      workspaceDir,
		Backend:           agentproto.BackendOpenCode,
		OpenCodeProfileID: state.DefaultOpenCodeProfileID,
	}
	authorizePendingHeadlessForTest(t, app, command)
	app.startManagedHeadless(command)

	configRaw := envValueForTest(captured.Env, config.OpenCodeConfigContentEnv)
	if configRaw == "" {
		t.Fatalf("expected default opencode launch to project recent system model, got %#v", captured.Env)
	}
	var configDoc map[string]any
	if err := json.Unmarshal([]byte(configRaw), &configDoc); err != nil {
		t.Fatalf("config overlay is not JSON: %v", err)
	}
	if configDoc["model"] != "mimo/mimo-v2.5-pro" {
		t.Fatalf("unexpected default opencode model overlay: %#v", configDoc)
	}
	if got := envValueForTest(captured.Env, "XDG_CONFIG_HOME"); got != configHome {
		t.Fatalf("XDG_CONFIG_HOME = %q, want %q in %#v", got, configHome, captured.Env)
	}
	if got := envValueForTest(captured.Env, "XDG_STATE_HOME"); got != stateHome {
		t.Fatalf("XDG_STATE_HOME = %q, want %q in %#v", got, stateHome, captured.Env)
	}
}

func TestResolveOpenCodeLaunchProfileFallsBackToCurrentRevisionWithoutAdmissionRef(t *testing.T) {
	record, err := config.PrepareOpenCodeAPIProfileCreate(nil, config.OpenCodeAPIProfileInput{
		Name: "Team OpenCode", BaseURL: "https://proxy.example/v1", APIKey: "secret-v1", Model: "kimi-k2",
	})
	if err != nil {
		t.Fatalf("PrepareOpenCodeAPIProfileCreate: %v", err)
	}
	record, changed, err := config.PrepareOpenCodeAPIProfileUpdate(record, config.OpenCodeAPIProfileInput{
		Name: "Team OpenCode", BaseURL: "https://proxy.example/v1", APIKey: "secret-v2", Model: "kimi-k2",
	})
	if err != nil || !changed {
		t.Fatalf("PrepareOpenCodeAPIProfileUpdate: changed=%v err=%v", changed, err)
	}
	cfg := config.DefaultAppConfig()
	cfg.OpenCode.Profiles = []config.OpenCodeAPIProfileRecord{record}

	profile, err := resolveOpenCodeLaunchProfile(cfg, record.ID, nil)
	if err != nil {
		t.Fatalf("expected custom opencode profile to resolve to current revision without admission ref: %v", err)
	}
	if profile.Revision == 0 || profile.APIKey == "" {
		t.Fatalf("expected current revision profile without admission ref, got %#v", profile)
	}
	if _, err := resolveOpenCodeLaunchProfile(cfg, record.ID, &state.OpenCodeAdmissionRef{
		ProfileRef: state.OpenCodeProfileRef{ID: record.ID, Revision: 1},
	}); err != nil {
		t.Fatalf("expected exact historical revision to resolve with admission ref: %v", err)
	}
	if profile, err := resolveOpenCodeLaunchProfile(cfg, record.ID, &state.OpenCodeAdmissionRef{
		ProfileRef: state.OpenCodeProfileRef{ID: "op_some_other_profile", Revision: 1},
	}); err != nil {
		t.Fatalf("expected mismatched admission ref to fall back to current revision: %v", err)
	} else if profile.Revision == 0 {
		t.Fatalf("expected current revision profile after mismatched ref fallback, got %#v", profile)
	}
	if _, err := resolveOpenCodeLaunchProfile(cfg, state.DefaultOpenCodeProfileID, nil); err != nil {
		t.Fatalf("expected default opencode profile to resolve without admission ref: %v", err)
	}
}

func envValueForTest(env []string, key string) string {
	for _, entry := range env {
		currentKey, value, ok := strings.Cut(entry, "=")
		if ok && currentKey == key {
			return value
		}
	}
	return ""
}
