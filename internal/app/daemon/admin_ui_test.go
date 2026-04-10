package daemon

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestAdminPageServesEmbeddedShell(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app := newAdminUITestApp(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="root"`) {
		t.Fatalf("expected embedded shell root, body=%s", body)
	}
	if strings.Contains(body, "管理页骨架已就位") {
		t.Fatalf("expected placeholder page to be replaced, body=%s", body)
	}
}

func TestSetupPageServesEmbeddedShell(t *testing.T) {
	app := newAdminUITestApp(config.DefaultAppConfig())
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: config.DefaultAppConfig()}, nil
		},
		Services: config.ServicesConfig{
			RelayHost:    "127.0.0.1",
			RelayPort:    "9500",
			RelayAPIHost: "127.0.0.1",
			RelayAPIPort: "9501",
		},
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/",
		SetupURL:        "http://localhost:9501/setup",
		SetupRequired:   true,
	})

	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="root"`) {
		t.Fatalf("expected embedded shell root, body=%s", body)
	}
	if strings.Contains(body, "setup token、状态接口和认证链路已经接通") {
		t.Fatalf("expected placeholder page to be replaced, body=%s", body)
	}
}

func TestAdminUIAssetRouteServesBuiltBundle(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{
		ID:        "main",
		Name:      "Main",
		AppID:     "cli_xxx",
		AppSecret: "secret_xxx",
	}}
	app := newAdminUITestApp(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), `src="/assets/`) || strings.Contains(rec.Body.String(), `href="/assets/`) {
		t.Fatalf("expected embedded shell to avoid absolute asset paths, body=%s", rec.Body.String())
	}

	re := regexp.MustCompile(`(?:\./)?assets/[^"]+\.js`)
	match := re.FindString(rec.Body.String())
	if match == "" {
		t.Fatalf("expected js asset path in shell, body=%s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(match, "./"), nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if contentType := rec.Header().Get("Content-Type"); !strings.Contains(contentType, "javascript") && !strings.Contains(contentType, "text/plain") {
		t.Fatalf("unexpected asset content-type: %s", contentType)
	}
	if !strings.Contains(rec.Body.String(), "Codex Remote") {
		t.Fatalf("expected built bundle body, got %s", rec.Body.String())
	}
}

func newAdminUITestApp(cfg config.AppConfig) *App {
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
	return app
}
