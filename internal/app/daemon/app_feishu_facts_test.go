package daemon

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/feishufacts"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestForceRefreshFeishuBotFactsPersistsNameOpenIDAndScopes(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", Name: "Old Bot", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{AppName: "New Bot", OpenID: "ou_bot"}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return []feishu.AppScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}

	record, err := app.RefreshFeishuBotFacts(context.Background(), "main")
	if err != nil {
		t.Fatalf("RefreshFeishuBotFacts: %v", err)
	}
	if record.AppName != "New Bot" || record.BotOpenID != "ou_bot" {
		t.Fatalf("bot facts = %#v", record)
	}
	if len(record.Scopes) != 1 || record.Scopes[0].ScopeName != "im:message.group_msg" {
		t.Fatalf("scopes = %#v", record.Scopes)
	}
	if record.FetchedAt.IsZero() {
		t.Fatal("expected fetchedAt to be recorded")
	}

	reloaded, err := feishufacts.LoadStore(feishufacts.StatePath(stateDir))
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	persisted, ok := reloaded.Get("main")
	if !ok {
		t.Fatal("expected facts to persist")
	}
	if persisted.AppName != "New Bot" || persisted.BotOpenID != "ou_bot" || len(persisted.Scopes) != 1 {
		t.Fatalf("persisted facts = %#v", persisted)
	}
}

func TestForceRefreshFeishuBotFactsKeepsOldValuesOnFailure(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", Name: "Old Bot", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)
	old := time.Date(2026, 8, 6, 3, 0, 0, 0, time.UTC)
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		AppName:   "Old Bot",
		BotOpenID: "ou_old",
		Scopes: []feishufacts.ScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		},
		FetchedAt: old,
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{}, errors.New("bot info failed")
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return nil, errors.New("scopes failed")
	}

	record, err := app.RefreshFeishuBotFacts(context.Background(), "main")
	if err == nil {
		t.Fatal("expected refresh to fail")
	}
	if record.AppName != "Old Bot" || record.BotOpenID != "ou_old" {
		t.Fatalf("refresh wiped old bot facts: %#v", record)
	}
	if len(record.Scopes) != 1 || record.Scopes[0].ScopeName != "im:message.group_msg" {
		t.Fatalf("refresh wiped old scopes: %#v", record.Scopes)
	}
	if record.LastError == "" || record.LastErrorAt.IsZero() {
		t.Fatalf("expected last error to be recorded: %#v", record)
	}
	if record.FetchedAt.Equal(old) {
		t.Fatal("expected fetchedAt to advance even on failure")
	}
}

func TestForceRefreshFeishuBotFactsMergesPartialSuccess(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", Name: "Old Bot", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		AppName:   "Old Bot",
		BotOpenID: "ou_old",
		Scopes: []feishufacts.ScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		},
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{AppName: "New Bot", OpenID: "ou_new"}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return nil, errors.New("scopes failed")
	}

	record, err := app.RefreshFeishuBotFacts(context.Background(), "main")
	if err == nil {
		t.Fatal("expected scope failure to surface")
	}
	if record.AppName != "New Bot" || record.BotOpenID != "ou_new" {
		t.Fatalf("bot info merge = %#v", record)
	}
	if len(record.Scopes) != 1 || record.Scopes[0].ScopeName != "im:message.group_msg" {
		t.Fatalf("old scopes should survive scope failure: %#v", record.Scopes)
	}
}

func TestMaybeStartFeishuFactsRefreshRefreshesConfiguredApps(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", Name: "Old Bot", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{AppName: "Scheduled Bot", OpenID: "ou_scheduled"}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return []feishu.AppScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}

	now := time.Now().UTC()
	app.maybeStartFeishuFactsRefreshLocked(now)

	deadline := time.Now().Add(2 * time.Second)
	for {
		record, ok := app.FeishuBotFacts("main")
		if ok && record.AppName == "Scheduled Bot" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("facts refresh did not complete in time: %#v ok=%v", record, ok)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !app.feishuFactsState.nextRefresh.After(now) {
		t.Fatal("expected next refresh to be scheduled after now")
	}

	for time.Now().Before(deadline) {
		app.feishuFactsState.mu.RLock()
		inFlight := app.feishuFactsState.refreshInFlight
		app.feishuFactsState.mu.RUnlock()
		if !inFlight {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.feishuFactsState.mu.RLock()
	inFlight := app.feishuFactsState.refreshInFlight
	app.feishuFactsState.mu.RUnlock()
	if inFlight {
		t.Fatal("expected background refresh to finish")
	}
}

func TestMaybeStartFeishuFactsRefreshThrottlesBeforeNextRefresh(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	now := time.Now().UTC()
	app.feishuFactsState.nextRefresh = now.Add(time.Hour)

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		t.Fatal("bot info fetch should not run before next refresh")
		return feishu.BotInfo{}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		t.Fatal("scope fetch should not run before next refresh")
		return nil, nil
	}

	app.maybeStartFeishuFactsRefreshLocked(now)
	if app.feishuFactsState.refreshInFlight {
		t.Fatal("refresh should not start before next refresh")
	}
}

func TestPrimaryPermissionCheckerUsesFreshFactsWhenNotForced(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		Scopes: []feishufacts.ScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		},
		FetchedAt:       time.Now().UTC(),
		ScopesFetchedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	previousScopes := listFeishuAppConfiguredScopes
	defer func() { listFeishuAppConfiguredScopes = previousScopes }()
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		t.Fatal("scope fetch should not run when facts are fresh")
		return nil, nil
	}

	decision := app.CheckPrimaryBotPermission(context.Background(), orchestrator.PrimaryBotPermissionRequest{
		GatewayID: "main",
	})
	if !decision.Allowed || decision.Scope != "im:message.group_msg" {
		t.Fatalf("fresh facts decision = %#v, want allowed", decision)
	}
}

func TestPrimaryPermissionCheckerForceRefreshUpdatesFacts(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		Scopes: []feishufacts.ScopeStatus{
			{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
		},
		FetchedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	previousScopes := listFeishuAppConfiguredScopes
	previousBotInfo := getFeishuBotInfo
	defer func() {
		listFeishuAppConfiguredScopes = previousScopes
		getFeishuBotInfo = previousBotInfo
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return []feishu.AppScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}

	decision := app.CheckPrimaryBotPermission(context.Background(), orchestrator.PrimaryBotPermissionRequest{
		GatewayID:    "main",
		ForceRefresh: true,
	})
	if !decision.Allowed {
		t.Fatalf("force refresh decision = %#v, want allowed", decision)
	}
	record, ok := app.FeishuBotFacts("main")
	if !ok || len(record.Scopes) != 1 || record.Scopes[0].ScopeName != "im:message.group_msg" {
		t.Fatalf("force refresh did not update facts: %#v ok=%v", record, ok)
	}
}

func TestPrimaryPermissionCheckerIgnoresFactsAfterScopeFetchFailure(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)
	now := time.Now().UTC()
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		Scopes: []feishufacts.ScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		},
		FetchedAt:       now,
		ScopesFetchedAt: now,
		ScopesError:     "scopes failed",
		LastError:       "scopes failed",
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return nil, errors.New("scopes failed")
	}

	decision := app.CheckPrimaryBotPermission(context.Background(), orchestrator.PrimaryBotPermissionRequest{
		GatewayID: "main",
	})
	if decision.Allowed || decision.Reason != "scope_read_failed" {
		t.Fatalf("decision after scope failure = %#v, want scope_read_failed", decision)
	}
}

func TestAdminFeishuAppsPrefersFactsAppName(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", Name: "Old Config Bot", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(stateDir)
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		AppName:   "New Facts Bot",
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	loaded, err := app.loadAdminConfig()
	if err != nil {
		t.Fatalf("loadAdminConfig: %v", err)
	}
	summaries, err := app.adminFeishuApps(loaded)
	if err != nil {
		t.Fatalf("adminFeishuApps: %v", err)
	}
	if len(summaries) != 1 || summaries[0].Name != "New Facts Bot" {
		t.Fatalf("summaries = %#v, want facts app name", summaries)
	}
}

func TestBuildRuntimeGatewayAppsUsesBotOpenIDHook(t *testing.T) {
	apps := buildRuntimeGatewayApps(config.AppConfig{
		Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
			{ID: "main", AppID: "cli_test", AppSecret: "secret"},
		}},
	}, config.ServicesConfig{}, relayruntime.Paths{}, gatewayRuntimeHooks{
		BotOpenIDForGateway: func(gatewayID string) string {
			if gatewayID != "main" {
				t.Fatalf("unexpected gateway id %q", gatewayID)
			}
			return "ou_hook"
		},
	})
	if len(apps) != 1 || apps[0].BotOpenID != "ou_hook" {
		t.Fatalf("runtime apps = %#v, want injected bot open id", apps)
	}
}

func TestGatewayRuntimeHooksReadBotOpenIDFromFacts(t *testing.T) {
	stateDir := t.TempDir()
	app := New(":0", ":0", &recordingGateway{}, serverIdentityForTest())
	app.configureFeishuFactsStateLocked(stateDir)
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		BotOpenID: "ou_facts",
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	hooks := app.gatewayRuntimeHooks()
	if hooks.BotOpenIDForGateway == nil {
		t.Fatal("expected BotOpenIDForGateway hook")
	}
	if got := hooks.BotOpenIDForGateway("main"); got != "ou_facts" {
		t.Fatalf("BotOpenIDForGateway = %q, want ou_facts", got)
	}
}

func TestFeishuAppFactsGetReturnsPersistedFacts(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{
		{ID: "main", Name: "Old Bot", AppID: "cli_test", AppSecret: "secret"},
	}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		AppName:   "New Facts Bot",
		BotOpenID: "ou_facts",
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps/main/facts", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("facts status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"appName":"New Facts Bot"`) || !strings.Contains(body, `"botOpenID":"ou_facts"`) {
		t.Fatalf("facts body = %s", body)
	}
}

func TestFeishuAppFactsRefreshUpdatesPersistedFacts(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{
		{ID: "main", Name: "Old Bot", AppID: "cli_test", AppSecret: "secret"},
	}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	if err := app.feishuFactsState.store.Put(feishufacts.Record{
		GatewayID: "main",
		AppID:     "cli_test",
		AppName:   "Old Bot",
	}); err != nil {
		t.Fatalf("seed facts: %v", err)
	}

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{AppName: "Synced Bot", OpenID: "ou_synced"}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return []feishu.AppScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/facts/refresh", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	record, ok := app.FeishuBotFacts("main")
	if !ok || record.AppName != "Synced Bot" || record.BotOpenID != "ou_synced" {
		t.Fatalf("facts after refresh = %#v ok=%v", record, ok)
	}
}

func TestRefreshFeishuBotFactsPushesOpenIDToRunningGateway(t *testing.T) {
	gateway := &botOpenIDSettingGateway{}
	app := New(":0", ":0", gateway, serverIdentityForTest())
	app.admin.loadConfig = func() (config.LoadedAppConfig, error) {
		return config.LoadedAppConfig{Config: config.AppConfig{
			Feishu: config.FeishuSettings{Apps: []config.FeishuAppConfig{
				{ID: "main", AppID: "cli_test", AppSecret: "secret"},
			}},
		}}, nil
	}
	app.configureFeishuFactsStateLocked(t.TempDir())

	previousBotInfo := getFeishuBotInfo
	previousScopes := listFeishuAppConfiguredScopes
	defer func() {
		getFeishuBotInfo = previousBotInfo
		listFeishuAppConfiguredScopes = previousScopes
	}()
	getFeishuBotInfo = func(context.Context, feishu.LiveGatewayConfig) (feishu.BotInfo, error) {
		return feishu.BotInfo{AppName: "Bot", OpenID: "ou_bot"}, nil
	}
	listFeishuAppConfiguredScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return nil, nil
	}

	if _, err := app.RefreshFeishuBotFacts(context.Background(), "main"); err != nil {
		t.Fatalf("RefreshFeishuBotFacts: %v", err)
	}
	if len(gateway.setCalls) != 1 || gateway.setCalls[0] != "main|ou_bot" {
		t.Fatalf("gateway open id pushes = %#v, want main|ou_bot", gateway.setCalls)
	}
}

type botOpenIDSettingGateway struct {
	recordingGateway
	setCalls []string
}

func (g *botOpenIDSettingGateway) SetBotOpenID(gatewayID, openID string) {
	g.setCalls = append(g.setCalls, gatewayID+"|"+openID)
}
