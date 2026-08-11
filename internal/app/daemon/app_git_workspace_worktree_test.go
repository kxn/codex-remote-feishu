package daemon

import (
	"context"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/gitworkspace"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestHandleGitWorkspaceWorktreeCreateCommandLockedMapsErrorsToUserNotice(t *testing.T) {
	original := runGitWorkspaceWorktreeCreate
	defer func() { runGitWorkspaceWorktreeCreate = original }()
	runGitWorkspaceWorktreeCreate = func(context.Context, gitworkspace.WorktreeRequest) (gitworkspace.WorktreeResult, error) {
		return gitworkspace.WorktreeResult{}, &gitmeta.WorktreeCreateError{
			Code:    gitmeta.WorktreeCreateErrorBranchExists,
			Message: "branch already exists",
		}
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.mu.Lock()
	events := app.handleGitWorkspaceWorktreeCreateCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
		SurfaceSessionID: "surface-1",
		PickerID:         "picker-1",
		WorkspaceKey:     t.TempDir(),
		BranchName:       "feat/login",
	})
	app.mu.Unlock()

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != string(gitmeta.WorktreeCreateErrorBranchExists) {
		t.Fatalf("expected branch-exists notice, got %#v", events)
	}
	if got := events[0].Notice.Text; got == "" || got != "这个分支已经存在，请换一个新的分支名后重试。" {
		t.Fatalf("unexpected worktree failure notice text: %#v", events[0].Notice)
	}
}

func TestHandleGitWorkspaceWorktreeCreateCommandLockedCompletesTargetPickerRoute(t *testing.T) {
	original := runGitWorkspaceWorktreeCreate
	defer func() { runGitWorkspaceWorktreeCreate = original }()

	baseWorkspaceRoot := t.TempDir()
	createdWorkspaceRoot := t.TempDir()
	runGitWorkspaceWorktreeCreate = func(_ context.Context, req gitworkspace.WorktreeRequest) (gitworkspace.WorktreeResult, error) {
		if !testutil.SamePath(req.BaseWorkspacePath, baseWorkspaceRoot) || req.BranchName != "feat/login" {
			t.Fatalf("unexpected worktree request: %#v", req)
		}
		return gitworkspace.WorktreeResult{WorkspacePath: createdWorkspaceRoot, BranchName: "feat/login", DirectoryName: "repo-login"}, nil
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-base",
		DisplayName:   "base",
		WorkspaceRoot: baseWorkspaceRoot,
		WorkspaceKey:  baseWorkspaceRoot,
		ShortName:     "base",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	app.service.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-created",
		DisplayName:   "created",
		WorkspaceRoot: createdWorkspaceRoot,
		WorkspaceKey:  createdWorkspaceRoot,
		ShortName:     "created",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	events := app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewWorktree,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) == 0 || events[0].TargetPickerView == nil {
		t.Fatalf("expected target picker event, got %#v", events)
	}
	pickerID := events[0].TargetPickerView.PickerID

	app.mu.Lock()
	commandEvents := app.handleGitWorkspaceWorktreeCreateCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
		SurfaceSessionID: "surface-1",
		PickerID:         pickerID,
		WorkspaceKey:     baseWorkspaceRoot,
		BranchName:       "feat/login",
		DirectoryName:    "repo-login",
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
		t.Fatal("expected surface to exist after worktree completion")
	}
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, createdWorkspaceRoot) {
		t.Fatalf("expected worktree success to enter new-thread-ready, got %#v", surface)
	}
	if runtime := app.service.SurfaceUIRuntime("surface-1"); runtime.ActiveTargetPickerID != "" {
		t.Fatalf("expected successful worktree create to clear active target picker, got %#v", runtime)
	}
	if len(commandEvents) != 1 || commandEvents[0].TargetPickerView == nil {
		t.Fatalf("expected same-card success after worktree success, got %#v", commandEvents)
	}
	if got := commandEvents[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded worktree terminal card, got %#v", got)
	}
}

func TestHandleGitWorkspaceWorktreeCreateCommandLockedProtocolNotices(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.mu.Lock()
	defer app.mu.Unlock()

	cases := []struct {
		name     string
		command  control.DaemonCommand
		wantCode string
		wantText string
	}{
		{
			name: "missing workspace key",
			command: control.DaemonCommand{
				Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
				SurfaceSessionID: "surface-1",
				PickerID:         "picker-1",
				BranchName:       "feat/login",
			},
			wantCode: "worktree_create_invalid_base_workspace",
			wantText: "基准工作区无效，请重新开始创建流程。",
		},
		{
			name: "missing branch name",
			command: control.DaemonCommand{
				Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
				SurfaceSessionID: "surface-1",
				PickerID:         "picker-1",
				WorkspaceKey:     t.TempDir(),
			},
			wantCode: "worktree_create_invalid_branch_name",
			wantText: "新分支名无效，请重新开始创建流程。",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			events := app.handleGitWorkspaceWorktreeCreateCommandLocked(tc.command)
			if len(events) != 1 || events[0].Notice == nil {
				t.Fatalf("expected protocol notice, got %#v", events)
			}
			if events[0].Notice.Code != tc.wantCode || events[0].Notice.Text != tc.wantText {
				t.Fatalf("unexpected protocol notice: %#v", events[0].Notice)
			}
		})
	}
}

func TestHandleGitWorkspaceWorktreeCreateCommandLockedMapsBusinessErrors(t *testing.T) {
	original := runGitWorkspaceWorktreeCreate
	defer func() { runGitWorkspaceWorktreeCreate = original }()

	cases := []struct {
		name     string
		code     gitmeta.WorktreeCreateErrorCode
		wantText string
	}{
		{"branch exists", gitmeta.WorktreeCreateErrorBranchExists, "这个分支已经存在，请换一个新的分支名后重试。"},
		{"invalid directory", gitmeta.WorktreeCreateErrorInvalidDirectoryName, "本地目录名无效，请改成不含路径分隔符的普通目录名。"},
		{"destination exists", gitmeta.WorktreeCreateErrorDestinationExists, "目标目录已经存在，请换一个目录名或基准工作区后重试。"},
		{"non git workspace", gitmeta.WorktreeCreateErrorBaseWorkspaceNotGit, "当前选择的工作区不是 Git 工作区，不能从它创建 worktree。"},
		{"git missing", gitmeta.WorktreeCreateErrorGitMissing, "当前机器未检测到 `git`，暂时不能创建 worktree 工作区。"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runGitWorkspaceWorktreeCreate = func(context.Context, gitworkspace.WorktreeRequest) (gitworkspace.WorktreeResult, error) {
				return gitworkspace.WorktreeResult{}, &gitmeta.WorktreeCreateError{Code: tc.code}
			}
			app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
			app.mu.Lock()
			events := app.handleGitWorkspaceWorktreeCreateCommandLocked(control.DaemonCommand{
				Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
				SurfaceSessionID: "surface-1",
				PickerID:         "picker-1",
				WorkspaceKey:     t.TempDir(),
				BranchName:       "feat/login",
			})
			app.mu.Unlock()
			if len(events) != 1 || events[0].Notice == nil {
				t.Fatalf("expected business error notice, got %#v", events)
			}
			if events[0].Notice.Code != string(tc.code) || events[0].Notice.Text != tc.wantText {
				t.Fatalf("unexpected business notice: %#v", events[0].Notice)
			}
		})
	}
}

func TestHandleGitWorkspaceWorktreeCreateCommandLockedSuccessWithMissingSurface(t *testing.T) {
	original := runGitWorkspaceWorktreeCreate
	defer func() { runGitWorkspaceWorktreeCreate = original }()
	createdWorkspaceRoot := t.TempDir()
	runGitWorkspaceWorktreeCreate = func(_ context.Context, req gitworkspace.WorktreeRequest) (gitworkspace.WorktreeResult, error) {
		return gitworkspace.WorktreeResult{WorkspacePath: createdWorkspaceRoot, BranchName: "feat/login", DirectoryName: "repo-login"}, nil
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.mu.Lock()
	events := app.handleGitWorkspaceWorktreeCreateCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
		SurfaceSessionID: "surface-ghost",
		PickerID:         "picker-1",
		WorkspaceKey:     t.TempDir(),
		BranchName:       "feat/login",
	})
	app.mu.Unlock()

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "worktree_create_completed" {
		t.Fatalf("expected completed notice for ghost surface, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, createdWorkspaceRoot) {
		t.Fatalf("expected completed notice to carry created path, got %#v", events[0].Notice)
	}
}

func TestHandleGitWorkspaceWorktreeCreateCommandLockedSuccessWithStaleFlow(t *testing.T) {
	original := runGitWorkspaceWorktreeCreate
	defer func() { runGitWorkspaceWorktreeCreate = original }()
	createdWorkspaceRoot := t.TempDir()
	runGitWorkspaceWorktreeCreate = func(_ context.Context, req gitworkspace.WorktreeRequest) (gitworkspace.WorktreeResult, error) {
		return gitworkspace.WorktreeResult{WorkspacePath: createdWorkspaceRoot, BranchName: "feat/login", DirectoryName: "repo-login"}, nil
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	app.service.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewWorktree,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	app.mu.Lock()
	events := app.handleGitWorkspaceWorktreeCreateCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
		SurfaceSessionID: "surface-1",
		PickerID:         "picker-stale",
		WorkspaceKey:     t.TempDir(),
		BranchName:       "feat/login",
	})
	app.mu.Unlock()

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "worktree_create_flow_stale" {
		t.Fatalf("expected flow-stale notice after attach failure, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "目录会保留") {
		t.Fatalf("expected flow-stale notice to mention directory kept, got %#v", events[0].Notice)
	}
}

func TestHandleGitWorkspaceWorktreeCancelCommandLockedSuppressesCancelledFollowup(t *testing.T) {
	original := runGitWorkspaceWorktreeCreate
	defer func() { runGitWorkspaceWorktreeCreate = original }()

	started := make(chan struct{})
	done := make(chan []eventcontract.Event, 1)
	runGitWorkspaceWorktreeCreate = func(ctx context.Context, _ gitworkspace.WorktreeRequest) (gitworkspace.WorktreeResult, error) {
		close(started)
		<-ctx.Done()
		return gitworkspace.WorktreeResult{}, ctx.Err()
	}

	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	command := control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceWorktreeCreate,
		SurfaceSessionID: "surface-1",
		PickerID:         "picker-1",
		WorkspaceKey:     t.TempDir(),
		BranchName:       "feat/login",
	}
	go func() {
		app.mu.Lock()
		done <- app.handleGitWorkspaceWorktreeCreateCommandLocked(command)
		app.mu.Unlock()
	}()
	<-started

	app.mu.Lock()
	cancelEvents := app.handleGitWorkspaceWorktreeCancelCommandLocked(control.DaemonCommand{
		Kind:             control.DaemonCommandGitWorkspaceWorktreeCancel,
		SurfaceSessionID: "surface-1",
		PickerID:         "picker-1",
	})
	app.mu.Unlock()

	if len(cancelEvents) != 0 {
		t.Fatalf("expected cancel command to stay silent, got %#v", cancelEvents)
	}
	if events := <-done; len(events) != 0 {
		t.Fatalf("expected cancelled worktree create to suppress followup events, got %#v", events)
	}
}
