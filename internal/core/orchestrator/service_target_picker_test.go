package orchestrator

import (
	"os"
	"path/filepath"
	"runtime"
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
	if picker := svc.activeTargetPicker(surface); picker != nil {
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
	if picker := svc.activeTargetPicker(surface); picker != nil {
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
	if svc.activeTargetPicker(surface) == nil {
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
	if !view.ShowSourceSelect || view.CanConfirm || view.ConfirmLabel != "接入并继续" {
		t.Fatalf("expected add-workspace flow to wait for directory selection, got %#v", view)
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

func TestTargetPickerOpenAddWorkspaceLocalDirectoryPathPickerWithoutRouteMutation(t *testing.T) {
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
		WorkspaceKey:     "/data/dl/web",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	svc.root.Surfaces["surface-1"].ClaimedWorkspaceKey = workspaceRoot

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
	if updated.ConfirmLabel != "接入并继续" || updated.CanConfirm {
		t.Fatalf("expected add-workspace/local-directory branch to wait for path selection, got %#v", updated)
	}

	pathEvents := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          updated.PickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldLocalDirectory,
	})
	pathView := singlePathPickerEvent(t, pathEvents)
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeUnbound || surface.PendingHeadless != nil {
		t.Fatalf("expected route to stay on current workspace until path confirm, got %#v", surface)
	}
	if svc.activeTargetPicker(surface) == nil || svc.activePathPicker(surface) == nil {
		t.Fatalf("expected both target picker and appended path picker to stay active, got %#v", surface)
	}
	if !pathEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected local-directory path picker to replace current card inline, got %#v", pathEvents)
	}
	if pathView.Title != "选择目录路径" || pathView.ConfirmLabel != "使用这个目录" || pathView.CancelLabel != "返回" || !strings.Contains(pathView.Hint, "回到主卡") {
		t.Fatalf("unexpected local-directory path picker view: %#v", pathView)
	}
}

func TestTargetPickerAddWorkspacePathPickerCancelRestoresTargetCard(t *testing.T) {
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
		WorkspaceKey:     "/data/dl/web",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	svc.root.Surfaces["surface-1"].ClaimedWorkspaceKey = workspaceRoot
	updated := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          updated.PickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldLocalDirectory,
	}))

	cancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerCancel,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	})
	surface := svc.root.Surfaces["surface-1"]
	if svc.activePathPicker(surface) != nil || svc.activeTargetPicker(surface) == nil {
		t.Fatalf("expected cancel to close only the path picker and keep target picker alive, got %#v", surface)
	}
	if surface.RouteMode != state.RouteModeUnbound || surface.PendingHeadless != nil {
		t.Fatalf("expected cancel to keep current target unchanged, got %#v", surface)
	}
	if len(cancelEvents) != 1 || cancelEvents[0].FeishuTargetPickerView == nil || !cancelEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected cancel to restore target picker inline, got %#v", cancelEvents)
	}
	if got := cancelEvents[0].FeishuTargetPickerView; got.LocalDirectoryPath != "" || got.CanConfirm {
		t.Fatalf("expected cancel to preserve empty local-directory selection, got %#v", got)
	}
}

func TestTargetPickerCancelClearsActivePickerAndKeepsSurfaceRoute(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 46, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "当前会话", CWD: "/data/dl/web", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
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
	surface := svc.root.Surfaces["surface-1"]
	beforeRouteMode := surface.RouteMode
	beforeWorkspace := surface.ClaimedWorkspaceKey
	beforeAttachedInstance := surface.AttachedInstanceID
	beforeSelectedThread := surface.SelectedThreadID

	cancelEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerCancel,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if svc.activeTargetPicker(surface) != nil {
		t.Fatalf("expected cancel to clear active target picker, got %#v", surface)
	}
	if surface.RouteMode != beforeRouteMode || surface.ClaimedWorkspaceKey != beforeWorkspace || surface.AttachedInstanceID != beforeAttachedInstance || surface.SelectedThreadID != beforeSelectedThread {
		t.Fatalf("expected cancel to keep surface route unchanged, got %#v", surface)
	}
	if len(cancelEvents) != 1 || !cancelEvents[0].InlineReplaceCurrentCard || cancelEvents[0].Notice == nil {
		t.Fatalf("expected cancel to replace current card with a notice, got %#v", cancelEvents)
	}
	if cancelEvents[0].Notice.Code != "target_picker_cancelled" {
		t.Fatalf("expected target_picker_cancelled notice, got %#v", cancelEvents[0].Notice)
	}
}

func TestTargetPickerAddWorkspacePathPickerConfirmBackfillsLocalDirectoryAndWaitsForMainConfirm(t *testing.T) {
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
		WorkspaceKey:     "/data/dl/web",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	svc.root.Surfaces["surface-1"].ClaimedWorkspaceKey = workspaceRoot
	updated := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          updated.PickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldLocalDirectory,
	}))

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface.RouteMode != state.RouteModeUnbound || surface.PendingHeadless != nil {
		t.Fatalf("expected path confirm to keep current route unchanged until main confirm, got %#v", surface)
	}
	if svc.activePathPicker(surface) != nil || svc.activeTargetPicker(surface) == nil {
		t.Fatalf("expected path confirm to close only the path picker, got %#v", surface)
	}
	if len(confirmEvents) != 1 || confirmEvents[0].FeishuTargetPickerView == nil || !confirmEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected path confirm to restore target card inline, got %#v", confirmEvents)
	}
	got := confirmEvents[0].FeishuTargetPickerView
	if !testutil.SamePath(got.LocalDirectoryPath, workspaceRoot) || !got.CanConfirm {
		t.Fatalf("expected path confirm to backfill local directory and enable main confirm, got %#v", got)
	}
	if len(got.SourceMessages) == 0 || !strings.Contains(got.SourceMessages[0].Text, "复用") {
		t.Fatalf("expected local-directory backfill to explain workspace reuse, got %#v", got.SourceMessages)
	}
}

func TestTargetPickerConfirmAddWorkspaceLocalDirectoryEntersNewThreadReady(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 48, 0, 0, time.UTC)
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
	svc.root.Surfaces["surface-1"].ClaimedWorkspaceKey = workspaceRoot
	addMode := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	surface := svc.root.Surfaces["surface-1"]
	record := svc.activeTargetPicker(surface)
	record.LocalDirectoryPath = workspaceRoot

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         addMode.PickerID,
	})
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected main confirm to enter new-thread-ready on selected directory, got %#v", surface)
	}
	if svc.activeTargetPicker(surface) != nil || svc.activePathPicker(surface) != nil {
		t.Fatalf("expected local-directory success path to clear active picker state, got %#v", surface)
	}
	var sawReady bool
	for _, event := range confirmEvents {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread ready notice after main confirm, got %#v", confirmEvents)
	}
}

func TestTargetPickerConfirmAddWorkspaceLocalDirectoryTreatsSymlinkedCurrentWorkspaceAsSameWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink setup is not reliable on windows CI")
	}

	now := time.Date(2026, 4, 14, 15, 50, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := t.TempDir()
	realWorkspace := filepath.Join(root, "real-workspace")
	if err := os.MkdirAll(realWorkspace, 0o755); err != nil {
		t.Fatalf("MkdirAll(realWorkspace): %v", err)
	}
	linkWorkspace := filepath.Join(root, "link-workspace")
	if err := os.Symlink(realWorkspace, linkWorkspace); err != nil {
		t.Fatalf("Symlink(%q -> %q): %v", linkWorkspace, realWorkspace, err)
	}

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: linkWorkspace,
		WorkspaceKey:  linkWorkspace,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     linkWorkspace,
	})
	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	svc.root.Surfaces["surface-1"].ClaimedWorkspaceKey = linkWorkspace
	addMode := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectMode,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: string(control.FeishuTargetPickerModeAddWorkspace),
	}))
	surface := svc.root.Surfaces["surface-1"]
	record := svc.activeTargetPicker(surface)
	record.LocalDirectoryPath = linkWorkspace

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         addMode.PickerID,
	})
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, realWorkspace) {
		t.Fatalf("expected symlinked current workspace to stay on prepared new-thread route, got %#v", surface)
	}
	if surface.PendingHeadless != nil {
		t.Fatalf("did not expect symlinked current workspace to start headless launch, got %#v", surface.PendingHeadless)
	}
	var sawReady bool
	for _, event := range confirmEvents {
		if event.Notice != nil && event.Notice.Code == "new_thread_ready" {
			sawReady = true
		}
	}
	if !sawReady {
		t.Fatalf("expected new-thread-ready notice after symlinked workspace confirm, got %#v", confirmEvents)
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/web",
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
	if len(gitSource.SourceMessages) == 0 || !strings.Contains(gitSource.SourceMessages[0].Text, "git") {
		t.Fatalf("expected missing-git message on main card, got %#v", gitSource.SourceMessages)
	}
}

func TestTargetPickerGitImportFlowBackfillsMainCardAndDispatchesDaemonCommand(t *testing.T) {
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

	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          gitSource.PickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldGitParentDir,
		RequestAnswers: map[string][]string{
			control.FeishuTargetPickerGitRepoURLFieldName:       {"https://github.com/kxn/codex-remote-feishu.git"},
			control.FeishuTargetPickerGitDirectoryNameFieldName: {"crf"},
		},
	}))
	if pathView.Title != "选择落地目录" || pathView.CancelLabel != "返回" {
		t.Fatalf("expected git parent-directory picker, got %#v", pathView)
	}

	backfilled := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	}))
	if !testutil.SamePath(backfilled.GitParentDir, workspaceRoot) || !strings.HasSuffix(backfilled.GitFinalPath, "/crf") {
		t.Fatalf("expected git parent-dir confirm to backfill main card, got %#v", backfilled)
	}
	if backfilled.GitRepoURL != "https://github.com/kxn/codex-remote-feishu.git" || backfilled.GitDirectoryName != "crf" || !backfilled.CanConfirm {
		t.Fatalf("expected git form values to be preserved and become confirmable, got %#v", backfilled)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         backfilled.PickerID,
	})
	if len(confirmEvents) != 2 || confirmEvents[1].DaemonCommand == nil {
		t.Fatalf("expected starting notice plus daemon command, got %#v", confirmEvents)
	}
	command := confirmEvents[1].DaemonCommand
	if command.Kind != control.DaemonCommandGitWorkspaceImport || command.PickerID != gitSource.PickerID {
		t.Fatalf("unexpected git import daemon command: %#v", command)
	}
	if command.RepoURL != "https://github.com/kxn/codex-remote-feishu.git" || command.RefName != "" || command.DirectoryName != "crf" || !testutil.SamePath(command.LocalPath, workspaceRoot) {
		t.Fatalf("unexpected git import daemon command payload: %#v", command)
	}
}
