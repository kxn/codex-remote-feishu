package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestAdminInstanceCreateRemovedFromWebAdmin(t *testing.T) {
	workspaceRoot := t.TempDir()
	app := newManagedInstancesAdminTestApp(t)

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/instances", `{"workspaceRoot":"`+workspaceRoot+`","displayName":"Alpha"}`)
	if rec.Code != http.StatusGone {
		t.Fatalf("create status = %d, want 410 body=%s", rec.Code, rec.Body.String())
	}

	var payload apiErrorPayload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create error: %v", err)
	}
	if payload.Error.Code != "managed_instance_admin_removed" {
		t.Fatalf("unexpected create error payload: %#v", payload)
	}
	if captured.InstanceID != "" || captured.WorkDir != "" {
		t.Fatalf("expected removed endpoint not to launch managed instance, got %#v", captured)
	}
}

func TestAdminInstanceDeleteRemovedFromWebAdmin(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	stoppedPID := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		stoppedPID = pid
		return nil
	}

	rec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/instances/inst-headless-1", "")
	if rec.Code != http.StatusGone {
		t.Fatalf("delete status = %d, want 410 body=%s", rec.Code, rec.Body.String())
	}

	var payload apiErrorPayload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode delete error: %v", err)
	}
	if payload.Error.Code != "managed_instance_admin_removed" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if stoppedPID != 0 {
		t.Fatalf("expected removed endpoint not to stop processes, got %d", stoppedPID)
	}
}

func TestAdminInstancesListHidesManagedHeadlessOfflineEntry(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	workspaceRoot := t.TempDir()

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-offline",
			DisplayName:   "Offline Headless",
			WorkspaceRoot: workspaceRoot,
			WorkspaceKey:  workspaceRoot,
			ShortName:     "offline-headless",
			Source:        "headless",
			Managed:       true,
			PID:           2468,
		},
	})
	app.onDisconnect(context.Background(), "inst-headless-offline")

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/instances", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var listed adminInstancesResponse
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Instances) != 0 {
		t.Fatalf("expected managed headless to be hidden from admin list, got %#v", listed.Instances)
	}
}

func TestAdminInstancesListHidesManagedHeadlessBusyEntry(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	workspaceRoot := t.TempDir()

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    "inst-headless-busy",
			DisplayName:   "Busy Headless",
			WorkspaceRoot: workspaceRoot,
			WorkspaceKey:  workspaceRoot,
			ShortName:     "busy-headless",
			Source:        "headless",
			Managed:       true,
			PID:           2469,
		},
	})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-headless-busy",
	})

	rec := performAdminRequest(t, app, http.MethodGet, "/api/admin/instances", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}

	var listed adminInstancesResponse
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Instances) != 0 {
		t.Fatalf("expected attached managed headless to stay hidden from admin list, got %#v", listed.Instances)
	}
}

func TestAdminInstancesSnapshotFiltersManagedHeadlessButKeepsVSCode(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	workspaceRoot := testutil.WorkspacePath("data", "dl", "droid")
	headlessRoot := testutil.WorkspacePath("tmp", "headless")
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: workspaceRoot, Loaded: true},
		},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: headlessRoot,
		WorkspaceKey:  headlessRoot,
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	now := time.Now().UTC()
	app.managedHeadlessRuntime.Processes["inst-headless-1"] = &managedHeadlessProcess{
		InstanceID:    "inst-headless-1",
		PID:           4321,
		RequestedAt:   now,
		StartedAt:     now,
		WorkspaceRoot: headlessRoot,
		DisplayName:   "headless",
		Status:        managedHeadlessStatusIdle,
	}

	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})

	summaries := app.adminInstancesSnapshot()
	if len(summaries) != 1 {
		t.Fatalf("expected only vscode summary, got %#v", summaries)
	}
	if summaries[0].InstanceID != "inst-offline" || summaries[0].Source != "" || !testutil.SamePath(summaries[0].WorkspaceRoot, workspaceRoot) {
		t.Fatalf("unexpected vscode-only summary: %#v", summaries)
	}
}

func TestCreateManagedHeadlessInstanceSetsExplicitDaemonOwnedLifetime(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	workspaceRoot := t.TempDir()

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	summary, err := app.createManagedHeadlessInstance(workspaceRoot, "Alpha")
	if err != nil {
		t.Fatalf("createManagedHeadlessInstance: %v", err)
	}
	if summary.InstanceID == "" || captured.InstanceID != summary.InstanceID {
		t.Fatalf("expected launched instance id to match summary, got summary=%#v launch=%#v", summary, captured)
	}
	if !testutil.SamePath(captured.WorkDir, workspaceRoot) {
		t.Fatalf("expected managed headless workdir = %q, got %#v", workspaceRoot, captured)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_SOURCE=headless") ||
		!containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_MANAGED=1") ||
		!containsEnvEntry(captured.Env, "CODEX_REMOTE_LIFETIME=daemon-owned") {
		t.Fatalf("expected explicit daemon-owned managed headless env, got %#v", captured.Env)
	}
}

func newManagedInstancesAdminTestApp(t *testing.T) *App {
	t.Helper()

	cfg := config.DefaultAppConfig()
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.sendAgentCommand = func(string, agentproto.Command) error { return nil }
	app.SetHeadlessRuntime(HeadlessRuntimeConfig{
		BinaryPath: "/tmp/codex-remote",
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Paths: relayruntime.Paths{
			StateDir: t.TempDir(),
			LogsDir:  t.TempDir(),
		},
		KillGrace: time.Second,
	})
	app.ConfigureAdmin(AdminRuntimeOptions{
		LoadConfig: func() (config.LoadedAppConfig, error) {
			return config.LoadedAppConfig{Path: "/tmp/config.json", Config: cfg}, nil
		},
		Services:        defaultFeishuServices(),
		AdminListenHost: "127.0.0.1",
		AdminListenPort: "9501",
		AdminURL:        "http://localhost:9501/admin/",
		SetupURL:        "http://localhost:9501/setup",
	})
	return app
}
