package orchestrator

import (
	"errors"
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
	if len(events) != 1 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected same-card success state after picker confirm, got %#v", events)
	}
	if got := events[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已切换会话" {
		t.Fatalf("expected succeeded target picker card, got %#v", got)
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
	if len(events) != 1 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected same-card success state for new-thread confirm, got %#v", events)
	}
	if got := events[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded new-thread target picker card, got %#v", got)
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
	if len(events) == 0 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected processing target picker card before headless completion, got %#v", events)
	}
	if got := events[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing {
		t.Fatalf("expected processing stage while headless launch is pending, got %#v", got)
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
	if len(connectEvents) != 1 || connectEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected same-card success after headless connect, got %#v", connectEvents)
	}
	if got := connectEvents[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded target picker card after headless connect, got %#v", got)
	}
}

func TestTargetPickerPendingNewThreadFailureFinishesSameCardAndClearsRuntime(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 17, 0, 0, time.UTC)
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
	svc.RecordTargetPickerMessage("surface-1", view.PickerID, "om-card-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      "/data/dl/picdetect",
		TargetPickerValue: targetPickerNewThreadValue,
	})
	if len(events) == 0 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected processing target picker card before headless failure, got %#v", events)
	}
	if got := events[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing || got.MessageID != "om-card-1" {
		t.Fatalf("expected processing stage to target same owner card, got %#v", got)
	}

	surface := svc.root.Surfaces["surface-1"]
	pending := surface.PendingHeadless
	if pending == nil {
		t.Fatalf("expected pending headless launch after processing stage")
	}

	failureEvents := svc.HandleHeadlessLaunchFailed("surface-1", pending.InstanceID, errors.New("dial failed"))
	if len(failureEvents) != 1 || failureEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected single failed target picker card after headless failure, got %#v", failureEvents)
	}
	got := failureEvents[0].FeishuTargetPickerView
	if got.Stage != control.FeishuTargetPickerStageFailed || got.StatusTitle != "切换失败" {
		t.Fatalf("expected failed terminal target picker card, got %#v", got)
	}
	if got.MessageID != "om-card-1" {
		t.Fatalf("expected failed terminal card to update original owner card, got %#v", got)
	}
	if strings.TrimSpace(got.StatusText) == "" {
		t.Fatalf("expected failed terminal card to include failure detail, got %#v", got)
	}
	if svc.activeTargetPicker(surface) != nil || svc.activeOwnerCardFlow(surface) != nil {
		t.Fatalf("expected failed terminal card to clear picker runtime, got runtime=%#v", svc.SurfaceUIRuntime("surface-1"))
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
	if len(events) != 1 || events[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected refreshed picker after stale confirm, got %#v", events)
	}
	got := events[0].FeishuTargetPickerView
	if got.SelectedSessionValue != "" || got.CanConfirm {
		t.Fatalf("expected refreshed picker to clear stale session selection, got %#v", got)
	}
	if len(got.Messages) == 0 || !strings.Contains(got.Messages[0].Text, "刚刚发生变化") {
		t.Fatalf("expected stale confirm to surface in-card warning, got %#v", got.Messages)
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
	if len(cancelEvents) != 1 || cancelEvents[0].FeishuTargetPickerView == nil || cancelEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected cancel to restore target picker by updating the owner card, got %#v", cancelEvents)
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
	if len(cancelEvents) != 1 || !cancelEvents[0].InlineReplaceCurrentCard || cancelEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected cancel to seal the current owner card inline, got %#v", cancelEvents)
	}
	if got := cancelEvents[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageCancelled || got.StatusTitle != "已取消" {
		t.Fatalf("expected cancelled target picker terminal card, got %#v", got)
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
	if len(confirmEvents) != 1 || confirmEvents[0].FeishuTargetPickerView == nil || confirmEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected path confirm to restore target card by updating the owner card, got %#v", confirmEvents)
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
	if len(confirmEvents) != 1 || confirmEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected same-card success after local-directory confirm, got %#v", confirmEvents)
	}
	if got := confirmEvents[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded target picker card after local-directory confirm, got %#v", got)
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
	if len(confirmEvents) != 1 || confirmEvents[0].FeishuTargetPickerView == nil {
		t.Fatalf("expected same-card success after symlinked workspace confirm, got %#v", confirmEvents)
	}
	if got := confirmEvents[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded target picker card after symlinked workspace confirm, got %#v", got)
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

func TestTargetPickerGitImportKeepsConfirmEnabledAndValidatesOnSubmit(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 52, 0, 0, time.UTC)
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
	if !gitSource.CanConfirm {
		t.Fatalf("expected git import confirm to stay clickable and validate on submit, got %#v", gitSource)
	}

	invalid := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         gitSource.PickerID,
		RequestAnswers: map[string][]string{
			control.FeishuTargetPickerGitRepoURLFieldName:       {"https://github.com/kxn/codex-remote-feishu.git"},
			control.FeishuTargetPickerGitDirectoryNameFieldName: {"test1122"},
		},
	}))
	if !invalid.CanConfirm {
		t.Fatalf("expected invalid submit to keep confirm clickable after inline validation, got %#v", invalid)
	}
	if invalid.GitRepoURL != "https://github.com/kxn/codex-remote-feishu.git" || invalid.GitDirectoryName != "test1122" {
		t.Fatalf("expected invalid submit to preserve draft answers on main card, got %#v", invalid)
	}
	if len(invalid.SourceMessages) == 0 || invalid.SourceMessages[0].Level != control.FeishuTargetPickerMessageDanger ||
		!strings.Contains(invalid.SourceMessages[0].Text, "落地目录") {
		t.Fatalf("expected inline blocking error on main card, got %#v", invalid.SourceMessages)
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
	if !gitSource.CanConfirm {
		t.Fatalf("expected git source confirm to stay enabled before preview, got %#v", gitSource)
	}

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
	if len(confirmEvents) != 2 || confirmEvents[0].FeishuTargetPickerView == nil || confirmEvents[1].DaemonCommand == nil {
		t.Fatalf("expected processing card plus daemon command, got %#v", confirmEvents)
	}
	processing := confirmEvents[0].FeishuTargetPickerView
	if processing.Stage != control.FeishuTargetPickerStageProcessing || processing.StatusTitle != "正在导入 Git 工作区" {
		t.Fatalf("expected git import processing card, got %#v", processing)
	}
	command := confirmEvents[1].DaemonCommand
	if command.Kind != control.DaemonCommandGitWorkspaceImport || command.PickerID != gitSource.PickerID {
		t.Fatalf("unexpected git import daemon command: %#v", command)
	}
	if command.RepoURL != "https://github.com/kxn/codex-remote-feishu.git" || command.RefName != "" || command.DirectoryName != "crf" || !testutil.SamePath(command.LocalPath, workspaceRoot) {
		t.Fatalf("unexpected git import daemon command payload: %#v", command)
	}
}

func TestTargetPickerGitImportProcessingBlocksOrdinaryInputButAllowsStatus(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 57, 0, 0, time.UTC)
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
	backfilled := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPathPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         pathView.PickerID,
	}))
	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         backfilled.PickerID,
	})
	if len(confirmEvents) == 0 || confirmEvents[0].FeishuTargetPickerView == nil || confirmEvents[0].FeishuTargetPickerView.Stage != control.FeishuTargetPickerStageProcessing {
		t.Fatalf("expected git import processing state before blocking checks, got %#v", confirmEvents)
	}

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续说话",
	})
	if len(blocked) != 1 || blocked[0].Notice == nil || blocked[0].Notice.Code != "target_picker_processing" {
		t.Fatalf("expected ordinary input to be blocked by target picker processing, got %#v", blocked)
	}

	statusEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStatus,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(statusEvents) != 1 || statusEvents[0].Snapshot == nil || statusEvents[0].Snapshot.Gate.Kind != "target_picker" {
		t.Fatalf("expected /status to stay available and expose target picker gate, got %#v", statusEvents)
	}
}

func TestTargetPickerCancelGitImportProcessingSealsCardAndDispatchesCancel(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 58, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	record := &activeTargetPickerRecord{
		PickerID:            "picker-1",
		OwnerUserID:         "user-1",
		Source:              control.TargetPickerRequestSourceList,
		SelectedMode:        control.FeishuTargetPickerModeAddWorkspace,
		Stage:               control.FeishuTargetPickerStageProcessing,
		PendingKind:         targetPickerPendingGitImport,
		PendingWorkspaceKey: "/data/dl/projects/repo-a",
		GitRepoURL:          "https://github.com/kxn/codex-remote-feishu.git",
		GitFinalPath:        "/data/dl/projects/repo-a",
	}
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
	svc.setActiveTargetPicker(surface, record)
	svc.RecordTargetPickerMessage("surface-1", record.PickerID, "om-card-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerCancel,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         record.PickerID,
	})
	if len(events) != 2 || events[0].FeishuTargetPickerView == nil || events[1].DaemonCommand == nil {
		t.Fatalf("expected cancelled same-card result plus daemon cancel, got %#v", events)
	}
	if got := events[0].FeishuTargetPickerView; got.Stage != control.FeishuTargetPickerStageCancelled || got.StatusTitle != "已取消导入" || got.MessageID != "om-card-1" {
		t.Fatalf("expected cancelled git-import terminal card on original owner card, got %#v", got)
	}
	if got := events[1].DaemonCommand; got.Kind != control.DaemonCommandGitWorkspaceImportCancel || got.PickerID != record.PickerID {
		t.Fatalf("expected git import cancel daemon command, got %#v", got)
	}
	if svc.activeTargetPicker(surface) != nil || svc.activeOwnerCardFlow(surface) != nil {
		t.Fatalf("expected cancel to clear target picker runtime, got %#v", svc.SurfaceUIRuntime("surface-1"))
	}
}
