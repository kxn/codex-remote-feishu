package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestHandleGitWorkspaceImportCommandLockedMapsImportErrorsToUserNotice(t *testing.T) {
	original := runGitWorkspaceImport
	defer func() { runGitWorkspaceImport = original }()
	runGitWorkspaceImport = func(context.Context, gitworkspace.ImportRequest) (gitworkspace.ImportResult, error) {
		return gitworkspace.ImportResult{}, &gitworkspace.ImportError{
			Code:    gitworkspace.ImportErrorAuthFailed,
			Message: "git authentication failed",
		}
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.mu.Lock()
	events := app.handleGitWorkspaceImportCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceImport,
		SurfaceSessionID: "surface-1",
		PickerID:         "picker-1",
		LocalPath:        t.TempDir(),
		RepoURL:          "https://github.com/kxn/private.git",
	})
	app.mu.Unlock()

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != string(gitworkspace.ImportErrorAuthFailed) {
		t.Fatalf("expected auth failure notice, got %#v", events)
	}
	if got := events[0].Notice.Text; got == "" || got != "无法访问这个仓库，请确认当前机器上的 Git 凭据或仓库权限后重试。" {
		t.Fatalf("unexpected auth failure notice text: %#v", events[0].Notice)
	}
}

func TestHandleGitWorkspaceImportCommandLockedCompletesTargetPickerRoute(t *testing.T) {
	original := runGitWorkspaceImport
	defer func() { runGitWorkspaceImport = original }()

	workspaceRoot := t.TempDir()
	runGitWorkspaceImport = func(_ context.Context, req gitworkspace.ImportRequest) (gitworkspace.ImportResult, error) {
		if req.RepoURL != "https://github.com/kxn/codex-remote-feishu.git" {
			t.Fatalf("unexpected repo url: %#v", req)
		}
		return gitworkspace.ImportResult{WorkspacePath: workspaceRoot, DirectoryName: "codex-remote-feishu"}, nil
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) == 0 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected target picker event, got %#v", events)
	}
	pickerID := events[0].FeishuTargetPickerView.PickerID

	app.mu.Lock()
	commandEvents := app.handleGitWorkspaceImportCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceImport,
		SurfaceSessionID: "surface-1",
		PickerID:         pickerID,
		LocalPath:        t.TempDir(),
		RepoURL:          "https://github.com/kxn/codex-remote-feishu.git",
	})
	app.mu.Unlock()

	var surface *state.SurfaceConsoleRecord
	for _, candidate := range app.service.Surfaces() {
		if candidate != nil && candidate.SurfaceSessionID == "surface-1" {
			surface = candidate
			break
		}
	}
	if surface == nil {
		t.Fatal("expected surface to exist after git import completion")
	}
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected git import success to enter new-thread-ready, got %#v", surface)
	}
	if surface.ActiveTargetPicker != nil {
		t.Fatalf("expected successful git import to clear active target picker, got %#v", surface.ActiveTargetPicker)
	}
	var sawReady bool
	for _, event := range commandEvents {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread-ready notice after git import success, got %#v", commandEvents)
	}
}
