package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/adminauth"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestSetupSessionCanUseFeishuAndVSCodeSetupAPIsAfterCredentialsSaved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	req := httptest.NewRequest(http.MethodPost, "/api/setup/feishu/apps", strings.NewReader(`{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`))
	req.RemoteAddr = "198.51.100.20:23456"
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/setup/bootstrap-state", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap state status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var bootstrap bootstrapStatePayload
	if err := json.NewDecoder(rec.Body).Decode(&bootstrap); err != nil {
		t.Fatalf("decode bootstrap state: %v", err)
	}
	if !bootstrap.SetupRequired {
		t.Fatal("expected setupRequired to remain true before machine decisions are recorded")
	}
	if bootstrap.Session.Scope != "setup" {
		t.Fatalf("session scope = %q, want setup", bootstrap.Session.Scope)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/setup/vscode/detect", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("vscode detect status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup page status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetupCompleteRevokesRemoteSetupSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	req := httptest.NewRequest(http.MethodPost, "/api/setup/complete", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("complete before config status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/setup/feishu/apps", strings.NewReader(`{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`))
	req.RemoteAddr = "198.51.100.20:23456"
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/setup/feishu/apps/main/verify", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/setup/onboarding/machine-decisions/autostart", strings.NewReader(`{"decision":"deferred"}`))
	req.RemoteAddr = "198.51.100.20:23456"
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("autostart decision status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/setup/onboarding/machine-decisions/vscode", strings.NewReader(`{"decision":"remote_only"}`))
	req.RemoteAddr = "198.51.100.20:23456"
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("vscode decision status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/setup/complete", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("complete status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var payload setupCompleteResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode setup complete: %v", err)
	}
	if payload.SetupRequired {
		t.Fatalf("unexpected setupRequired payload: %#v", payload)
	}
	if payload.AdminURL != "http://10.0.0.8:9501/admin/" {
		t.Fatalf("admin url = %q, want remote /admin/", payload.AdminURL)
	}
	foundExpiredCookie := false
	for _, responseCookie := range rec.Result().Cookies() {
		if responseCookie.Name == adminauth.CookieName && responseCookie.MaxAge < 0 {
			foundExpiredCookie = true
		}
	}
	if !foundExpiredCookie {
		t.Fatalf("expected setup complete to expire setup cookie, cookies=%#v", rec.Result().Cookies())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/setup/bootstrap-state", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bootstrap after complete status = %d, want 401 body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("setup page after complete status = %d, want 401 body=%s", rec.Code, rec.Body.String())
	}
}

func TestSetupOnboardingWorkflowTracksMachineDecisionsAndManualSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	req := performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`, cookie)
	rec := performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps/main/verify", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/onboarding/machine-decisions/autostart", `{"decision":"deferred"}`, cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("autostart decision status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/onboarding/machine-decisions/vscode", `{"decision":"remote_only"}`, cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("vscode decision status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps/main/onboarding-steps/menu/complete", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("menu step status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodGet, "/api/setup/onboarding/workflow?app=main", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workflow status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var payload onboardingWorkflowResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	if payload.SelectedAppID != "main" {
		t.Fatalf("selected app = %q, want main", payload.SelectedAppID)
	}
	if !payload.Completion.CanComplete || payload.Completion.SetupRequired {
		t.Fatalf("unexpected completion gate: %#v", payload.Completion)
	}
	if payload.Autostart.Status != onboardingStageStatusDeferred && payload.Autostart.Status != onboardingStageStatusNotApplicable {
		t.Fatalf("autostart status = %q, want deferred or not_applicable", payload.Autostart.Status)
	}
	if payload.VSCode.Status != onboardingStageStatusDeferred {
		t.Fatalf("vscode status = %q, want deferred", payload.VSCode.Status)
	}
	if payload.App == nil || payload.App.Menu.Status != onboardingStageStatusComplete {
		t.Fatalf("menu step = %#v, want complete", payload.App)
	}
}

func TestSetupOnboardingPermissionStepSupportsForceSkipAndReset(t *testing.T) {
	oldListScopes := listFeishuAppScopes
	listFeishuAppScopes = func(context.Context, feishu.LiveGatewayConfig) ([]feishu.AppScopeStatus, error) {
		return []feishu.AppScopeStatus{
			{ScopeName: "im:message", ScopeType: "tenant", GrantStatus: 1},
			{ScopeName: "im:message:send_as_bot", ScopeType: "tenant", GrantStatus: 1},
		}, nil
	}
	t.Cleanup(func() {
		listFeishuAppScopes = oldListScopes
	})

	home := t.TempDir()
	t.Setenv("HOME", home)

	app, token := newRemoteSetupTestApp(t, home)
	cookie := exchangeSetupSessionCookie(t, app, token)

	req := performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps", `{"id":"main","name":"Main Bot","appId":"cli_xxx","appSecret":"secret_xxx"}`, cookie)
	rec := performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps/main/verify", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("verify status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodGet, "/api/setup/onboarding/workflow?app=main", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workflow status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var payload onboardingWorkflowResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode workflow before skip: %v", err)
	}
	if payload.App == nil || payload.App.Permission.Status != onboardingStageStatusPending {
		t.Fatalf("permission before skip = %#v, want pending", payload.App)
	}
	if payload.CurrentStage != onboardingStagePermission {
		t.Fatalf("current stage before skip = %q, want permission", payload.CurrentStage)
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps/main/onboarding-permission/skip", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("permission skip status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodGet, "/api/setup/onboarding/workflow?app=main", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workflow after skip status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode workflow after skip: %v", err)
	}
	if payload.App == nil || payload.App.Permission.Status != onboardingStageStatusDeferred {
		t.Fatalf("permission after skip = %#v, want deferred", payload.App)
	}
	if payload.CurrentStage != onboardingStageEvents {
		t.Fatalf("current stage after skip = %q, want events", payload.CurrentStage)
	}

	req = performSetupRequestWithCookie(http.MethodPost, "/api/setup/feishu/apps/main/onboarding-permission/reset", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("permission reset status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}

	req = performSetupRequestWithCookie(http.MethodGet, "/api/setup/onboarding/workflow?app=main", "", cookie)
	rec = performSetupRequestRecorder(app, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("workflow after reset status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode workflow after reset: %v", err)
	}
	if payload.App == nil || payload.App.Permission.Status != onboardingStageStatusPending {
		t.Fatalf("permission after reset = %#v, want pending", payload.App)
	}
	if payload.CurrentStage != onboardingStagePermission {
		t.Fatalf("current stage after reset = %q, want permission", payload.CurrentStage)
	}
}

func newRemoteSetupTestApp(t *testing.T, home string) (*App, string) {
	t.Helper()

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.WriteAppConfig(configPath, config.DefaultAppConfig()); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}

	binaryPath := filepath.Join(home, "bin", executableName("codex-remote"))
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(binary dir): %v", err)
	}
	if err := os.WriteFile(binaryPath, []byte("wrapper-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(binary): %v", err)
	}
	realBinaryPath := filepath.Join(home, "bin", executableName("codex-real"))
	if err := os.WriteFile(realBinaryPath, []byte("real-binary"), 0o755); err != nil {
		t.Fatalf("WriteFile(real binary): %v", err)
	}

	loaded, err := config.LoadAppConfigAtPath(configPath)
	if err != nil {
		t.Fatalf("LoadAppConfigAtPath: %v", err)
	}
	cfg := loaded.Config
	cfg.Wrapper.CodexRealBinary = realBinaryPath
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig(update real binary): %v", err)
	}

	app := New(":0", ":0", &fakeAdminGatewayController{}, agentproto.ServerIdentity{})
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: binaryPath,
		Paths: relayruntime.Paths{
			DataDir:  filepath.Join(home, ".local", "share", "codex-remote"),
			StateDir: filepath.Join(home, ".local", "state", "codex-remote"),
		},
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		ConfigPath: configPath,
		Services: config.ServicesConfig{
			RelayHost:    "127.0.0.1",
			RelayPort:    "9500",
			RelayAPIHost: "0.0.0.0",
			RelayAPIPort: "9501",
		},
		AdminListenHost: "0.0.0.0",
		AdminListenPort: "9501",
		AdminURL:        "http://10.0.0.8:9501/admin/",
		SetupURL:        "http://10.0.0.8:9501/setup",
		SSHSession:      true,
		SetupRequired:   true,
	})
	token, _, err := app.EnableSetupAccess(time.Hour)
	if err != nil {
		t.Fatalf("EnableSetupAccess: %v", err)
	}
	return app, token
}

func exchangeSetupSessionCookie(t *testing.T, app *App, token string) *http.Cookie {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/setup?token="+url.QueryEscape(token), nil)
	req.RemoteAddr = "198.51.100.20:23456"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup exchange status = %d, want 303 body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected setup session cookie")
	}
	return cookies[0]
}

func performSetupRequestWithCookie(method, path, body string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "198.51.100.20:23456"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(cookie)
	return req
}

func performSetupRequestRecorder(app *App, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	return rec
}
