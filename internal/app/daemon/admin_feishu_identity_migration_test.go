package daemon

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/botcapabilitysettings"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishuroomstate"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/surfaceresume"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFirstIdentityStoreStartupAdoptsCurrentAppWithoutPurgingExistingState(t *testing.T) {
	stateDir := t.TempDir()
	resumeStore, err := surfaceresume.LoadStore(surfaceresume.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load resume store: %v", err)
	}
	if err := resumeStore.Put(surfaceresume.Entry{SurfaceSessionID: "feishu:main:user:ou_user", GatewayID: "main", ChatID: "ou_user", ActorUserID: "ou_user"}); err != nil {
		t.Fatalf("seed resume store: %v", err)
	}
	settingsStore, err := botcapabilitysettings.LoadStore(botcapabilitysettings.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load settings store: %v", err)
	}
	if err := settingsStore.Put(state.BotCapabilitySettingsRecord{GatewayID: "main", ProductMode: state.ProductModeNormal, Backend: agentproto.BackendClaude, ClaudeProfileID: "claude-profile"}); err != nil {
		t.Fatalf("seed settings store: %v", err)
	}
	roomStore, err := feishuroomstate.LoadStore(feishuroomstate.StatePath(stateDir))
	if err != nil {
		t.Fatalf("load room store: %v", err)
	}
	if err := roomStore.Put(state.FeishuRoomStateRecord{RoomID: "feishu:chat:oc_room", ChatID: "oc_room", WorkspaceKey: "/data/workspaces/shared", PrimaryGatewayID: "main"}); err != nil {
		t.Fatalf("seed room store: %v", err)
	}

	configPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "main", AppID: "cli_current", AppSecret: "secret_current"}}
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}
	gateway := &fakeAdminGatewayController{}
	app := newFeishuIdentityRuntimeTestApp(t, configPath, stateDir, gateway)
	if err := app.applyRuntimeFeishuConfigs(cfg); err != nil {
		t.Fatalf("apply first identity-aware startup: %v", err)
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if app.service.Surface("feishu:main:user:ou_user") == nil {
		t.Fatal("first identity-aware startup purged existing surface")
	}
	if _, ok := app.botCapabilitySettingsState.store.Get(state.BotCapabilitySettingsKey("main")); !ok {
		t.Fatal("first identity-aware startup purged existing settings")
	}
	room, ok := app.feishuRoomState.store.Get("oc_room")
	if !ok || room.PrimaryGatewayID != "main" || room.WorkspaceKey != "/data/workspaces/shared" {
		t.Fatalf("first identity-aware startup changed room state: %#v, present=%v", room, ok)
	}
	identity, ok := app.feishuBotIdentityState.store.Get("main")
	if !ok || identity.AppID != "cli_current" || identity.Generation != 1 {
		t.Fatalf("adopted identity = %#v, present=%v", identity, ok)
	}
}

func TestDeletingFeishuAppPurgesIdentityBeforeSameSlotCanBeRecreated(t *testing.T) {
	gateway := &fakeAdminGatewayController{}
	app, _ := newFeishuAdminTestApp(t, config.DefaultAppConfig(), defaultFeishuServices(), gateway, false, "")
	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_old"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	seedFeishuGatewayIdentityState(t, app)

	rec = performAdminRequest(t, app, http.MethodDelete, "/api/admin/feishu/apps/main", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}
	app.mu.Lock()
	if _, ok := app.feishuBotIdentityState.store.Get("main"); ok {
		app.mu.Unlock()
		t.Fatal("deleted app retained committed identity")
	}
	if len(app.service.Surfaces()) != 0 || len(app.surfaceResumeRuntime.store.Entries()) != 0 {
		app.mu.Unlock()
		t.Fatal("deleted app retained gateway surfaces")
	}
	app.mu.Unlock()

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","appId":"cli_old","appSecret":"secret_new"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recreate status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.upserted) != 2 || gateway.upserted[1].AppID != "cli_old" {
		t.Fatalf("recreated runtime upserts = %#v", gateway.upserted)
	}
}
