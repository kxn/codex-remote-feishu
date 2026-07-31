package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/profilecontextstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestProfileCatalogStartupMigrationIsIdempotentAndCommitsLast(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID:              "team-proxy",
		Name:            "Team Proxy",
		BaseURL:         "https://proxy.example/v1",
		APIKey:          " secret ",
		Model:           "gpt-5.4",
		ReasoningEffort: "high",
	}}
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{
		ID:    "devseek",
		Name:  "DevSeek",
		Model: "claude-sonnet-4-5[1m]",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	botStore := botcapabilitysettings.NewStore(botcapabilitysettings.StatePath(stateDir))
	if err := botStore.Put(state.BotCapabilitySettingsRecord{
		GatewayID:       "main",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "team-proxy",
	}); err != nil {
		t.Fatalf("write legacy bot settings: %v", err)
	}
	surfaceStore := surfaceresume.NewStore(surfaceresume.StatePath(stateDir))
	if err := surfaceStore.Put(surfaceresume.Entry{
		SurfaceSessionID: "surface-1",
		ProductMode:      string(state.ProductModeNormal),
		Backend:          string(agentproto.BackendCodex),
		CodexProviderID:  "team-proxy",
	}); err != nil {
		t.Fatalf("write legacy surface resume: %v", err)
	}

	startProfileMigrationTestApp(t, configPath, stateDir)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Codex.ProfileCatalogMigrationVersion != 1 {
		t.Fatalf("migration marker = %d", loaded.Config.Codex.ProfileCatalogMigrationVersion)
	}
	if len(loaded.Config.Codex.Providers) != 0 || len(loaded.Config.Codex.Profiles) != 1 {
		t.Fatalf("legacy provider was not cut over: %#v", loaded.Config.Codex)
	}
	profile, ok := config.CurrentCodexAPIProfile(loaded.Config.Codex.Profiles[0])
	if !ok || profile.ID != "team-proxy" || profile.Revision != 1 || profile.APIKey != " secret " {
		t.Fatalf("unexpected migrated profile: %#v ok=%v", profile, ok)
	}
	if loaded.Config.Claude.Profiles[0].Model != "claude-sonnet-4-5" {
		t.Fatalf("Claude model suffix was not split: %#v", loaded.Config.Claude.Profiles[0])
	}

	preferences, err := profilecontextstate.LoadStore(profilecontextstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load migrated preferences: %v", err)
	}
	if preference, ok := preferences.CodexCurrent("team-proxy"); !ok || preference.Revision != 1 || preference.Mode != state.CodexContextModeDefault {
		t.Fatalf("migrated codex preference = %#v ok=%v", preference, ok)
	}
	if preference, ok := preferences.ClaudeCurrent("devseek"); !ok || preference.Revision != 1 || preference.Mode != state.ClaudeContextModeExtended {
		t.Fatalf("migrated claude preference = %#v ok=%v", preference, ok)
	}

	migratedBot, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load migrated bot settings: %v", err)
	}
	if record, ok := migratedBot.Get(state.BotCapabilitySettingsKey("main")); !ok || record.CodexProfileID != "team-proxy" {
		t.Fatalf("migrated bot profile selection = %#v ok=%v", record, ok)
	}
	migratedSurface, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load migrated surface resume: %v", err)
	}
	if entry, ok := migratedSurface.Get("surface-1"); !ok || entry.CodexProfileID != "team-proxy" {
		t.Fatalf("migrated surface profile selection = %#v ok=%v", entry, ok)
	}

	before, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile before replay: %v", err)
	}
	startProfileMigrationTestApp(t, configPath, stateDir)
	after, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile after replay: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("idempotent migration rewrote config\nbefore=%s\nafter=%s", before, after)
	}
}

func TestProfileCatalogMigrationPreservesClaudeExtendedContextAtLaunch(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{
		ID:    "devseek",
		Name:  "DevSeek",
		Model: "claude-sonnet-4-5[1m]",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := startProfileMigrationTestApp(t, configPath, stateDir)
	env, settings, err := app.applyClaudeHeadlessProfileEnv(nil, agentproto.BackendClaude, "devseek")
	if err != nil {
		t.Fatalf("applyClaudeHeadlessProfileEnv: %v", err)
	}
	if model, ok := lookupEnvEntry(env, config.ClaudeModelEnv); !ok || model != "claude-sonnet-4-5[1m]" {
		t.Fatalf("migrated Claude launch model = %q, %v; want preserved [1m] suffix", model, ok)
	}
	if settings.Env[config.ClaudeModelEnv] != "claude-sonnet-4-5[1m]" {
		t.Fatalf("migrated Claude runtime settings = %#v", settings)
	}
}

func TestProfileCatalogMigrationPreservesSurfaceAdmissionRefAfterProjection(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID: "team-proxy", Name: "Team Proxy", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.4", ReasoningEffort: "high",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	surfaceStore := surfaceresume.NewStore(surfaceresume.StatePath(stateDir))
	if err := surfaceStore.Put(surfaceresume.Entry{
		SurfaceSessionID:   "surface-1",
		ProductMode:        string(state.ProductModeNormal),
		Backend:            string(agentproto.BackendCodex),
		CodexProviderID:    "team-proxy",
		ResumeThreadID:     "thread-1",
		ResumeThreadCWD:    "/data/dl/repo",
		ResumeWorkspaceKey: "/data/dl/repo",
		ResumeHeadless:     true,
	}); err != nil {
		t.Fatalf("write legacy surface resume: %v", err)
	}

	startProfileMigrationTestApp(t, configPath, stateDir)
	migrated, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load migrated surface resume: %v", err)
	}
	entry, ok := migrated.Get("surface-1")
	if !ok || entry.CodexAdmissionRef == nil {
		t.Fatalf("migrated admission ref was cleared by projection: %#v ok=%v", entry, ok)
	}
	if entry.CodexAdmissionRef.ProfileRef.ID != "team-proxy" || entry.CodexAdmissionRef.ProfileRef.Revision != 1 ||
		entry.CodexAdmissionRef.ContextPreferenceRef.ProfileID != "team-proxy" || entry.CodexAdmissionRef.ContextPreferenceRef.Revision != 1 {
		t.Fatalf("unexpected migrated admission ref: %#v", entry.CodexAdmissionRef)
	}
}

func TestProfileCatalogMigrationRedactsUnsafeLegacyEndpointFromPublicSummary(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID:              "unsafe-proxy",
		Name:            "Unsafe Proxy",
		BaseURL:         "https://visible-user:visible-pass@proxy.example/v1?token=url-secret",
		APIKey:          "stored-api-secret",
		Model:           "gpt-5.4",
		ReasoningEffort: "high",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := startProfileMigrationTestApp(t, configPath, stateDir)

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/codex/profiles", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("profile list status = %d body=%s", rec.Code, rec.Body.String())
	}
	for _, secret := range []string{"visible-user", "visible-pass", "url-secret", "stored-api-secret"} {
		if strings.Contains(rec.Body.String(), secret) {
			t.Fatalf("public profile summary leaked %q: %s", secret, rec.Body.String())
		}
	}
	var response codexProfilesResponse
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode profile list: %v", err)
	}
	profile, ok := findCodexProfileSummary(response.Profiles, "unsafe-proxy")
	if !ok || profile.BaseURL != "" || profile.StatusCode != "profile_definition_incomplete" || profile.Available {
		t.Fatalf("unsafe migrated profile summary = %#v ok=%v", profile, ok)
	}
}

func TestProfileCatalogMigrationRecordsCanonicalSurfaceSelectionConflict(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{
		{ID: "proxy-a", Name: "Proxy A", BaseURL: "https://a.example/v1", APIKey: "a", Model: "gpt-5.4", ReasoningEffort: "high"},
		{ID: "proxy-b", Name: "Proxy B", BaseURL: "https://b.example/v1", APIKey: "b", Model: "gpt-5.4", ReasoningEffort: "high"},
	}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	surfaceStore := surfaceresume.NewStore(surfaceresume.StatePath(stateDir))
	if err := surfaceStore.ReplaceAll(map[string]surfaceresume.Entry{
		"feishu:main:user:ou_old": {
			SurfaceSessionID: "feishu:main:user:ou_old", GatewayID: "main", ChatID: "oc_chat", ActorUserID: "ou_old",
			ProductMode: string(state.ProductModeNormal), Backend: string(agentproto.BackendCodex), CodexProviderID: "proxy-a",
			UpdatedAt: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		},
		"feishu:main:user:ou_new": {
			SurfaceSessionID: "feishu:main:user:ou_new", GatewayID: "main", ChatID: "oc_chat", ActorUserID: "ou_new",
			ProductMode: string(state.ProductModeNormal), Backend: string(agentproto.BackendCodex), CodexProviderID: "proxy-b",
			UpdatedAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
		},
	}); err != nil {
		t.Fatalf("write conflicting legacy surfaces: %v", err)
	}

	app := startProfileMigrationTestApp(t, configPath, stateDir)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	found := false
	for _, diagnostic := range loaded.Config.Codex.MigrationDiagnostics {
		if diagnostic.Code == "profile_selection_conflict" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration diagnostics did not retain canonical route conflict: %#v", loaded.Config.Codex.MigrationDiagnostics)
	}
	migratedSurfaces, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load migrated surfaces: %v", err)
	}
	var conflicted surfaceresume.Entry
	for _, entry := range migratedSurfaces.Entries() {
		conflicted = entry
	}
	if conflicted.CodexProfileSelectionStatus != surfaceresume.CodexProfileSelectionStatusConflict {
		t.Fatalf("merged route did not retain conflict status: %#v", conflicted)
	}

	launched := false
	app.headlessRuntime.BinaryPath = "/bin/false"
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launched = true
		return 1, nil
	}
	app.handleDaemonCommand(control.DaemonCommand{
		Kind: control.DaemonCommandStartHeadless, SurfaceSessionID: conflicted.SurfaceSessionID, InstanceID: "instance-conflict",
		Backend: agentproto.BackendCodex, CodexProviderID: conflicted.CodexProviderID,
	})
	if launched {
		t.Fatal("profile selection conflict did not fail closed before Codex launch")
	}

	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: conflicted.SurfaceSessionID,
		GatewayID:        conflicted.GatewayID,
		ChatID:           conflicted.ChatID,
		ActorUserID:      conflicted.ActorUserID,
		Text:             "/codexprovider " + conflicted.CodexProviderID,
	})
	app.mu.Lock()
	app.syncSurfaceResumeStateLocked(nil)
	app.mu.Unlock()
	resolved, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load resolved surfaces: %v", err)
	}
	resolvedEntry, ok := resolved.Get(conflicted.SurfaceSessionID)
	if !ok || resolvedEntry.CodexProfileSelectionStatus != "" {
		t.Fatalf("explicit canonical selection did not clear conflict: %#v ok=%v", resolvedEntry, ok)
	}
}

func TestProfileCatalogMigrationRunsWhenStoresBecomeReadyAfterAdmin(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID: "team-proxy", Name: "Team Proxy", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.4", ReasoningEffort: "high",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Codex.ProfileCatalogMigrationVersion != profileCatalogMigrationVersion || len(loaded.Config.Codex.Providers) != 0 {
		t.Fatalf("reversed initialization did not run migration: %#v", loaded.Config.Codex)
	}
	app.mu.Lock()
	migrationErr := app.profileCatalogMigrationErr
	app.mu.Unlock()
	if migrationErr != nil {
		t.Fatalf("reversed initialization remained degraded: %v", migrationErr)
	}
}

func TestSetHeadlessRuntimeDoesNotReadCatalogBeforeAdminConfigured(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.service.MaterializeCodexProviders([]state.CodexProviderRecord{{ID: "sentinel-profile", Name: "Sentinel"}})

	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: t.TempDir()}})

	found := false
	for _, provider := range app.service.CodexProviders() {
		if provider.ID == "sentinel-profile" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("SetHeadlessRuntime read and projected a default app config before admin was configured")
	}
}

func TestConfigureAdminProjectsCommittedCatalogAfterHeadlessRuntime(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	record, err := config.PrepareCodexAPIProfileCreate(nil, config.CodexAPIProfileInput{
		Name: "Committed", BaseURL: "https://committed.example/v1", APIKey: "secret",
		Model: "gpt-5.4", ReasoningEffort: "high",
	})
	if err != nil {
		t.Fatalf("PrepareCodexAPIProfileCreate: %v", err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Codex.Profiles = []config.CodexAPIProfileRecord{record}
	cfg.Codex.ProfileCatalogMigrationVersion = profileCatalogMigrationVersion
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	preferenceStore := profilecontextstate.NewStore(profilecontextstate.StatePath(stateDir))
	for _, seed := range []struct {
		profileID string
		mode      string
		codex     bool
	}{
		{profileID: config.CodexNativeProfileID, mode: state.CodexContextModeDefault, codex: true},
		{profileID: record.ID, mode: state.CodexContextModeDefault, codex: true},
		{profileID: config.ClaudeDefaultProfileID, mode: state.ClaudeContextModeDefault},
	} {
		if seed.codex {
			err = preferenceStore.EnsureCodexProfile(seed.profileID, seed.mode)
		} else {
			err = preferenceStore.EnsureClaudeProfile(seed.profileID, seed.mode)
		}
		if err != nil {
			t.Fatalf("seed profile preference: %v", err)
		}
	}

	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})

	found := false
	for _, provider := range app.service.CodexProviders() {
		if provider.ID == record.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("committed catalog was not projected after admin configuration: %#v", app.service.CodexProviders())
	}
}

func TestProfileCatalogMigrationFailsClosedWhenPreferenceStateIsCorrupt(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID: "team-proxy", Name: "Team Proxy", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.4", ReasoningEffort: "high",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(profilecontextstate.StatePath(stateDir), []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupt preference state: %v", err)
	}

	app := startProfileMigrationTestApp(t, configPath, stateDir)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if loaded.Config.Codex.ProfileCatalogMigrationVersion != 0 || len(loaded.Config.Codex.Providers) != 1 || len(loaded.Config.Codex.Profiles) != 0 {
		t.Fatalf("failed migration committed partial config: %#v", loaded.Config.Codex)
	}
	list := performAdminRequest(t, app, "GET", "/api/admin/codex/profiles", "")
	if list.Code != 503 {
		t.Fatalf("degraded profile list status = %d body=%s", list.Code, list.Body.String())
	}
	app.mu.Lock()
	migrationErr := app.profileCatalogMigrationErr
	app.mu.Unlock()
	if migrationErr == nil {
		t.Fatal("corrupt preference state did not establish the profile migration gate")
	}
	app.headlessRuntime.BinaryPath = "/bin/false"
	launched := false
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launched = true
		return 1, nil
	}
	app.handleDaemonCommand(control.DaemonCommand{
		Kind: control.DaemonCommandStartHeadless, InstanceID: "corrupt-preference", Backend: agentproto.BackendCodex,
		CodexProviderID: "team-proxy",
	})
	if launched {
		t.Fatal("corrupt preference state did not block managed Codex launch")
	}
}

func TestProfileCatalogMigrationFailsClosedWhenSelectionStateIsCorrupt(t *testing.T) {
	tests := []struct {
		name string
		path func(string) string
	}{
		{name: "bot capability settings", path: botcapabilitysettings.StatePath},
		{name: "surface resume", path: surfaceresume.StatePath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			configPath := filepath.Join(root, "config.json")
			stateDir := filepath.Join(root, "state")
			cfg := config.DefaultAppConfig()
			cfg.Codex.Providers = []config.CodexProviderConfig{{
				ID: "team-proxy", Name: "Team Proxy", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.4", ReasoningEffort: "high",
			}}
			if err := config.WriteAppConfig(configPath, cfg); err != nil {
				t.Fatalf("WriteAppConfig: %v", err)
			}
			if err := os.MkdirAll(stateDir, 0o755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(test.path(stateDir), []byte("{"), 0o600); err != nil {
				t.Fatalf("write corrupt state: %v", err)
			}

			app := startProfileMigrationTestApp(t, configPath, stateDir)
			app.mu.Lock()
			migrationErr := app.profileCatalogMigrationErr
			app.mu.Unlock()
			if migrationErr == nil {
				t.Fatal("corrupt selection state did not establish the profile migration gate")
			}
			mutation := performAdminRequest(t, app, http.MethodPost, "/api/admin/codex/profiles", `{"name":"Blocked","baseURL":"https://blocked.example/v1","apiKey":"secret","model":"gpt-5.4","reasoningEffort":"high"}`)
			if mutation.Code != http.StatusServiceUnavailable {
				t.Fatalf("profile mutation status = %d body=%s", mutation.Code, mutation.Body.String())
			}
			app.headlessRuntime.BinaryPath = "/bin/false"
			launched := false
			app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
				launched = true
				return 1, nil
			}
			app.handleDaemonCommand(control.DaemonCommand{
				Kind: control.DaemonCommandStartHeadless, InstanceID: "corrupt-selection", Backend: agentproto.BackendCodex,
				CodexProviderID: "team-proxy",
			})
			if launched {
				t.Fatal("corrupt selection state did not block managed Codex launch")
			}
		})
	}
}

func TestProfileCatalogMigrationPreservesDanglingSelectionDiagnostic(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Codex.Providers = []config.CodexProviderConfig{{
		ID: "team-proxy", Name: "Team Proxy", BaseURL: "https://proxy.example/v1", APIKey: "secret", Model: "gpt-5.4", ReasoningEffort: "high",
	}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	botStore := botcapabilitysettings.NewStore(botcapabilitysettings.StatePath(stateDir))
	if err := botStore.Put(state.BotCapabilitySettingsRecord{
		GatewayID:       "main",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "removed-provider",
	}); err != nil {
		t.Fatalf("write dangling bot settings: %v", err)
	}

	startProfileMigrationTestApp(t, configPath, stateDir)
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Codex.MigrationDiagnostics) != 1 {
		t.Fatalf("migration diagnostics = %#v, want one dangling selection", loaded.Config.Codex.MigrationDiagnostics)
	}
	diagnostic := loaded.Config.Codex.MigrationDiagnostics[0]
	if diagnostic.ProfileID != "removed-provider" || diagnostic.Code != "profile_not_found" {
		t.Fatalf("migration diagnostic = %#v, want preserved profile_not_found", diagnostic)
	}
	migratedBot, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load migrated bot settings: %v", err)
	}
	record, ok := migratedBot.Get(state.BotCapabilitySettingsKey("main"))
	if !ok || record.CodexProfileID != "removed-provider" {
		t.Fatalf("dangling selection was not preserved: %#v ok=%v", record, ok)
	}
}

func TestProfileCatalogMigrationFailureBlocksProfileMutations(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := startProfileMigrationTestApp(t, configPath, stateDir)
	app.mu.Lock()
	app.profileCatalogMigrationErr = errors.New("migration commit incomplete")
	app.mu.Unlock()

	for _, request := range []struct {
		path string
		body string
	}{
		{path: "/api/admin/codex/profiles", body: `{"name":"Blocked","baseURL":"https://proxy.example/v1","apiKey":"secret","model":"gpt-5.4","reasoningEffort":"high"}`},
		{path: "/api/admin/claude/profiles", body: `{"name":"Blocked","model":"claude-sonnet-4-5"}`},
	} {
		rec := performAdminRequest(t, app, http.MethodPost, request.path, request.body)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("mutation %s status = %d body=%s", request.path, rec.Code, rec.Body.String())
		}
	}
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Codex.Profiles) != 0 || len(loaded.Config.Claude.Profiles) != 0 {
		t.Fatalf("degraded catalog accepted mutation: %#v", loaded.Config)
	}
}

func TestProfileCatalogMigrationFailureBlocksManagedCodexLaunch(t *testing.T) {
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{BinaryPath: "/bin/false"})
	app.mu.Lock()
	app.profileCatalogMigrationErr = errors.New("migration commit incomplete")
	app.mu.Unlock()
	launched := false
	app.startHeadless = func(relayruntime.HeadlessLaunchOptions) (int, error) {
		launched = true
		return 1, nil
	}

	app.handleDaemonCommand(control.DaemonCommand{
		Kind:       control.DaemonCommandStartHeadless,
		InstanceID: "instance-1",
		Backend:    agentproto.BackendCodex,
	})
	if launched {
		t.Fatal("managed Codex launch continued while profile migration was degraded")
	}
}

func TestProfileCreateRollsBackPreferenceWhenConfigWriteFails(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := startProfileMigrationTestApp(t, configPath, stateDir)
	preferencePath := profilecontextstate.StatePath(stateDir)
	before, err := os.ReadFile(preferencePath)
	if err != nil {
		t.Fatalf("ReadFile before mutation: %v", err)
	}
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	blockedPath := filepath.Join(root, "blocked-config")
	if err := os.MkdirAll(blockedPath, 0o755); err != nil {
		t.Fatalf("MkdirAll blocked path: %v", err)
	}
	loaded.Path = blockedPath
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) { return loaded, nil }

	for _, request := range []struct {
		path string
		body string
	}{
		{path: "/api/admin/codex/profiles", body: `{"name":"Blocked Codex","baseURL":"https://proxy.example/v1","apiKey":"secret","model":"gpt-5.4","reasoningEffort":"high"}`},
		{path: "/api/admin/claude/profiles", body: `{"name":"Blocked Claude","model":"claude-sonnet-4-5"}`},
	} {
		rec := performAdminRequest(t, app, http.MethodPost, request.path, request.body)
		if rec.Code < 400 {
			t.Fatalf("mutation %s unexpectedly succeeded: status=%d body=%s", request.path, rec.Code, rec.Body.String())
		}
		after, readErr := os.ReadFile(preferencePath)
		if readErr != nil {
			t.Fatalf("ReadFile after mutation: %v", readErr)
		}
		if string(after) != string(before) {
			t.Fatalf("mutation %s left orphan preference\nbefore=%s\nafter=%s", request.path, before, after)
		}
	}
}

func TestClaudeProfileDeleteRollsBackDefinitionWhenPreferenceDeleteFails(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.json")
	stateDir := filepath.Join(root, "state")
	cfg := config.DefaultAppConfig()
	cfg.Claude.Profiles = []config.ClaudeProfileConfig{{ID: "devseek", Name: "DevSeek", Model: "claude-sonnet-4-5"}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	app := startProfileMigrationTestApp(t, configPath, stateDir)
	preferencePath := profilecontextstate.StatePath(stateDir)
	if err := os.Remove(preferencePath); err != nil {
		t.Fatalf("Remove preference file: %v", err)
	}
	if err := os.Mkdir(preferencePath, 0o755); err != nil {
		t.Fatalf("Mkdir preference path: %v", err)
	}

	rec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/claude/profiles/devseek", "")
	if rec.Code == http.StatusNoContent {
		t.Fatal("delete unexpectedly succeeded while preference deletion failed")
	}
	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if config.IndexOfClaudeProfile(loaded.Config.Claude.Profiles, "devseek") < 0 {
		t.Fatalf("failed preference deletion left definition deleted: %#v", loaded.Config.Claude.Profiles)
	}
}

func startProfileMigrationTestApp(t *testing.T, configPath, stateDir string) *App {
	t.Helper()
	app := New(":0", ":0", nil, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	app.ConfigureAdmin(AdminRuntimeOptions{ConfigPath: configPath})
	return app
}
