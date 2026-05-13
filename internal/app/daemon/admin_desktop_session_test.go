package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/desktopsession"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestDesktopSessionStatusEndpointReturnsConfiguredRuntime(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureDesktopSession(DesktopSessionRuntimeOptions{
		StatePath:     filepath.Join(t.TempDir(), "desktop-session.json"),
		InstanceID:    "stable",
		BackendPID:    4321,
		AdminURL:      "http://localhost:9501/admin/",
		SetupURL:      "http://localhost:9501/setup",
		SetupRequired: true,
	})

	rec := performAdminRequest(t, app, "GET", "/api/admin/desktop-session/status", "")
	if rec.Code != 200 {
		t.Fatalf("status code = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var payload desktopsession.Status
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if payload.State != desktopsession.StateBackendOnly {
		t.Fatalf("state = %q, want %q", payload.State, desktopsession.StateBackendOnly)
	}
	if payload.BackendPID != 4321 || payload.InstanceID != "stable" {
		t.Fatalf("payload identity = %#v", payload)
	}
	if payload.AdminURL != "http://localhost:9501/admin/" || payload.SetupURL != "http://localhost:9501/setup" || !payload.SetupRequired {
		t.Fatalf("payload urls = %#v", payload)
	}
}

func TestDesktopSessionQuitEndpointMarksQuittingAndRequestsSelfShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "desktop-session.json")
	triggered := make(chan struct{}, 1)

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureDesktopSession(DesktopSessionRuntimeOptions{
		StatePath:  path,
		InstanceID: "stable",
		BackendPID: 4321,
		AdminURL:   "http://localhost:9501/admin/",
		RequestSelfShutdown: func() error {
			triggered <- struct{}{}
			return nil
		},
	})

	rec := performAdminRequest(t, app, "POST", "/api/admin/desktop-session/quit", "")
	if rec.Code != 202 {
		t.Fatalf("quit code = %d, want 202 body=%s", rec.Code, rec.Body.String())
	}

	var payload desktopsession.Status
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode quit payload: %v", err)
	}
	if payload.State != desktopsession.StateQuitting {
		t.Fatalf("state = %q, want %q", payload.State, desktopsession.StateQuitting)
	}

	stored, ok, err := desktopsession.ReadStatusFile(path)
	if err != nil {
		t.Fatalf("ReadStatusFile: %v", err)
	}
	if !ok {
		t.Fatal("expected desktop session state file to be written")
	}
	if stored.State != desktopsession.StateQuitting {
		t.Fatalf("stored state = %q, want %q", stored.State, desktopsession.StateQuitting)
	}

	select {
	case <-triggered:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timed out waiting for self-shutdown trigger")
	}
}

func TestDesktopSessionStatusEndpointUsesCurrentSetupState(t *testing.T) {
	cfg := config.DefaultAppConfig()

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services: config.ServicesConfig{
			RelayHost:    "127.0.0.1",
			RelayPort:    "9500",
			RelayAPIHost: "127.0.0.1",
			RelayAPIPort: "9501",
		},
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	app.ConfigureDesktopSession(DesktopSessionRuntimeOptions{
		StatePath:     filepath.Join(t.TempDir(), "desktop-session.json"),
		InstanceID:    "stable",
		BackendPID:    4321,
		AdminURL:      "http://localhost:9501/admin/",
		SetupURL:      "http://localhost:9501/setup",
		SetupRequired: false,
	})

	rec := performAdminRequest(t, app, "GET", "/api/admin/desktop-session/status", "")
	if rec.Code != 200 {
		t.Fatalf("status code = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var payload desktopsession.Status
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !payload.SetupRequired {
		t.Fatalf("setupRequired = false, want true payload=%#v", payload)
	}
}
