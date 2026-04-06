package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
		AdminURL:        "http://10.0.0.8:9501/",
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
}

func TestAdminEndpointsAllowLoopbackAndRedactSecret(t *testing.T) {
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
		AdminURL:        "http://localhost:9501/",
		SetupURL:        "http://localhost:9501/setup",
	})

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
		AdminURL:        "http://10.0.0.8:9501/",
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
		AdminURL:        "http://localhost:9501/",
		SetupURL:        "http://localhost:9501/setup",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/admin/storage/image-staging", nil)
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
