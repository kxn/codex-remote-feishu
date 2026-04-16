package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/externalaccess"
)

func TestAdminExternalAccessStatusAndLink(t *testing.T) {
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
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	app.SetExternalAccess(ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        0,
			DefaultLinkTTL:    10 * time.Second,
			DefaultSessionTTL: 30 * time.Second,
			ProviderKind:      "disabled",
		},
	})
	defer app.Shutdown(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/admin/external-access/status", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var status externalAccessStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Status.IdleTTL != 30*time.Minute {
		t.Fatalf("idle ttl = %v, want %v", status.Status.IdleTTL, 30*time.Minute)
	}

	body := map[string]any{
		"purpose":   "debug",
		"targetURL": "http://127.0.0.1:9501/admin/",
	}
	raw, _ := json.Marshal(body)
	req = httptest.NewRequest(http.MethodPost, "/api/admin/external-access/link", bytes.NewReader(raw))
	req.RemoteAddr = "127.0.0.1:12345"
	rec = httptest.NewRecorder()
	app.apiServer.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}
	var payload externalAccessLinkResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if !strings.Contains(payload.URL.ExternalURL, "/g/") {
		t.Fatalf("external url = %q, want /g/ path", payload.URL.ExternalURL)
	}
}

func TestExternalAccessIdleTimeoutShutsDownListener(t *testing.T) {
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
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://127.0.0.1:9501/admin/",
		SetupURL:        "http://127.0.0.1:9501/setup",
	})
	base := time.Date(2026, 4, 10, 20, 0, 0, 0, time.UTC)
	app.externalAccessRuntime = ExternalAccessRuntimeConfig{
		Settings: externalAccessSettingsView{
			ListenHost:        "127.0.0.1",
			ListenPort:        0,
			DefaultLinkTTL:    10 * time.Second,
			DefaultSessionTTL: 30 * time.Second,
			ProviderKind:      "disabled",
		},
	}
	app.externalAccess = externalaccess.NewService(externalaccess.Options{
		Now:               func() time.Time { return base },
		DefaultLinkTTL:    10 * time.Second,
		DefaultSessionTTL: 30 * time.Second,
		IdleTTL:           5 * time.Minute,
	})
	defer app.Shutdown(nil)

	issued, err := app.IssueExternalAccessURL(context.Background(), externalaccess.IssueRequest{
		Purpose:   externalaccess.PurposeDebug,
		TargetURL: "http://127.0.0.1:9501/admin/",
	})
	if err != nil {
		t.Fatalf("IssueExternalAccessURL: %v", err)
	}
	if app.externalAccessListener == nil {
		t.Fatal("expected listener to be started")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar.New: %v", err)
	}
	client := &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			Proxy: nil,
		},
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get(issued.ExternalURL)
	if err != nil {
		t.Fatalf("exchange request: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}

	app.onTick(context.Background(), base.Add(6*time.Minute))
	if app.externalAccessListener != nil {
		t.Fatal("expected idle timeout to stop listener")
	}
	if snapshot := app.externalAccess.Snapshot(); snapshot.ListenerActive {
		t.Fatalf("expected external access runtime inactive after idle timeout, got %#v", snapshot)
	}
}
