package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type fakeAdminGatewayController struct {
	statuses      []feishu.GatewayStatus
	upserted      []feishu.GatewayAppConfig
	removed       []string
	verifyConfigs []feishu.GatewayAppConfig
	verifyResult  feishu.VerifyResult
	verifyErr     error
}

func (f *fakeAdminGatewayController) Start(context.Context, feishu.ActionHandler) error { return nil }
func (f *fakeAdminGatewayController) Apply(context.Context, []feishu.Operation) error   { return nil }
func (f *fakeAdminGatewayController) RewriteFinalBlock(_ context.Context, req feishu.MarkdownPreviewRequest) (render.Block, error) {
	return req.Block, nil
}
func (f *fakeAdminGatewayController) UpsertApp(_ context.Context, cfg feishu.GatewayAppConfig) error {
	f.upserted = append(f.upserted, cfg)
	return nil
}
func (f *fakeAdminGatewayController) RemoveApp(_ context.Context, gatewayID string) error {
	f.removed = append(f.removed, gatewayID)
	return nil
}
func (f *fakeAdminGatewayController) Verify(_ context.Context, cfg feishu.GatewayAppConfig) (feishu.VerifyResult, error) {
	f.verifyConfigs = append(f.verifyConfigs, cfg)
	if f.verifyResult == (feishu.VerifyResult{}) {
		f.verifyResult = feishu.VerifyResult{Connected: true}
	}
	return f.verifyResult, f.verifyErr
}
func (f *fakeAdminGatewayController) Status() []feishu.GatewayStatus {
	return append([]feishu.GatewayStatus(nil), f.statuses...)
}

func TestFeishuManifestAndScopesRoutes(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")

	req := httptest.NewRequest(http.MethodGet, "/api/admin/feishu/manifest", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var manifestResp feishuManifestResponse
	if err := json.NewDecoder(rec.Body).Decode(&manifestResp); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifestResp.Manifest.Scopes.Scopes.Tenant[0] != "drive:drive" {
		t.Fatalf("unexpected manifest scopes: %#v", manifestResp.Manifest.Scopes)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/feishu/apps/main/scopes-json", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scopes status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var scopes feishuapp.ScopesImport
	if err := json.NewDecoder(rec.Body).Decode(&scopes); err != nil {
		t.Fatalf("decode scopes: %v", err)
	}
	if scopes.Scopes.Tenant[0] != "drive:drive" {
		t.Fatalf("unexpected scopes payload: %#v", scopes)
	}
}

func TestFeishuAppsCreateUpdateVerifyAndDisable(t *testing.T) {
	cfg := config.DefaultAppConfig()
	gateway := &fakeAdminGatewayController{
		verifyResult: feishu.VerifyResult{Connected: true, Duration: time.Second},
	}
	app, configPath := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), gateway, false, "")

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	if len(gateway.upserted) != 1 || gateway.upserted[0].GatewayID != "main" {
		t.Fatalf("unexpected upserted configs: %#v", gateway.upserted)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	if len(loaded.Config.Feishu.Apps) != 1 || loaded.Config.Feishu.Apps[0].AppSecret != "secret_xxx" {
		t.Fatalf("unexpected saved config after create: %#v", loaded.Config.Feishu.Apps)
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"name":"Main Bot 2","appSecret":""}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(update): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].Name != "Main Bot 2" || loaded.Config.Feishu.Apps[0].AppSecret != "secret_xxx" {
		t.Fatalf("unexpected saved config after update: %#v", loaded.Config.Feishu.Apps[0])
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/verify", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(verify): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].VerifiedAt == nil {
		t.Fatalf("expected verifiedAt to be persisted, got %#v", loaded.Config.Feishu.Apps[0])
	}

	rec = performAdminRequest(t, app, http.MethodPost, "/api/admin/feishu/apps/main/disable", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("disable status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	loaded, err = config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath(disable): %v", err)
	}
	if loaded.Config.Feishu.Apps[0].Enabled == nil || *loaded.Config.Feishu.Apps[0].Enabled {
		t.Fatalf("expected app disabled, got %#v", loaded.Config.Feishu.Apps[0].Enabled)
	}
	if len(gateway.upserted) < 3 || gateway.upserted[len(gateway.upserted)-1].Enabled {
		t.Fatalf("expected disable to hot-apply runtime config, got %#v", gateway.upserted)
	}
}

func TestFeishuAppsListMarksEnvOverrideReadOnly(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Config Main",
		AppID:     "cli_config",
		AppSecret: "secret_config",
	}}
	services := defaultFeishuServices()
	services.FeishuGatewayID = "main"
	services.FeishuAppID = "cli_env"
	services.FeishuAppSecret = "secret_env"

	gateway := &fakeAdminGatewayController{
		statuses: []feishu.GatewayStatus{{
			GatewayID: "main",
			State:     feishu.GatewayStateConnected,
		}},
	}
	app, _ := newFeishuAdminTestApp(t, cfg, services, gateway, true, "main")

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/feishu/apps", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload feishuAppsResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode apps: %v", err)
	}
	if len(payload.Apps) != 1 {
		t.Fatalf("expected one app, got %#v", payload.Apps)
	}
	appSummary := payload.Apps[0]
	if !appSummary.ReadOnly || !appSummary.RuntimeOverride {
		t.Fatalf("expected read-only runtime override, got %#v", appSummary)
	}
	if appSummary.AppID != "cli_env" {
		t.Fatalf("expected runtime app id, got %#v", appSummary)
	}
	if appSummary.Status == nil || appSummary.Status.State != feishu.GatewayStateConnected {
		t.Fatalf("expected connected status, got %#v", appSummary.Status)
	}

	rec = performAdminRequest(t, app, http.MethodPut, "/api/admin/feishu/apps/main", `{"name":"Should Fail"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("update status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "runtime_override_read_only") {
		t.Fatalf("unexpected read-only error body: %s", rec.Body.String())
	}
}

func newFeishuAdminTestApp(t *testing.T, cfg config.AppConfig, services config.ServicesConfig, gateway feishu.GatewayController, envOverrideActive bool, envOverrideGatewayID string) (*App, string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		Paths: relayruntime.Paths{
			StateDir: t.TempDir(),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath:           configPath,
		Services:             services,
		AdminListenHost:      services.RelayAPIHost,
		AdminListenPort:      services.RelayAPIPort,
		AdminURL:             "http://localhost:" + services.RelayAPIPort + "/",
		SetupURL:             "http://localhost:" + services.RelayAPIPort + "/setup",
		EnvOverrideActive:    envOverrideActive,
		EnvOverrideGatewayID: envOverrideGatewayID,
	})
	return app, configPath
}

func defaultFeishuServices() config.ServicesConfig {
	return config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
}

func performAdminRequest(t *testing.T, app *App, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:12345"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	return rec
}
