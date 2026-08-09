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
	workspaceDir := evalSymlinkForTest(t, t.TempDir())
	command := control.DaemonCommand{
		Kind:                 control.DaemonCommandStartHeadless,
		SurfaceSessionID:     "surface-1",
		InstanceID:           "inst-opencode",
		ThreadCWD:            workspaceDir,
		WorkspaceKey:         workspaceDir,
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    profile.ID,
		OpenCodeAdmissionRef: surface.OpenCodeAdmissionRef,
	}
	authorizePendingHeadlessForTest(t, app, command)
	app.startManagedHeadless(command)

	if captured.LaunchMode != relayruntime.HeadlessLaunchModeOpenCodeACP {
		t.Fatalf("expected opencode launch mode, got %#v", captured)
	}
	if strings.Join(captured.Args, "\x00") != strings.Join([]string{"acp", "--cwd", workspaceDir}, "\x00") {
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

func TestResolveOpenCodeLaunchProfileRequiresAdmissionRefForAPIProfile(t *testing.T) {
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

	if _, err := resolveOpenCodeLaunchProfile(cfg, record.ID, nil); err == nil {
		t.Fatal("expected custom opencode profile without admission ref to fail closed")
	}
	if _, err := resolveOpenCodeLaunchProfile(cfg, record.ID, &state.OpenCodeAdmissionRef{
		ProfileRef: state.OpenCodeProfileRef{ID: record.ID, Revision: 1},
	}); err != nil {
		t.Fatalf("expected exact historical revision to resolve with admission ref: %v", err)
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
