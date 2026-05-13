package desktopsession

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/install"
	"github.com/kxn/codex-remote-feishu/internal/config"
)

func TestQueryStatusPreservesHealthyCarrierState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != statusAPIPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Status{
			State:         StateBackendOnly,
			BackendPID:    4321,
			SetupRequired: false,
		})
	}))
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "desktop-session.json")
	if err := WriteStatusFile(statePath, Status{
		State:         StateHealthy,
		InstanceID:    "stable",
		SetupRequired: true,
	}); err != nil {
		t.Fatalf("WriteStatusFile: %v", err)
	}

	originalResolve := resolveTargetFunc
	resolveTargetFunc = func(ResolveOptions) (Target, error) {
		return Target{
			InstanceID:       "stable",
			SessionStatePath: statePath,
			AdminURL:         server.URL,
		}, nil
	}
	defer func() { resolveTargetFunc = originalResolve }()

	status, err := QueryStatus(context.Background(), ResolveOptions{})
	if err != nil {
		t.Fatalf("QueryStatus: %v", err)
	}
	if status.State != StateHealthy {
		t.Fatalf("state = %q, want %q", status.State, StateHealthy)
	}
	if status.BackendPID != 4321 {
		t.Fatalf("backend pid = %d, want 4321", status.BackendPID)
	}
	if status.SetupRequired {
		t.Fatalf("setupRequired = true, want false")
	}
}

func TestEnsureBackendRefreshesLiveStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != statusAPIPath {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(Status{
			State:         StateBackendOnly,
			BackendPID:    4321,
			InstanceID:    "stable",
			SetupRequired: false,
		})
	}))
	defer server.Close()

	originalResolve := resolveTargetFunc
	originalEnsure := ensureDaemonReadyFromStatePathFunc
	resolveTargetFunc = func(ResolveOptions) (Target, error) {
		return Target{
			InstanceID:       "stable",
			StatePath:        "/tmp/install-state.json",
			SessionStatePath: filepath.Join(t.TempDir(), "desktop-session.json"),
			AdminURL:         server.URL,
		}, nil
	}
	ensureDaemonReadyFromStatePathFunc = func(context.Context, string, string) (install.DaemonReadyStatus, error) {
		return install.DaemonReadyStatus{
			AdminURL:      server.URL,
			SetupRequired: true,
		}, nil
	}
	defer func() {
		resolveTargetFunc = originalResolve
		ensureDaemonReadyFromStatePathFunc = originalEnsure
	}()

	status, err := EnsureBackend(context.Background(), EnsureOptions{Version: "1.0.0"})
	if err != nil {
		t.Fatalf("EnsureBackend: %v", err)
	}
	if status.SetupRequired {
		t.Fatalf("setupRequired = true, want false")
	}
	if status.BackendPID != 4321 {
		t.Fatalf("backend pid = %d, want 4321", status.BackendPID)
	}
	if status.InstanceID != "stable" {
		t.Fatalf("instanceID = %q, want stable", status.InstanceID)
	}
}

func TestResolveTargetPreservesExplicitStatePath(t *testing.T) {
	root := t.TempDir()
	baseDir := filepath.Join(root, "base")
	configPath := filepath.Join(root, "config", "config.json")
	statePath := filepath.Join(root, "custom", "install-state-copy.json")

	cfg := config.DefaultAppConfig()
	cfg.Admin.ListenHost = "127.0.0.1"
	cfg.Admin.ListenPort = 9601
	if err := config.WriteAppConfig(configPath, cfg); err != nil {
		t.Fatalf("WriteAppConfig: %v", err)
	}
	state := install.InstallState{
		InstanceID: "stable",
		BaseDir:    baseDir,
		ConfigPath: configPath,
		StatePath:  statePath,
	}
	install.ApplyStateMetadata(&state, install.StateMetadataOptions{
		InstanceID: state.InstanceID,
		BaseDir:    state.BaseDir,
		StatePath:  state.StatePath,
	})
	if err := install.WriteState(statePath, state); err != nil {
		t.Fatalf("WriteState: %v", err)
	}

	target, err := ResolveTarget(ResolveOptions{StatePath: statePath})
	if err != nil {
		t.Fatalf("ResolveTarget: %v", err)
	}
	if target.StatePath != statePath {
		t.Fatalf("StatePath = %q, want %q", target.StatePath, statePath)
	}
	if target.ResolverSource != "explicit_state_path" {
		t.Fatalf("ResolverSource = %q, want explicit_state_path", target.ResolverSource)
	}
	if target.AdminURL != "http://127.0.0.1:9601" {
		t.Fatalf("AdminURL = %q, want http://127.0.0.1:9601", target.AdminURL)
	}
}
