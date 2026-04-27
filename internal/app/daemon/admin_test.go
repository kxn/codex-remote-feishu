package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestSetupTokenExchangeEnablesSetupBootstrapAPI(t *testing.T) {
	cfg := config.DefaultAppConfig()
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        services,
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

	req := httptest.NewRequest(http.MethodGet, "/setup?token="+url.QueryEscape(token), nil)
	req.RemoteAddr = "198.51.100.20:23456"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("setup exchange status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected setup session cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/setup/bootstrap-state", nil)
	req.RemoteAddr = "198.51.100.20:23456"
	req.AddCookie(cookies[0])
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap state status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var payload bootstrapStatePayload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode bootstrap state: %v", err)
	}
	if !payload.SetupRequired {
		t.Fatal("expected setup required")
	}
	if payload.Session.Scope != "setup" {
		t.Fatalf("session scope = %q, want setup", payload.Session.Scope)
	}
	if !payload.Session.Authenticated {
		t.Fatal("expected authenticated session")
	}
	if payload.Admin.URL != "http://10.0.0.8:9501/admin/" {
		t.Fatalf("admin url = %q, want remote /admin/", payload.Admin.URL)
	}
}

func TestAdminEndpointsAllowLoopbackAndRedactSecret(t *testing.T) {
	cfg := config.DefaultAppConfig()
	now := time.Now().UTC()
	currentBinary, realBinary := seedStartupPlanBinaries(t)
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:         "main",
		Name:       "Main",
		AppID:      "cli_xxx",
		AppSecret:  "secret_xxx",
		VerifiedAt: &now,
	}}
	cfg.Wrapper.CodexRealBinary = realBinary
	cfg.Admin.Onboarding.AutostartDecision = &config.OnboardingDecision{
		Value:     onboardingDecisionDeferred,
		DecidedAt: &now,
	}
	cfg.Admin.Onboarding.VSCodeDecision = &config.OnboardingDecision{
		Value:     onboardingDecisionVSCodeRemoteOnly,
		DecidedAt: &now,
	}
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        services,
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	app.headlessRuntime.BinaryPath = currentBinary

	req := httptest.NewRequest(http.MethodGet, "/api/admin/bootstrap-state", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap state status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var bootstrap bootstrapStatePayload
	if err := json.NewDecoder(rec.Body).Decode(&bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if bootstrap.SetupRequired {
		t.Fatal("did not expect setup required")
	}
	if bootstrap.Admin.URL != "http://localhost:9501/admin/" {
		t.Fatalf("admin url = %q, want localhost /admin/", bootstrap.Admin.URL)
	}
	if !bootstrap.Session.TrustedLoopback {
		t.Fatal("expected trusted loopback session")
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/config", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("config status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret_xxx") {
		t.Fatalf("config body leaked secret: %s", rec.Body.String())
	}

	var response adminConfigResponse
	if err := json.NewDecoder(strings.NewReader(rec.Body.String())).Decode(&response); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if len(response.Config.Feishu.Apps) != 1 || !response.Config.Feishu.Apps[0].HasSecret {
		t.Fatalf("unexpected redacted config: %#v", response.Config.Feishu.Apps)
	}
}

func TestAdminBootstrapStateTreatsLegacyVerifiedConfigAsReady(t *testing.T) {
	cfg := config.DefaultAppConfig()
	now := time.Now().UTC()
	currentBinary, realBinary := seedStartupPlanBinaries(t)
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:         "main",
		Name:       "Main",
		AppID:      "cli_xxx",
		AppSecret:  "secret_xxx",
		VerifiedAt: &now,
	}}
	cfg.Wrapper.CodexRealBinary = realBinary
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        services,
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	app.headlessRuntime.BinaryPath = currentBinary

	req := httptest.NewRequest(http.MethodGet, "/api/admin/bootstrap-state", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bootstrap state status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var bootstrap bootstrapStatePayload
	if err := json.NewDecoder(rec.Body).Decode(&bootstrap); err != nil {
		t.Fatalf("decode bootstrap: %v", err)
	}
	if bootstrap.SetupRequired {
		t.Fatalf("expected legacy verified config to remain ready, got %#v", bootstrap)
	}
}

func TestAdminAndSetupRoutesRejectUnauthorizedRemoteRequests(t *testing.T) {
	cfg := config.DefaultAppConfig()
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        services,
		AdminListenHost: "0.0.0.0",
		AdminListenPort: "9501",
		AdminURL:        "http://10.0.0.8:9501/admin/",
		SetupURL:        "http://10.0.0.8:9501/setup",
		SSHSession:      true,
		SetupRequired:   true,
	})

	for _, path := range []string{"/api/setup/bootstrap-state", "/api/admin/runtime-status", "/v1/status"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "198.51.100.20:23456"
		rec := httptest.NewRecorder()
		app.apiServer.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s status = %d, want 401 body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminSkeletonReturnsStructuredNotImplemented(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	services := config.ServicesConfig{
		RelayHost:    "127.0.0.1",
		RelayPort:    "9500",
		RelayAPIHost: "127.0.0.1",
		RelayAPIPort: "9501",
	}
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        services,
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})

	req := httptest.NewRequest(http.MethodPut, "/api/admin/config", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 body=%s", rec.Code, rec.Body.String())
	}

	var payload apiErrorPayload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload.Error.Code != "not_implemented" {
		t.Fatalf("error code = %q, want not_implemented", payload.Error.Code)
	}
}

func TestRuntimeStatusPayloadOmitsSurfaceProgressSummaries(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-1",
			DisplayName:   "Demo Workspace",
			WorkspaceRoot: "/tmp/demo",
			WorkspaceKey:  "/tmp/demo",
			Source:        "vscode",
		},
	})
	inst := app.service.Instance("inst-1")
	if inst == nil {
		t.Fatal("expected instance after hello")
	}
	inst.Threads["thread-1"] = &state.ThreadRecord{
		ThreadID:         "thread-1",
		Name:             "整理 websetup 流程",
		FirstUserMessage: "请把 websetup 的体验重新整理一下",
		LastUserMessage:  "顺便把探索过程卡也接到 web 里",
		Loaded:           true,
		LastUsedAt:       time.Date(2026, 4, 16, 8, 15, 0, 0, time.UTC),
	}
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 {
		t.Fatalf("expected one surface, got %d", len(surfaces))
	}
	surface := surfaces[0]
	surface.AttachedInstanceID = "inst-1"
	surface.SelectedThreadID = "thread-1"
	surface.LastInboundAt = time.Date(2026, 4, 16, 8, 20, 0, 0, time.UTC)
	surface.ActiveExecProgress = &state.ExecCommandProgressRecord{
		InstanceID: "inst-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		Status:     "running",
		Exploration: &state.ExecCommandProgressExplorationRecord{
			Block: state.ExecCommandProgressBlockRecord{
				BlockID: "exploration",
				Kind:    "exploration",
				Status:  "running",
				Rows: []state.ExecCommandProgressBlockRowRecord{
					{RowID: "read", Kind: "read", Items: []string{"docs/README.md", "web/src/routes/AdminRoute.tsx"}},
				},
			},
			ActiveItemIDs: map[string]bool{"item-1": true},
		},
	}

	payload := app.runtimeStatusPayload()
	if len(payload.SurfaceStatuses) != 1 {
		t.Fatalf("expected one surface summary, got %#v", payload.SurfaceStatuses)
	}
	summary := payload.SurfaceStatuses[0]
	if summary.DisplayTitle != summary.ThreadTitle {
		t.Fatalf("display title = %q, want %q", summary.DisplayTitle, summary.ThreadTitle)
	}
	if summary.FirstUserMessage != "请把 websetup 的体验重新整理一下" {
		t.Fatalf("unexpected first user message: %#v", summary)
	}
	if summary.LastUserMessage != "顺便把探索过程卡也接到 web 里" {
		t.Fatalf("unexpected last user message: %#v", summary)
	}
	if summary.InstanceDisplayName != "Demo Workspace" || !testutil.SamePath(summary.WorkspacePath, "/tmp/demo") {
		t.Fatalf("unexpected instance summary: %#v", summary)
	}
}
