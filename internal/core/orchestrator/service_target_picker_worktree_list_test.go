package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestTargetPickerListShowsBusyGitWorkspaceAsWorktreeOnlyAction(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 50, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	busyGitWorkspace := createTargetPickerGitRepo(t)
	busyPlainWorkspace := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-busy-git",
		DisplayName:   "busy-git",
		WorkspaceRoot: busyGitWorkspace,
		WorkspaceKey:  busyGitWorkspace,
		ShortName:     "busy-git",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-busy-git": {ThreadID: "thread-busy-git", Name: "忙碌 Git 会话", CWD: busyGitWorkspace, LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-busy-plain",
		DisplayName:   "busy-plain",
		WorkspaceRoot: busyPlainWorkspace,
		WorkspaceKey:  busyPlainWorkspace,
		ShortName:     "busy-plain",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-busy-plain": {ThreadID: "thread-busy-plain", Name: "忙碌普通会话", CWD: busyPlainWorkspace, LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-busy-git", ChatID: "chat-busy-git", ActorUserID: "user-busy-git", WorkspaceKey: busyGitWorkspace})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachWorkspace, SurfaceSessionID: "surface-busy-plain", ChatID: "chat-busy-plain", ActorUserID: "user-busy-plain", WorkspaceKey: busyPlainWorkspace})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	if _, ok := targetPickerWorkspaceOption(view, busyGitWorkspace); !ok {
		t.Fatalf("expected busy Git workspace to stay visible as worktree base, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerWorkspaceOption(view, busyPlainWorkspace); ok {
		t.Fatalf("expected busy non-Git workspace to stay hidden, got %#v", view.WorkspaceOptions)
	}
	if !testutil.SamePath(view.SelectedWorkspaceKey, busyGitWorkspace) {
		t.Fatalf("expected busy Git workspace to be selected, got %#v", view)
	}
	if len(view.SessionOptions) != 1 {
		t.Fatalf("expected busy Git workspace to expose only worktree action, got %#v", view.SessionOptions)
	}
	wantWorktreeValue := "worktree_create"
	if option := view.SessionOptions[0]; option.Value != wantWorktreeValue || option.Kind != control.FeishuTargetPickerSessionKind(wantWorktreeValue) {
		t.Fatalf("expected only worktree action, got %#v", option)
	}
	if view.SelectedSessionValue != wantWorktreeValue || view.ConfirmLabel != "继续创建" || !view.CanConfirm {
		t.Fatalf("expected busy Git worktree action to be confirmable by default, got %#v", view)
	}
}

func TestTargetPickerListAttachableGitWorkspaceIncludesWorktreeAction(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 50, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	gitWorkspace := createTargetPickerGitRepo(t)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-git",
		DisplayName:   "git",
		WorkspaceRoot: gitWorkspace,
		WorkspaceKey:  gitWorkspace,
		ShortName:     "git",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-git": {ThreadID: "thread-git", Name: "已有 Git 会话", CWD: gitWorkspace, LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	if _, ok := targetPickerSessionOption(view, targetPickerNewThreadValue); !ok {
		t.Fatalf("expected attachable Git workspace to expose new-thread, got %#v", view.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-git")); !ok {
		t.Fatalf("expected attachable Git workspace to expose existing sessions, got %#v", view.SessionOptions)
	}
	wantWorktreeValue := "worktree_create"
	if option, ok := targetPickerSessionOption(view, wantWorktreeValue); !ok || option.Kind != control.FeishuTargetPickerSessionKind(wantWorktreeValue) {
		t.Fatalf("expected attachable Git workspace to expose worktree action, got %#v", view.SessionOptions)
	}
	if view.SelectedSessionValue != targetPickerNewThreadValue || view.ConfirmLabel != "新建会话" {
		t.Fatalf("expected attachable Git workspace to keep new-thread default, got %#v", view)
	}
}

func TestTargetPickerListWorktreeActionOpensWorktreeSubpageWithoutDaemonCommand(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 50, 45, 0, time.UTC)
	svc := newServiceForTest(&now)
	gitWorkspace := createTargetPickerGitRepo(t)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-git",
		DisplayName:   "git",
		WorkspaceRoot: gitWorkspace,
		WorkspaceKey:  gitWorkspace,
		ShortName:     "git",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	wantWorktreeValue := "worktree_create"
	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      gitWorkspace,
		TargetPickerValue: wantWorktreeValue,
	})

	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected same-card worktree subpage, got %#v", events)
	}
	next := events[0].TargetPickerView
	if next.Source != control.TargetPickerRequestSourceList || next.Page != control.FeishuTargetPickerPageWorktree {
		t.Fatalf("expected list picker to enter worktree subpage, got %#v", next)
	}
	if !testutil.SamePath(next.SelectedWorkspaceKey, gitWorkspace) {
		t.Fatalf("expected worktree subpage to preserve base workspace, got %#v", next)
	}
	if next.Stage != control.FeishuTargetPickerStageEditing || next.ConfirmLabel != "创建并进入" {
		t.Fatalf("expected worktree subpage to remain editing, got %#v", next)
	}
	for _, event := range events {
		if event.DaemonCommand != nil {
			t.Fatalf("worktree action from list must not create immediately, got %#v", events)
		}
	}
}

func TestTargetPickerListWorktreeSubpageBackReturnsToTargetPage(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 50, 50, 0, time.UTC)
	svc := newServiceForTest(&now)
	gitWorkspace := createTargetPickerGitRepo(t)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-git",
		DisplayName:   "git",
		WorkspaceRoot: gitWorkspace,
		WorkspaceKey:  gitWorkspace,
		ShortName:     "git",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-git": {ThreadID: "thread-git", Name: "已有 Git 会话", CWD: gitWorkspace, LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	worktree := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      gitWorkspace,
		TargetPickerValue: "worktree_create",
	}))
	if worktree.Page != control.FeishuTargetPickerPageWorktree || !worktree.CanGoBack {
		t.Fatalf("expected list-entered worktree subpage to expose internal back, got %#v", worktree)
	}

	back := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerBack,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	}))

	if back.Page != control.FeishuTargetPickerPageTarget || back.Source != control.TargetPickerRequestSourceList {
		t.Fatalf("expected back to return to original list target page, got %#v", back)
	}
	if !testutil.SamePath(back.SelectedWorkspaceKey, gitWorkspace) {
		t.Fatalf("expected back to preserve selected workspace, got %#v", back)
	}
	if back.SelectedSessionValue != targetPickerNewThreadValue || back.ConfirmLabel != "新建会话" {
		t.Fatalf("expected back to restore list default session, got %#v", back)
	}
}

func TestTargetPickerWorktreeConfirmPreflightsRoomWorkspaceBeforeDaemonCreate(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 50, 55, 0, time.UTC)
	svc := newServiceForTest(&now)
	currentWorkspace := t.TempDir()
	gitWorkspace := createTargetPickerGitRepo(t)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-current",
		DisplayName:   "current",
		WorkspaceRoot: currentWorkspace,
		WorkspaceKey:  currentWorkspace,
		ShortName:     "current",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-git",
		DisplayName:   "git",
		WorkspaceRoot: gitWorkspace,
		WorkspaceKey:  gitWorkspace,
		ShortName:     "git",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_owner",
		WorkspaceKey:     currentWorkspace,
	})
	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	room.PrimaryGatewayID = "app-2"

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_member",
	}))
	worktree := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "feishu:app-1:chat:oc_room",
		GatewayID:         "app-1",
		ChatID:            "oc_room",
		ActorUserID:       "ou_member",
		PickerID:          view.PickerID,
		WorkspaceKey:      gitWorkspace,
		TargetPickerValue: "worktree_create",
	}))

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_member",
		PickerID:         worktree.PickerID,
		RequestAnswers: map[string][]string{
			control.FeishuTargetPickerWorktreeBranchFieldName:    {"feat/room-worktree"},
			control.FeishuTargetPickerWorktreeDirectoryFieldName: {"room-worktree"},
		},
	})

	for _, event := range events {
		if event.DaemonCommand != nil {
			t.Fatalf("room workspace preflight must block before daemon worktree create, got %#v", events)
		}
	}
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected same-card preflight failure, got %#v", events)
	}
	blocked := events[0].TargetPickerView
	if blocked.Page != control.FeishuTargetPickerPageWorktree || !targetPickerHasBlockingMessage(blocked.SourceMessages, "请先对当前机器人执行 `/primary on`，再切换群 workspace。") {
		t.Fatalf("expected worktree page to show room primary preflight failure, got %#v", blocked)
	}
	if room.WorkspaceKey != currentWorkspace || room.WorkspaceResetGeneration != 0 {
		t.Fatalf("preflight must not mutate room binding, got %#v", room)
	}
}
