package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestAdminInstancesCreateListAndDeleteManagedHeadless(t *testing.T) {
	workspaceRoot := t.TempDir()
	app := newManagedInstancesAdminTestApp(t)

	var captured relayruntime.HeadlessLaunchOptions
	app.startHeadless = func(opts relayruntime.HeadlessLaunchOptions) (int, error) {
		captured = opts
		return 4321, nil
	}

	rec := performAdminRequest(t, app, http.MethodPost, "/api/admin/instances", `{"workspaceRoot":"`+workspaceRoot+`","displayName":"Alpha"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201 body=%s", rec.Code, rec.Body.String())
	}

	var created struct {
		Instance adminInstanceSummary `json:"instance"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.Instance.InstanceID == "" || !strings.HasPrefix(created.Instance.InstanceID, "inst-headless-admin-") {
		t.Fatalf("unexpected created instance id: %#v", created.Instance)
	}
	if created.Instance.Status != "starting" || created.Instance.Online {
		t.Fatalf("unexpected created instance status: %#v", created.Instance)
	}
	if captured.InstanceID != created.Instance.InstanceID || captured.WorkDir != workspaceRoot {
		t.Fatalf("unexpected launch options: %#v", captured)
	}
	if !containsEnvEntry(captured.Env, "CODEX_REMOTE_INSTANCE_DISPLAY_NAME=Alpha") {
		t.Fatalf("expected display name env override, got %#v", captured.Env)
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/instances", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	var listed adminInstancesResponse
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Instances) != 1 || listed.Instances[0].Status != "starting" {
		t.Fatalf("unexpected list response before hello: %#v", listed.Instances)
	}

	app.onHello(context.Background(), agentproto.Hello{
		Instance: agentproto.InstanceHello{
			InstanceID:    created.Instance.InstanceID,
			DisplayName:   "Alpha",
			WorkspaceRoot: workspaceRoot,
			WorkspaceKey:  workspaceRoot,
			ShortName:     "alpha",
			Source:        "headless",
			Managed:       true,
			PID:           4321,
		},
	})

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/instances", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list after hello status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list after hello: %v", err)
	}
	if len(listed.Instances) != 1 || listed.Instances[0].Status != managedHeadlessStatusIdle || !listed.Instances[0].Online {
		t.Fatalf("unexpected list response after hello: %#v", listed.Instances)
	}

	stoppedPID := 0
	app.stopProcess = func(pid int, _ time.Duration) error {
		stoppedPID = pid
		return nil
	}

	rec = performAdminRequest(t, app, http.MethodDelete, "/api/admin/instances/"+created.Instance.InstanceID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204 body=%s", rec.Code, rec.Body.String())
	}
	if stoppedPID != 4321 {
		t.Fatalf("expected stopped pid 4321, got %d", stoppedPID)
	}

	rec = performAdminRequest(t, app, http.MethodGet, "/api/admin/instances", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list after delete status = %d, want 200 body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list after delete: %v", err)
	}
	if len(listed.Instances) != 0 {
		t.Fatalf("expected no instances after delete, got %#v", listed.Instances)
	}
}

func TestAdminInstanceDeleteRejectsNonManagedHeadless(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-vscode-1",
		DisplayName:   "VS Code",
		WorkspaceRoot: t.TempDir(),
		WorkspaceKey:  t.TempDir(),
		Source:        "vscode",
		PID:           2222,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	rec := performAdminRequest(t, app, http.MethodDelete, "/api/admin/instances/inst-vscode-1", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, want 409 body=%s", rec.Code, rec.Body.String())
	}

	var payload apiErrorPayload
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode delete error: %v", err)
	}
	if payload.Error.Code != "managed_instance_delete_forbidden" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
}

func TestAdminInstancesListMarksManagedHeadlessOfflineAfterDisconnect(t *testing.T) {
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
	if len(listed.Instances) != 1 || listed.Instances[0].Status != "offline" || listed.Instances[0].Online {
		t.Fatalf("unexpected offline list response: %#v", listed.Instances)
	}
}

func TestAdminInstancesListMarksManagedHeadlessBusyWhenAttached(t *testing.T) {
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
	if len(listed.Instances) != 1 || listed.Instances[0].Status != managedHeadlessStatusBusy || !listed.Instances[0].Online {
		t.Fatalf("unexpected busy list response: %#v", listed.Instances)
	}
}

func TestAdminInstancesSnapshotUsesRetargetedManagedWorkspaceRoot(t *testing.T) {
	app := newManagedInstancesAdminTestApp(t)
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-offline",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        false,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", Preview: "修登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-headless-1",
		DisplayName:   "headless",
		WorkspaceRoot: "/tmp/headless",
		WorkspaceKey:  "/tmp/headless",
		ShortName:     "headless",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	now := time.Now().UTC()
	app.managedHeadless["inst-headless-1"] = &managedHeadlessProcess{
		InstanceID:    "inst-headless-1",
		PID:           4321,
		RequestedAt:   now,
		StartedAt:     now,
		WorkspaceRoot: "/tmp/headless",
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
	if len(summaries) != 2 {
		t.Fatalf("expected two summaries, got %#v", summaries)
	}
	for _, summary := range summaries {
		if summary.InstanceID != "inst-headless-1" {
			continue
		}
		if summary.WorkspaceRoot != "/data/dl/droid" {
			t.Fatalf("expected retargeted workspace root in admin summary, got %#v", summary)
		}
		if summary.Status != managedHeadlessStatusBusy {
			t.Fatalf("expected reused headless to be busy while attached, got %#v", summary)
		}
		return
	}
	t.Fatalf("missing reused managed headless summary: %#v", summaries)
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
		AdminURL:        "http://localhost:9501/",
		SetupURL:        "http://localhost:9501/setup",
	})
	return app
}
