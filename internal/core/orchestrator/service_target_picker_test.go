package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestTargetPickerSelectWorkspaceRefreshesSessionsInline(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-droid": {ThreadID: "thread-droid", Name: "修复登录", CWD: "/data/dl/droid", LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})

	initial := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerSelectWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         initial.PickerID,
		WorkspaceKey:     "/data/dl/droid",
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected inline target picker refresh, got %#v", events)
	}
	view := targetPickerFromEvent(t, events[0])
	if view.SelectedWorkspaceKey != "/data/dl/droid" {
		t.Fatalf("expected selected workspace to update, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-droid")); !ok {
		t.Fatalf("expected workspace-specific sessions after refresh, got %#v", view.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-web")); ok {
		t.Fatalf("expected other workspace session to disappear after refresh, got %#v", view.SessionOptions)
	}
	if view.SelectedSessionValue != "" {
		t.Fatalf("expected session selection to clear after workspace switch, got %#v", view)
	}
	if view.CanConfirm {
		t.Fatalf("expected confirm to stay disabled until a new session is chosen, got %#v", view)
	}
}

func TestTargetPickerConfirmExistingThreadAttachesSelection(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      "/data/dl/web",
		TargetPickerValue: targetPickerThreadValue("thread-web"),
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "thread-web" || !testutil.SamePath(surface.ClaimedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected target picker confirm to attach selected thread, got %#v", surface)
	}
	if surface.ActiveTargetPicker != nil {
		t.Fatalf("expected successful confirm to clear active picker")
	}
	var sawAttached bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "attached" {
			sawAttached = true
		}
	}
	if !sawAttached {
		t.Fatalf("expected attach notice after picker confirm, got %#v", events)
	}
}

func TestTargetPickerConfirmNewThreadOnAttachedWorkspaceEntersReadyState(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "当前会话", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/droid",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      "/data/dl/droid",
		TargetPickerValue: targetPickerNewThreadValue,
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, "/data/dl/droid") {
		t.Fatalf("expected target picker new-thread confirm to enter ready state, got %#v", surface)
	}
	if surface.ActiveTargetPicker != nil {
		t.Fatalf("expected successful confirm to clear active picker")
	}
	var sawReady bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread ready notice, got %#v", events)
	}
}

func TestTargetPickerConfirmRecoverableWorkspaceNewThreadStartsHeadless(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{
			{
				ThreadID:   "thread-picdetect",
				Name:       "排查图片识别",
				CWD:        "/data/dl/picdetect",
				LastUsedAt: now.Add(-1 * time.Minute),
			},
		},
		byID: map[string]state.ThreadRecord{},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      "/data/dl/picdetect",
		TargetPickerValue: targetPickerNewThreadValue,
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.PendingHeadless == nil || !surface.PendingHeadless.PrepareNewThread || !testutil.SamePath(surface.PendingHeadless.ThreadCWD, "/data/dl/picdetect") {
		t.Fatalf("expected recoverable workspace new-thread to start prepared headless launch, got %#v", surface.PendingHeadless)
	}
	var sawStart bool
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatalf("expected headless start command, got %#v", events)
	}

	pending := surface.PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: "/data/dl/picdetect",
		WorkspaceKey:  "/data/dl/picdetect",
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	connectEvents := svc.ApplyInstanceConnected(pending.InstanceID)
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, "/data/dl/picdetect") {
		t.Fatalf("expected connected headless workspace to enter new-thread ready, got %#v", surface)
	}
	var sawReady bool
	for _, event := range connectEvents {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread ready notice after headless connect, got %#v", connectEvents)
	}
}

func TestTargetPickerConfirmRejectsStaleSessionFallback(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-old": {ThreadID: "thread-old", Name: "旧会话", CWD: "/data/dl/web", Loaded: true, LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	inst := svc.root.Instances["inst-web"]
	inst.Threads = map[string]*state.ThreadRecord{
		"thread-new": {ThreadID: "thread-new", Name: "新会话", CWD: "/data/dl/web", Loaded: true, LastUsedAt: now.Add(-1 * time.Minute)},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      "/data/dl/web",
		TargetPickerValue: targetPickerThreadValue("thread-old"),
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "" {
		t.Fatalf("expected stale confirm not to attach fallback session, got %#v", surface)
	}
	if surface.ActiveTargetPicker == nil {
		t.Fatalf("expected stale confirm to keep active picker for retry")
	}
	var sawRefresh bool
	var sawNotice bool
	for _, event := range events {
		if event.FeishuTargetPickerView != nil {
			sawRefresh = true
			if event.FeishuTargetPickerView.SelectedSessionValue != "" || event.FeishuTargetPickerView.CanConfirm {
				t.Fatalf("expected refreshed picker to clear stale session selection, got %#v", event.FeishuTargetPickerView)
			}
		}
		if event.Notice != nil && event.Notice.Code == "target_picker_selection_changed" {
			sawNotice = true
		}
	}
	if !sawRefresh || !sawNotice {
		t.Fatalf("expected refreshed picker and stale-selection notice, got %#v", events)
	}
}

func TestTargetPickerListPrefersRealWorkspaceWhileExposeAddModeSwitch(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 25, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	if len(view.WorkspaceOptions) != 1 {
		t.Fatalf("expected one real workspace in existing-workspace mode, got %#v", view.WorkspaceOptions)
	}
	if _, ok := targetPickerModeOption(view, control.FeishuTargetPickerModeAddWorkspace); !ok || !view.ShowModeSwitch {
		t.Fatalf("expected target picker to expose add-workspace mode switch, got %#v", view)
	}
	if view.SelectedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected initial selection to stay on real workspace, got %#v", view)
	}
	if view.SelectedSessionValue != "" || view.CanConfirm {
		t.Fatalf("expected detached target picker to keep session empty until explicit selection, got %#v", view)
	}
}

func TestTargetPickerListFallsBackToAddWorkspaceModeWhenNoWorkspaceExists(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	if len(view.WorkspaceOptions) != 0 {
		t.Fatalf("expected existing-workspace dropdown to be empty when no workspace exists, got %#v", view.WorkspaceOptions)
	}
	if view.SelectedMode != control.FeishuTargetPickerModeAddWorkspace || view.SelectedSource != control.FeishuTargetPickerSourceLocalDirectory {
		t.Fatalf("expected picker to fall back to add-workspace/local-directory flow, got %#v", view)
	}
	if !view.ShowSourceSelect || !view.CanConfirm || view.ConfirmLabel != "选择目录" {
		t.Fatalf("expected add-workspace flow to be immediately actionable, got %#v", view)
	}
}

func TestTargetPickerShowThreadsOnAttachedWorkspaceKeepsSessionEmptyWhenRouteUnbound(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 32, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	if view.SelectedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected current workspace to remain selected, got %#v", view)
	}
	if view.SelectedSessionValue != "" || view.CanConfirm {
		t.Fatalf("expected unbound route to keep session empty until explicit user choice, got %#v", view)
	}
}

func TestTargetPickerShowThreadsKeepsCurrentThreadSelection(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 33, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", Loaded: true},
			"thread-alt": {ThreadID: "thread-alt", Name: "修复按钮", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-web",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	if view.SelectedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected current workspace to remain selected, got %#v", view)
	}
	if view.SelectedSessionValue != targetPickerThreadValue("thread-web") || !view.CanConfirm {
		t.Fatalf("expected current thread to stay preselected, got %#v", view)
	}
}

func TestTargetPickerConfirmAddWorkspaceLocalDirectoryOpensDirectoryPickerWithoutRouteMutation(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 35, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     workspaceRoot,
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	})
	updated := singleTargetPickerEvent(t, events)
	if updated.SelectedMode != control.FeishuTargetPickerModeAddWorkspace || updated.SelectedSource != control.FeishuTargetPickerSourceLocalDirectory {
		t.Fatalf("expected picker to switch into add-workspace/local-directory branch, got %#v", updated)
	}
	if updated.ConfirmLabel != "选择目录" || !updated.CanConfirm {
		t.Fatalf("expected add-workspace/local-directory branch to become confirmable, got %#v", updated)
	}

	pathEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         updated.PickerID,
	})
	pathView := singlePathPickerEvent(t, pathEvents)
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeUnbound || !testutil.SamePath(surface.ClaimedWorkspaceKey, workspaceRoot) {
		t.Fatalf("expected route to stay on current workspace until path confirm, got %#v", surface)
	}
	if surface.ActiveTargetPicker == nil || surface.ActivePathPicker == nil {
		t.Fatalf("expected both target picker and appended path picker to stay active, got %#v", surface)
	}
	if pathView.Title != "选择要接入的目录" || pathView.ConfirmLabel != "接入并准备新会话" || !strings.Contains(pathView.Hint, "未确认前不会切换当前工作目标。") {
		t.Fatalf("unexpected target-picker workspace create path view: %#v", pathView)
	}
}

func TestTargetPickerAddWorkspacePathPickerCancelKeepsCurrentTarget(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 40, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     workspaceRoot,
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	updated := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         updated.PickerID,
	}))

	cancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActivePathPicker != nil || surface.ActiveTargetPicker == nil {
		t.Fatalf("expected cancel to close only the path picker and keep target picker alive, got %#v", surface)
	}
	if surface.RouteMode != state.RouteModeUnbound || !testutil.SamePath(surface.ClaimedWorkspaceKey, workspaceRoot) {
		t.Fatalf("expected cancel to keep current target unchanged, got %#v", surface)
	}
	if len(cancelEvents) != 1 || cancelEvents[0].Notice == nil || cancelEvents[0].Notice.Code != "workspace_create_cancelled" {
		t.Fatalf("expected explicit cancellation notice, got %#v", cancelEvents)
	}
	if cancelEvents[0].Notice.Text != "已取消添加工作区。当前工作目标保持不变。" {
		t.Fatalf("unexpected cancel notice: %#v", cancelEvents[0].Notice)
	}
}

func TestTargetPickerAddWorkspacePathPickerConfirmEntersNewThreadReadyAndClearsSourcePicker(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 45, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     workspaceRoot,
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	updated := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         updated.PickerID,
	}))

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected path confirm to enter new-thread-ready on the selected directory, got %#v", surface)
	}
	if surface.ActivePathPicker != nil || surface.ActiveTargetPicker != nil {
		t.Fatalf("expected success path to clear both pickers, got %#v", surface)
	}
	var sawReady bool
	for _, event := range confirmEvents {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread ready notice after path confirm, got %#v", confirmEvents)
	}
}

func TestTargetPickerAddWorkspaceGitSourceShowsDisabledHintWhenGitMissing(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 50, 0, 0, time.UTC)
	svc := NewService(func() time.Time { return now }, Config{TurnHandoffWait: 800 * time.Millisecond, GitAvailable: false}, renderer.NewPlanner())
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	addMode := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	gitSource := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectSource,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          addMode.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerSourceGitURL),
	}))

	if gitSource.SelectedMode != control.FeishuTargetPickerModeAddWorkspace || gitSource.SelectedSource != control.FeishuTargetPickerSourceGitURL {
		t.Fatalf("expected add-workspace/git source selection, got %#v", gitSource)
	}
	if gitSource.CanConfirm {
		t.Fatalf("expected git source to stay disabled without git executable, got %#v", gitSource)
	}
	if gitSource.SourceUnavailableHint == "" || !strings.Contains(gitSource.SourceUnavailableHint, "git") {
		t.Fatalf("expected explicit missing-git hint, got %#v", gitSource)
	}
}

func TestTargetPickerGitImportFlowCollectsPromptAndDaemonCommand(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 55, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	addMode := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	gitSource := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectSource,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          addMode.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerSourceGitURL),
	}))

	prompt := singleRequestPromptEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         gitSource.PickerID,
	}))
	if prompt.RequestType != "request_user_input" || len(prompt.Questions) != 3 {
		t.Fatalf("expected git import prompt with three questions, got %#v", prompt)
	}
	if len(prompt.Options) != 1 || prompt.Options[0].OptionID != "cancel" {
		t.Fatalf("expected git import prompt cancel option, got %#v", prompt.Options)
	}

	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		RequestID:        prompt.RequestID,
		RequestType:      prompt.RequestType,
		RequestOptionID:  "submit",
		RequestAnswers: map[string][]string{
			targetPickerGitImportFieldRepoURL:       {"https://github.com/kxn/codex-remote-feishu.git"},
			targetPickerGitImportFieldBranchOrTag:   {"release/1.5"},
			targetPickerGitImportFieldDirectoryName: {"crf"},
		},
		RequestRevision: prompt.RequestRevision,
	}))
	if pathView.ConfirmLabel != "克隆到这里" {
		t.Fatalf("expected follow-up parent-directory picker, got %#v", pathView)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	})
	if len(confirmEvents) != 2 || confirmEvents[1].DaemonCommand == nil {
		t.Fatalf("expected starting notice plus daemon command, got %#v", confirmEvents)
	}
	command := confirmEvents[1].DaemonCommand
	if command.Kind != control.DaemonCommandGitWorkspaceImport || command.PickerID != gitSource.PickerID {
		t.Fatalf("unexpected git import daemon command: %#v", command)
	}
	if command.RepoURL != "https://github.com/kxn/codex-remote-feishu.git" || command.RefName != "release/1.5" || command.DirectoryName != "crf" {
		t.Fatalf("unexpected git import daemon command payload: %#v", command)
	}
}
