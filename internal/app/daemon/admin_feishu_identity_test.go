package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishubotidentity"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	turnpatchruntime "github.com/kxn/codex-remote-feishu/internal/app/daemon/turnpatchruntime"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestFeishuAppIDChangePurgesGatewayIdentityStateAndPreservesRoomWorkspace(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_old","appSecret":"secret_old"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	privateSurfaceID := "feishu:main:user:ou_user"
	groupSurfaceID := "feishu:main:chat:oc_room"
	app.mu.Lock()
	app.service.MaterializeSurfaceResumeContract(
		privateSurfaceID,
		"main",
		"ou_user",
		"ou_user",
		state.HeadlessClaudeSurfaceBackendContract("claude-profile"),
		state.SurfaceVerbosityVerbose,
		state.PlanModeSettingOff,
	)
	app.service.MaterializeSurfaceResumeContract(
		groupSurfaceID,
		"main",
		"oc_room",
		"ou_user",
		state.HeadlessCodexSurfaceBackendContract("default"),
		state.SurfaceVerbosityNormal,
		state.PlanModeSettingOff,
	)
	app.service.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
		GatewayID:       "main",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "claude-profile",
	}})
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     "/data/workspaces/shared",
		PrimaryGatewayID: "main",
	}})
	app.syncSurfaceResumeStateLocked(nil)
	app.syncBotCapabilitySettingsStateLocked()
	app.syncFeishuRoomStateLocked()
	app.feishuRuntime.attentionRequests[privateSurfaceID+"::request-1::1"] = time.Now().UTC()
	app.turnPatchRuntime.ActiveFlows["req-main"] = &turnpatchruntime.FlowRecord{
		FlowID:           "flow-main",
		RequestID:        "req-main",
		SurfaceSessionID: privateSurfaceID,
		Stage:            turnpatchruntime.FlowStageEditing,
	}
	app.turnPatchRuntime.ActiveFlows["req-main-orphan"] = &turnpatchruntime.FlowRecord{
		FlowID:    "flow-main-orphan",
		RequestID: "req-main-orphan",
		Stage:     turnpatchruntime.FlowStageApplying,
	}
	app.turnPatchRuntime.ActiveFlows["req-other"] = &turnpatchruntime.FlowRecord{
		FlowID:           "flow-other",
		RequestID:        "req-other",
		SurfaceSessionID: "feishu:other:user:ou_other",
		Stage:            turnpatchruntime.FlowStageEditing,
	}
	app.turnPatchRuntime.ActiveTx["inst-main"] = &turnpatchruntime.Transaction{
		ID:               "tx-main",
		FlowID:           "flow-main",
		InstanceID:       "inst-main",
		InitiatorSurface: privateSurfaceID,
		PausedSurfaceIDs: map[string]bool{privateSurfaceID: true},
	}
	app.turnPatchRuntime.ActiveTx["inst-main-orphan"] = &turnpatchruntime.Transaction{
		ID:               "tx-main-orphan",
		FlowID:           "flow-main-orphan",
		InstanceID:       "inst-main-orphan",
		InitiatorSurface: privateSurfaceID,
		PausedSurfaceIDs: map[string]bool{},
	}
	app.turnPatchRuntime.ActiveTx["inst-other"] = &turnpatchruntime.Transaction{
		ID:               "tx-other",
		FlowID:           "flow-other",
		InstanceID:       "inst-other",
		InitiatorSurface: "feishu:other:user:ou_other",
		PausedSurfaceIDs: map[string]bool{},
	}
	app.mu.Unlock()

	app.feishuRuntime.permissionMu.Lock()
	app.feishuRuntime.permissionGaps["main"] = map[string]*feishuPermissionGapRecord{
		"im:message|tenant": {Scope: "im:message", ScopeType: "tenant"},
	}
	app.feishuRuntime.primaryPermissionCache["main"] = feishuPrimaryPermissionCacheRecord{GatewayID: "main", Allowed: true}
	app.feishuRuntime.permissionMu.Unlock()
	importCancelled := make(chan struct{})
	worktreeCancelled := make(chan struct{})
	app.mu.Lock()
	app.gitWorkspaceImports[privateSurfaceID+"::picker-old"] = &gitWorkspaceImportRuntime{cancel: func() { close(importCancelled) }}
	app.gitWorkspaceImports["feishu:other:user:ou_other::picker-other"] = &gitWorkspaceImportRuntime{}
	app.gitWorkspaceWorktrees[groupSurfaceID+"::picker-old"] = &gitWorkspaceWorktreeRuntime{cancel: func() { close(worktreeCancelled) }}
	app.gitWorkspaceWorktrees["feishu:other:chat:oc_other::picker-other"] = &gitWorkspaceWorktreeRuntime{}
	app.mu.Unlock()
	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appId":"cli_new"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update app id status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.upserted) != 2 || gateway.upserted[1].AppID != "cli_new" {
		t.Fatalf("runtime upserts = %#v, want old app then new app", gateway.upserted)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if app.service.Surface(privateSurfaceID) != nil || app.service.Surface(groupSurfaceID) != nil {
		t.Fatalf("old gateway surfaces survived identity change: %#v", app.service.Surfaces())
	}
	for _, entry := range app.surfaceResumeRuntime.store.Entries() {
		if strings.TrimSpace(entry.GatewayID) == "main" {
			t.Fatalf("old surface resume entry survived identity change: %#v", entry)
		}
	}
	if _, ok := app.botCapabilitySettingsState.store.Get(state.BotCapabilitySettingsKey("main")); ok {
		t.Fatal("old bot capability settings survived identity change")
	}
	room, ok := app.feishuRoomState.store.Get("oc_room")
	if !ok || room.WorkspaceKey != "/data/workspaces/shared" || room.PrimaryGatewayID != "" {
		t.Fatalf("room state after identity change = %#v, present=%v", room, ok)
	}
	if got := app.gatewayRuntimeHooks().PrimaryGatewayForChat("oc_room"); got != "" {
		t.Fatalf("primary snapshot after identity change = %q, want empty", got)
	}
	if _, ok := app.turnPatchRuntime.ActiveFlows["req-main"]; ok {
		t.Fatal("old turn patch flow survived identity change")
	}
	if _, ok := app.turnPatchRuntime.ActiveTx["inst-main"]; ok {
		t.Fatal("old turn patch transaction survived identity change")
	}
	if _, ok := app.turnPatchRuntime.ActiveFlows["req-main-orphan"]; ok {
		t.Fatal("old turn patch orphan flow survived identity change")
	}
	if _, ok := app.turnPatchRuntime.ActiveTx["inst-main-orphan"]; ok {
		t.Fatal("old turn patch orphan transaction survived identity change")
	}
	if _, ok := app.turnPatchRuntime.ActiveFlows["req-other"]; !ok {
		t.Fatal("other gateway turn patch flow was removed")
	}
	if _, ok := app.turnPatchRuntime.ActiveTx["inst-other"]; !ok {
		t.Fatal("other gateway turn patch transaction was removed")
	}
	for key := range app.feishuRuntime.attentionRequests {
		if strings.HasPrefix(key, privateSurfaceID+"::") {
			t.Fatalf("old attention cache survived identity change: %s", key)
		}
	}
	app.feishuRuntime.permissionMu.RLock()
	defer app.feishuRuntime.permissionMu.RUnlock()
	if _, ok := app.feishuRuntime.permissionGaps["main"]; ok {
		t.Fatal("old permission gaps survived identity change")
	}
	if _, ok := app.feishuRuntime.primaryPermissionCache["main"]; ok {
		t.Fatal("old primary permission cache survived identity change")
	}
	select {
	case <-importCancelled:
	default:
		t.Fatal("old surface Git import was not cancelled")
	}
	select {
	case <-worktreeCancelled:
	default:
		t.Fatal("old surface worktree creation was not cancelled")
	}
	if _, ok := app.gitWorkspaceImports[privateSurfaceID+"::picker-old"]; ok {
		t.Fatal("old surface Git import runtime survived identity change")
	}
	if _, ok := app.gitWorkspaceImports["feishu:other:user:ou_other::picker-other"]; !ok {
		t.Fatal("other gateway Git import runtime was removed")
	}
	if _, ok := app.gitWorkspaceWorktrees[groupSurfaceID+"::picker-old"]; ok {
		t.Fatal("old surface worktree runtime survived identity change")
	}
	if _, ok := app.gitWorkspaceWorktrees["feishu:other:chat:oc_other::picker-other"]; !ok {
		t.Fatal("other gateway worktree runtime was removed")
	}
}

func TestFeishuAppSecretChangePreservesGatewayIdentityState(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_old"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	seedFeishuGatewayIdentityState(t, app)

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appSecret":"secret_new"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update secret status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.removed) != 0 {
		t.Fatalf("secret-only update removed gateway runtime: %#v", gateway.removed)
	}
	if len(gateway.upserted) != 2 || gateway.upserted[1].AppSecret != "secret_new" {
		t.Fatalf("runtime upserts = %#v, want secret-only reconnect", gateway.upserted)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if app.service.Surface("feishu:main:user:ou_user") == nil {
		t.Fatal("secret-only update removed the existing surface")
	}
	if _, ok := app.botCapabilitySettingsState.store.Get(state.BotCapabilitySettingsKey("main")); !ok {
		t.Fatal("secret-only update removed bot capability settings")
	}
	room, ok := app.feishuRoomState.store.Get("oc_room")
	if !ok || room.PrimaryGatewayID != "main" || room.WorkspaceKey != "/data/workspaces/shared" {
		t.Fatalf("secret-only update changed room state: %#v, present=%v", room, ok)
	}
	identity, ok := app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_old" || identity.Generation != 1 {
		t.Fatalf("secret-only update changed committed identity: %#v, present=%v", identity, ok)
	}
}

func TestFeishuAppIDChangeFailureDoesNotStartNewRuntimeAndRetryReplaysCleanup(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_old"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	seedFeishuGatewayIdentityState(t, app)

	stateDir := filepath.Dir(app.surfaceResumeRuntime.path)
	if err := os.RemoveAll(stateDir); err != nil {
		t.Fatalf("remove state dir: %v", err)
	}
	if err := os.WriteFile(stateDir, []byte("blocks state writes"), 0o600); err != nil {
		t.Fatalf("block state dir: %v", err)
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appId":"cli_new"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed identity cleanup status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.removed) != 1 || gateway.removed[0] != "main" {
		t.Fatalf("old runtime removal = %#v, want main stopped before cleanup", gateway.removed)
	}
	if len(gateway.upserted) != 1 {
		t.Fatalf("new runtime started despite cleanup failure: %#v", gateway.upserted)
	}
	identity, ok := app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_old" {
		t.Fatalf("failed cleanup advanced committed identity: %#v, present=%v", identity, ok)
	}

	if err := os.Remove(stateDir); err != nil {
		t.Fatalf("remove state blocker: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("restore state dir: %v", err)
	}
	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/retry-apply", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("retry apply status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.upserted) != 2 || gateway.upserted[1].AppID != "cli_new" {
		t.Fatalf("retry runtime upserts = %#v, want new app", gateway.upserted)
	}
	identity, ok = app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_new" || identity.Generation != 2 {
		t.Fatalf("retry committed identity = %#v, present=%v", identity, ok)
	}
}

func TestFeishuAppIDChangeReplaysEveryDurableCleanupFailureBoundary(t *testing.T) {
	tests := []struct {
		name      string
		statePath func(string) string
	}{
		{name: "surface resume", statePath: surfaceresume.StatePath},
		{name: "bot capability settings", statePath: botcapabilitysettings.StatePath},
		{name: "Feishu room", statePath: feishuroomstate.StatePath},
		{name: "bot identity commit", statePath: feishubotidentity.StatePath},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &fakeAdminGatewayController{}
			app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), gateway, false, "")
			rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_old"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
			}
			seedFeishuGatewayIdentityState(t, app)

			stateDir := filepath.Dir(app.surfaceResumeRuntime.path)
			blockedPath := test.statePath(stateDir)
			if err := os.Remove(blockedPath); err != nil {
				t.Fatalf("remove state file before blocking: %v", err)
			}
			if err := os.Mkdir(blockedPath, 0o700); err != nil {
				t.Fatalf("block state file: %v", err)
			}

			rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appId":"cli_new"}`)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("failed transition status = %d, want 500 body=%s", rec.Code, rec.Body.String())
			}
			if len(gateway.upserted) != 1 {
				t.Fatalf("new runtime started across failed transition: %#v", gateway.upserted)
			}
			identity, ok := app.feishuBotIdentityState.store.Get("main")
			if !ok || identity.AppID != "cli_old" || identity.Generation != 1 {
				t.Fatalf("failed transition committed identity = %#v, present=%v", identity, ok)
			}

			if err := os.Remove(blockedPath); err != nil {
				t.Fatalf("remove state blocker: %v", err)
			}
			rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/retry-apply", "")
			if rec.Code != http.StatusNoContent {
				t.Fatalf("retry status = %d, want 204 body=%s", rec.Code, rec.Body.String())
			}
			if len(gateway.upserted) != 2 || gateway.upserted[1].AppID != "cli_new" {
				t.Fatalf("runtime after retry = %#v", gateway.upserted)
			}
			identity, ok = app.feishuBotIdentityState.store.Get("main")
			if !ok || identity.AppID != "cli_new" || identity.Generation != 2 {
				t.Fatalf("identity after retry = %#v, present=%v", identity, ok)
			}
			app.mu.Lock()
			defer app.mu.Unlock()
			if len(app.service.Surfaces()) != 0 || len(app.surfaceResumeRuntime.store.Entries()) != 0 {
				t.Fatalf("old surfaces after retry: service=%#v durable=%#v", app.service.Surfaces(), app.surfaceResumeRuntime.store.Entries())
			}
		})
	}
}

func TestFeishuAppIDChangeFailureThenRevertStillPurgesOldGeneration(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), gateway, false, "")
	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_old"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	seedFeishuGatewayIdentityState(t, app)

	stateDir := filepath.Dir(app.surfaceResumeRuntime.path)
	blockedPath := botcapabilitysettings.StatePath(stateDir)
	if err := os.Remove(blockedPath); err != nil {
		t.Fatalf("remove bot capability state before blocking: %v", err)
	}
	if err := os.Mkdir(blockedPath, 0o700); err != nil {
		t.Fatalf("block bot capability state: %v", err)
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appId":"cli_new"}`)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed transition status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}
	identity, ok := app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_old" || identity.Generation != 1 || identity.Pending == nil {
		t.Fatalf("failed transition identity = %#v, present=%v", identity, ok)
	}

	if err := os.Remove(blockedPath); err != nil {
		t.Fatalf("remove state blocker: %v", err)
	}
	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"appId":"cli_old"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("revert app id status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.removed) != 2 {
		t.Fatalf("runtime removals after revert = %#v, want old generation removed again", gateway.removed)
	}
	if len(gateway.upserted) != 2 || gateway.upserted[1].AppID != "cli_old" {
		t.Fatalf("runtime upserts after revert = %#v, want old app as fresh generation", gateway.upserted)
	}
	identity, ok = app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_old" || identity.Generation != 2 || identity.Pending != nil {
		t.Fatalf("identity after revert = %#v, present=%v", identity, ok)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.service.Surfaces()) != 0 || len(app.surfaceResumeRuntime.store.Entries()) != 0 {
		t.Fatalf("old surfaces after revert: service=%#v durable=%#v", app.service.Surfaces(), app.surfaceResumeRuntime.store.Entries())
	}
	if _, ok := app.botCapabilitySettingsState.store.Get(state.BotCapabilitySettingsKey("main")); ok {
		t.Fatal("old bot capability settings survived revert after failed transition")
	}
}

func TestFeishuDeleteFailureThenRecreateSameAppStillPurgesOldGeneration(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "main", AppID: "cli_old", AppSecret: "secret_old"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")
	if err := app.applyRuntimeFeishuConfigs(cfg); err != nil {
		t.Fatalf("apply initial runtime: %v", err)
	}
	seedFeishuGatewayIdentityState(t, app)

	stateDir := filepath.Dir(app.surfaceResumeRuntime.path)
	blockedPath := botcapabilitysettings.StatePath(stateDir)
	if err := os.Remove(blockedPath); err != nil {
		t.Fatalf("remove bot capability state before blocking: %v", err)
	}
	if err := os.Mkdir(blockedPath, 0o700); err != nil {
		t.Fatalf("block bot capability state: %v", err)
	}

	rec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/feishu/apps/main", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("failed delete status = %d, want 500 body=%s", rec.Code, rec.Body.String())
	}
	identity, ok := app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_old" || identity.Generation != 1 || identity.Pending == nil {
		t.Fatalf("failed delete identity = %#v, present=%v", identity, ok)
	}

	if err := os.Remove(blockedPath); err != nil {
		t.Fatalf("remove state blocker: %v", err)
	}
	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_new"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recreate status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.removed) != 2 {
		t.Fatalf("runtime removals after recreate = %#v, want old generation removed again", gateway.removed)
	}
	if len(gateway.upserted) != 2 || gateway.upserted[1].AppID != "cli_old" {
		t.Fatalf("runtime upserts after recreate = %#v, want recreated app as fresh generation", gateway.upserted)
	}
	identity, ok = app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_old" || identity.Generation != 2 || identity.Pending != nil {
		t.Fatalf("identity after recreate = %#v, present=%v", identity, ok)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if len(app.service.Surfaces()) != 0 || len(app.surfaceResumeRuntime.store.Entries()) != 0 {
		t.Fatalf("old surfaces after recreate: service=%#v durable=%#v", app.service.Surfaces(), app.surfaceResumeRuntime.store.Entries())
	}
	if _, ok := app.botCapabilitySettingsState.store.Get(state.BotCapabilitySettingsKey("main")); ok {
		t.Fatal("old bot capability settings survived recreate after failed delete")
	}
}

func TestStartupReconcilesOfflineFeishuAppIDChange(t *testing.T) {
	stateDir := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "main", AppID: "cli_old", AppSecret: "secret_old"}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("write old config: %v", err)
	}

	firstGateway := &fakeAdminGatewayController{}
	first := newFeishuIdentityRuntimeTestApp(t, configPath, stateDir, firstGateway)
	if err := first.applyRuntimeFeishuConfigs(cfg); err != nil {
		t.Fatalf("apply initial runtime: %v", err)
	}
	seedFeishuGatewayIdentityState(t, first)

	updated := cfg
	updated.Feishu.Apps[0].AppID = "cli_new"
	if err := config.WriteAppConfig(configPath, updated); err != nil {
		t.Fatalf("write updated config: %v", err)
	}

	restartedGateway := &fakeAdminGatewayController{}
	restarted := newFeishuIdentityRuntimeTestApp(t, configPath, stateDir, restartedGateway)
	if err := restarted.applyRuntimeFeishuConfigs(updated); err != nil {
		t.Fatalf("reconcile restarted runtime: %v", err)
	}
	if len(restartedGateway.upserted) != 1 || restartedGateway.upserted[0].AppID != "cli_new" {
		t.Fatalf("restarted runtime upserts = %#v, want new app only", restartedGateway.upserted)
	}
	if len(restartedGateway.removed) != 1 || restartedGateway.removed[0] != "main" {
		t.Fatalf("restarted runtime removals = %#v, want stale slot removal", restartedGateway.removed)
	}

	restarted.mu.Lock()
	defer restarted.mu.Unlock()
	if len(restarted.service.Surfaces()) != 0 || len(restarted.surfaceResumeRuntime.store.Entries()) != 0 {
		t.Fatalf("restarted runtime retained old surfaces: service=%#v store=%#v", restarted.service.Surfaces(), restarted.surfaceResumeRuntime.store.Entries())
	}
	room, ok := restarted.feishuRoomState.store.Get("oc_room")
	if !ok || room.WorkspaceKey != "/data/workspaces/shared" || room.PrimaryGatewayID != "" {
		t.Fatalf("restarted room state = %#v, present=%v", room, ok)
	}
}

func seedFeishuGatewayIdentityState(t *testing.T, app *App) {
	t.Helper()
	app.mu.Lock()
	defer app.mu.Unlock()
	app.service.MaterializeSurfaceResumeContract(
		"feishu:main:user:ou_user",
		"main",
		"ou_user",
		"ou_user",
		state.HeadlessClaudeSurfaceBackendContract("claude-profile"),
		state.SurfaceVerbosityVerbose,
		state.PlanModeSettingOff,
	)
	app.service.MaterializeBotCapabilitySettings([]state.BotCapabilitySettingsRecord{{
		GatewayID:       "main",
		ProductMode:     state.ProductModeNormal,
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "claude-profile",
	}})
	app.service.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		WorkspaceKey:     "/data/workspaces/shared",
		PrimaryGatewayID: "main",
	}})
	app.syncSurfaceResumeStateLocked(nil)
	app.syncBotCapabilitySettingsStateLocked()
	app.syncFeishuRoomStateLocked()
}

func newFeishuIdentityRuntimeTestApp(t *testing.T, configPath, stateDir string, gateway *fakeAdminGatewayController) *App {
	t.Helper()
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{Paths: relayruntime.Paths{StateDir: stateDir}})
	services := defaultFeishuServices()
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:      configPath,
		Services:        services,
		AdminListenHost: services.RelayAPIHost,
		AdminListenPort: services.RelayAPIPort,
		AdminURL:        "http://localhost:" + services.RelayAPIPort + "/admin/",
		SetupURL:        "http://localhost:" + services.RelayAPIPort + "/setup",
	})
	return app
}
