package orchestrator

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestWorkspaceSessionCatalogProvenanceDrivesTargetPickerOpen(t *testing.T) {
	now := time.Date(2026, 4, 14, 14, 59, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
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

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		CatalogFamilyID:  control.FeishuCommandUseAll,
		CatalogVariantID: "useall.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
	}))
	if view.Source != control.TargetPickerRequestSourceUseAll {
		t.Fatalf("expected catalog family to drive target picker source, got %#v", view)
	}
	if view.CatalogFamilyID != control.FeishuCommandUseAll || view.CatalogVariantID != "useall.codex.normal" || view.CatalogBackend != agentproto.BackendCodex {
		t.Fatalf("expected target picker view to retain catalog provenance, got %#v", view)
	}
}

func TestTargetPickerUseFiltersSessionsByClaudeBackendButNotProfile(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "profile-a", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude",
		DisplayName:     "repo",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-b",
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-claude": {ThreadID: "thread-claude", Name: "Claude 会话", CWD: "/data/dl/repo", LastUsedAt: now},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-codex",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendCodex,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/repo", LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-claude",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if view.Source != control.TargetPickerRequestSourceUse || view.SelectedWorkspaceKey != "/data/dl/repo" {
		t.Fatalf("expected claude /use to stay scoped to current workspace, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-claude")); !ok {
		t.Fatalf("expected claude thread to stay visible, got %#v", view.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-codex")); ok {
		t.Fatalf("expected codex thread to be filtered out in claude mode, got %#v", view.SessionOptions)
	}
}

func TestTargetPickerUseShowsCodexSessionDespiteProviderMismatch(t *testing.T) {
	now := time.Date(2026, 5, 1, 2, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeWithCodexProvider("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-codex",
		DisplayName:     "repo",
		WorkspaceRoot:   "/data/dl/repo",
		WorkspaceKey:    "/data/dl/repo",
		ShortName:       "repo",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-codex": {ThreadID: "thread-codex", Name: "Codex 会话", CWD: "/data/dl/repo", LastUsedAt: now},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if _, ok := targetPickerSessionOption(view, targetPickerThreadValue("thread-codex")); !ok {
		t.Fatalf("expected provider-mismatched codex thread to stay visible, got %#v", view.SessionOptions)
	}
}

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

func TestTargetPickerLockedWorkspaceRejectsWorkspaceSwitch(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 2, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-droid": {ThreadID: "thread-droid", Name: "修复登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	view := singleTargetPickerEvent(t, svc.openLockedWorkspaceTargetPicker(surface, "/data/dl/web", true))
	if !view.WorkspaceSelectionLocked || !testutil.SamePath(view.SelectedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected locked target picker to stay on web workspace, got %#v", view)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerSelectWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
		WorkspaceKey:     "/data/dl/droid",
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected locked workspace refresh inline, got %#v", events)
	}
	got := targetPickerFromEvent(t, events[0])
	if !got.WorkspaceSelectionLocked || got.ShowWorkspaceSelect || !testutil.SamePath(got.SelectedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected locked picker to keep current workspace, got %#v", got)
	}
	if _, ok := targetPickerSessionOption(got, targetPickerThreadValue("thread-web")); !ok {
		t.Fatalf("expected locked picker to keep web sessions, got %#v", got.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(got, targetPickerThreadValue("thread-droid")); ok {
		t.Fatalf("expected locked picker to omit foreign workspace sessions, got %#v", got.SessionOptions)
	}
	var sawWarning bool
	for _, message := range got.Messages {
		if strings.Contains(message.Text, "当前工作区已锁定") {
			sawWarning = true
			break
		}
	}
	if !sawWarning {
		t.Fatalf("expected locked picker warning after stale workspace switch, got %#v", got.Messages)
	}
}

func TestTargetPickerPageWorkspaceSwitchRecomputesSessions(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 2, 30, 0, time.UTC)
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
	if !testutil.SamePath(initial.SelectedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected initial selection on most recent workspace, got %#v", initial)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerPage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         initial.PickerID,
		FieldName:        frontstagecontract.CardTargetPickerWorkspaceFieldName,
		Cursor:           1,
	})
	got := targetPickerFromEvent(t, events[0])
	if !testutil.SamePath(got.SelectedWorkspaceKey, "/data/dl/droid") || got.WorkspaceCursor != 1 {
		t.Fatalf("expected workspace page action to switch visible workspace, got %#v", got)
	}
	if got.SessionCursor != 0 || got.SelectedSessionValue != "" || got.CanConfirm {
		t.Fatalf("expected workspace page action to reset session selection state, got %#v", got)
	}
	if _, ok := targetPickerSessionOption(got, targetPickerThreadValue("thread-droid")); !ok {
		t.Fatalf("expected workspace page action to recompute session list, got %#v", got.SessionOptions)
	}
	if _, ok := targetPickerSessionOption(got, targetPickerThreadValue("thread-web")); ok {
		t.Fatalf("expected workspace page action to drop stale workspace sessions, got %#v", got.SessionOptions)
	}
}

func TestTargetPickerPageSessionKeepsWorkspaceStateAndClearsSelection(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 2, 45, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-web": {ThreadID: "thread-web", Name: "整理样式", CWD: "/data/dl/web", Loaded: true, LastUsedAt: now.Add(-1 * time.Minute)},
			"thread-alt": {ThreadID: "thread-alt", Name: "修复按钮", CWD: "/data/dl/web", Loaded: true, LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-web",
	})

	initial := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if initial.SelectedSessionValue != targetPickerThreadValue("thread-web") || !initial.CanConfirm {
		t.Fatalf("expected initial picker to carry current thread selection, got %#v", initial)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerPage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         initial.PickerID,
		FieldName:        frontstagecontract.CardTargetPickerSessionFieldName,
		Cursor:           1,
	})
	got := targetPickerFromEvent(t, events[0])
	if !testutil.SamePath(got.SelectedWorkspaceKey, "/data/dl/web") || got.WorkspaceCursor != initial.WorkspaceCursor {
		t.Fatalf("expected session page action to preserve workspace state, got %#v", got)
	}
	if got.SessionCursor != 1 || got.SelectedSessionValue != "" || got.CanConfirm {
		t.Fatalf("expected session page action to clear invisible session selection, got %#v", got)
	}
}

func TestTargetPickerLockedWorkspaceConfirmRejectsStaleWorkspacePayload(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 3, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
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
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-droid",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-droid": {ThreadID: "thread-droid", Name: "修复登录", CWD: "/data/dl/droid", Loaded: true},
		},
	})

	view := singleTargetPickerEvent(t, svc.openLockedWorkspaceTargetPicker(surface, "/data/dl/web", true))
	selectEvents := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerSelectSession,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		TargetPickerValue: targetPickerThreadValue("thread-web"),
	})
	selected := targetPickerFromEvent(t, selectEvents[0])
	if selected.SelectedSessionValue != targetPickerThreadValue("thread-web") {
		t.Fatalf("expected session selection to stick before confirm, got %#v", selected)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          view.PickerID,
		WorkspaceKey:      "/data/dl/droid",
		TargetPickerValue: targetPickerThreadValue("thread-web"),
	})
	if len(confirmEvents) != 1 || confirmEvents[0].TargetPickerView == nil {
		t.Fatalf("expected stale confirm to refresh target picker, got %#v", confirmEvents)
	}
	if confirmEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected stale confirm warning to use patch flow, got %#v", confirmEvents[0])
	}
	got := targetPickerFromEvent(t, confirmEvents[0])
	if !got.WorkspaceSelectionLocked || !testutil.SamePath(got.SelectedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected stale confirm to keep locked workspace, got %#v", got)
	}
	if got.SelectedSessionValue != targetPickerThreadValue("thread-web") {
		t.Fatalf("expected stale confirm to keep selected session, got %#v", got)
	}
	var sawWarning bool
	for _, message := range got.Messages {
		if strings.Contains(message.Text, "当前工作区已锁定") {
			sawWarning = true
			break
		}
	}
	if !sawWarning {
		t.Fatalf("expected stale confirm warning, got %#v", got.Messages)
	}
	if svc.root.Surfaces["surface-1"].SelectedThreadID != "" {
		t.Fatalf("expected stale confirm not to switch threads, got %#v", svc.root.Surfaces["surface-1"])
	}
	if svc.activeTargetPicker(svc.root.Surfaces["surface-1"]) == nil {
		t.Fatalf("expected target picker runtime to stay active after stale confirm")
	}
}

func TestTargetPickerLockedWorkspaceAutoSelectsNewThreadWhenOnlyOption(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 4, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.ClaimedWorkspaceKey = "/data/dl/web"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	view := singleTargetPickerEvent(t, svc.openLockedWorkspaceTargetPicker(surface, "/data/dl/web", true))
	if !view.WorkspaceSelectionLocked || view.ShowWorkspaceSelect || !view.AllowNewThread {
		t.Fatalf("expected locked target picker with new-thread fallback, got %#v", view)
	}
	if view.SelectedSessionValue != targetPickerNewThreadValue || view.ConfirmLabel != "新建会话" || !view.CanConfirm {
		t.Fatalf("expected new-thread fallback to become primary action, got %#v", view)
	}
	if len(view.SessionOptions) != 1 {
		t.Fatalf("expected only new-thread option in locked workspace, got %#v", view.SessionOptions)
	}
	if option, ok := targetPickerSessionOption(view, targetPickerNewThreadValue); !ok || option.Kind != control.FeishuTargetPickerSessionNewThread {
		t.Fatalf("expected locked picker to expose new-thread option only, got %#v", view.SessionOptions)
	}
	var sawInfo bool
	for _, message := range view.Messages {
		if strings.Contains(message.Text, "可直接新建会话") {
			sawInfo = true
			break
		}
	}
	if !sawInfo {
		t.Fatalf("expected locked picker to explain new-thread fallback, got %#v", view.Messages)
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
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected same-card success state after picker confirm, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已切换会话" {
		t.Fatalf("expected succeeded target picker card, got %#v", got)
	}
}

func TestReconcileSelectedThreadLostOpensLockedWorkspaceTargetPicker(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 8, 0, 0, time.UTC)
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
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-web",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ThreadID:         "thread-web",
	})

	delete(svc.root.Instances["inst-web"].Threads, "thread-web")
	events := svc.reconcileInstanceSurfaceThreads("inst-web")

	surface := svc.root.Surfaces["surface-1"]
	if surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected lost selected thread to return surface to unbound, got %#v", surface)
	}
	var sawNotice bool
	var picker *control.FeishuTargetPickerView
	for _, event := range events {
		if event.SurfaceSessionID == "surface-1" && event.Notice != nil && event.Notice.Code == "selected_thread_lost" {
			sawNotice = true
		}
		if event.SurfaceSessionID == "surface-1" && event.TargetPickerView != nil {
			picker = event.TargetPickerView
		}
	}
	if !sawNotice || picker == nil {
		t.Fatalf("expected lost-thread notice plus locked target picker, got %#v", events)
	}
	if !picker.WorkspaceSelectionLocked || !picker.AllowNewThread || !testutil.SamePath(picker.SelectedWorkspaceKey, "/data/dl/web") {
		t.Fatalf("expected lost-thread picker to stay scoped to current workspace, got %#v", picker)
	}
	if picker.SelectedSessionValue != targetPickerNewThreadValue || picker.ConfirmLabel != "新建会话" || !picker.CanConfirm {
		t.Fatalf("expected lost-thread picker to fall back to new-thread action, got %#v", picker)
	}
}

func TestTargetPickerConfirmValidationUsesCardPatchUpdate(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: "/data/dl/web",
		WorkspaceKey:  "/data/dl/web",
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	initial := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if initial.Page != control.FeishuTargetPickerPageTarget {
		t.Fatalf("expected target picker target page, got %#v", initial)
	}
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         initial.PickerID,
	})
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected mode confirm to refresh picker, got %#v", events)
	}
	if events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected validation refresh to use message-id patch flow, got %#v", events[0])
	}
	got := events[0].TargetPickerView
	if got.Page != control.FeishuTargetPickerPageTarget || got.CanConfirm {
		t.Fatalf("expected target page to stay blocked, got %#v", got)
	}
	if len(got.Messages) == 0 {
		t.Fatalf("expected validation feedback on target picker card, got %#v", got)
	}
}

func TestTargetPickerConfirmGitValidationUsesCardPatchUpdate(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 16, 0, 0, time.UTC)
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
		Kind:             control.ActionWorkspaceNewGit,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected git validation refresh, got %#v", events)
	}
	if events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected git validation to use message-id patch flow, got %#v", events[0])
	}
	got := events[0].TargetPickerView
	if got.Page != control.FeishuTargetPickerPageGit || got.CanConfirm {
		t.Fatalf("expected git page to stay blocked, got %#v", got)
	}
	if len(got.Messages) == 0 && len(got.SourceMessages) == 0 {
		t.Fatalf("expected source validation feedback on picker card, got %#v", got)
	}
}

func TestTargetPickerPendingNewThreadFailureFinishesSameCardAndClearsRuntime(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 17, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewDir,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	svc.RecordTargetPickerMessage("surface-1", view.PickerID, "om-card-1")
	surface := svc.root.Surfaces["surface-1"]
	record := svc.activeTargetPicker(surface)
	if record == nil {
		t.Fatalf("expected active target picker")
	}
	record.LocalDirectoryPath = workspaceRoot

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(events) == 0 || events[0].TargetPickerView == nil {
		t.Fatalf("expected checked target picker card before headless start, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageEditing || !got.LocalDirectoryChecked || got.MessageID != "om-card-1" {
		t.Fatalf("expected first confirm to produce checked local-directory state on same owner card, got %#v", got)
	}

	events = svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(events) == 0 || events[0].TargetPickerView == nil {
		t.Fatalf("expected processing target picker card before headless failure, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing || got.MessageID != "om-card-1" {
		t.Fatalf("expected processing stage to target same owner card, got %#v", got)
	}

	pending := surface.PendingHeadless
	if pending == nil {
		t.Fatalf("expected pending headless launch after processing stage")
	}

	failureEvents := svc.HandleHeadlessLaunchFailed("surface-1", pending.InstanceID, errors.New("dial failed"))
	if len(failureEvents) != 1 || failureEvents[0].TargetPickerView == nil {
		t.Fatalf("expected single failed target picker card after headless failure, got %#v", failureEvents)
	}
	got := failureEvents[0].TargetPickerView
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
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected refreshed picker after stale confirm, got %#v", events)
	}
	got := events[0].TargetPickerView
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
	if view.Page != control.FeishuTargetPickerPageTarget || view.ConfirmLabel != "切换" || view.CanConfirm {
		t.Fatalf("expected /list picker to start directly at target page with empty session, got %#v", view)
	}
	if view.SelectedWorkspaceKey != "/data/dl/web" {
		t.Fatalf("expected initial selection to stay on real workspace, got %#v", view)
	}
	if view.SelectedSessionValue != "" {
		t.Fatalf("expected detached target picker to keep session empty until explicit selection, got %#v", view)
	}
	if _, ok := targetPickerSessionOption(view, targetPickerNewThreadValue); ok {
		t.Fatalf("expected /list target picker not to expose new-thread option, got %#v", view.SessionOptions)
	}
}

func TestTargetPickerListShowsRepoFamilyBranchMeta(t *testing.T) {
	now := time.Date(2026, 4, 26, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	repoRoot := createTargetPickerGitRepo(t)
	worktreeRoot := filepath.Join(t.TempDir(), "feature-worktree")
	runTargetPickerGitCommand(t, repoRoot, "worktree", "add", "-b", "feature/auth", worktreeRoot, "HEAD")

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-main",
		DisplayName:   "main",
		WorkspaceRoot: repoRoot,
		WorkspaceKey:  repoRoot,
		ShortName:     filepath.Base(repoRoot),
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线修复", CWD: repoRoot, Loaded: true, LastUsedAt: now.Add(-2 * time.Minute)},
		},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-feature",
		DisplayName:   "feature",
		WorkspaceRoot: worktreeRoot,
		WorkspaceKey:  worktreeRoot,
		ShortName:     filepath.Base(worktreeRoot),
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-feature": {ThreadID: "thread-feature", Name: "特性开发", CWD: worktreeRoot, Loaded: true, LastUsedAt: now.Add(-1 * time.Minute)},
		},
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	mainOption, ok := targetPickerWorkspaceOption(view, repoRoot)
	if !ok {
		t.Fatalf("expected main repo workspace option, got %#v", view.WorkspaceOptions)
	}
	if mainOption.MetaText != "main" {
		t.Fatalf("main repo meta = %q, want %q", mainOption.MetaText, "main")
	}
	featureOption, ok := targetPickerWorkspaceOption(view, worktreeRoot)
	if !ok {
		t.Fatalf("expected feature worktree option, got %#v", view.WorkspaceOptions)
	}
	if featureOption.MetaText != "feature/auth" {
		t.Fatalf("feature worktree meta = %q, want %q", featureOption.MetaText, "feature/auth")
	}

	locked := singleTargetPickerEvent(t, svc.openLockedWorkspaceTargetPicker(svc.root.Surfaces["surface-1"], worktreeRoot, true))
	if locked.SelectedWorkspaceMeta != "feature/auth" {
		t.Fatalf("locked workspace meta = %q, want %q", locked.SelectedWorkspaceMeta, "feature/auth")
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
	if view.Page != control.FeishuTargetPickerPageTarget || view.ConfirmLabel != "切换" || view.CanConfirm {
		t.Fatalf("expected empty-runtime /list picker to stay on blocked target page, got %#v", view)
	}
	if len(view.Messages) == 0 || !strings.Contains(view.Messages[0].Text, "当前还没有可切换的工作区") {
		t.Fatalf("expected empty-runtime /list picker to explain missing workspaces, got %#v", view.Messages)
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
	if view.Page != control.FeishuTargetPickerPageTarget || view.CanConfirm || view.ConfirmLabel != "切换" {
		t.Fatalf("expected /use picker to start on direct target page, got %#v", view)
	}
	if view.SelectedSessionValue != "" {
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

	updated := openAddWorkspaceLocalDirectoryPage(t, svc, view)
	if updated.Page != control.FeishuTargetPickerPageLocalDirectory {
		t.Fatalf("expected picker to open direct local-directory branch, got %#v", updated)
	}
	if updated.ConfirmLabel != "检查目标目录" || updated.CanConfirm {
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
	if pathView.Title != "选择工作区与会话" || pathView.StageLabel != "目录/选择目录" || pathView.Question != "选择要接入的目录" ||
		pathView.ConfirmLabel != "使用这个目录" || pathView.CancelLabel != "返回" || !strings.Contains(pathView.Hint, "回到上一张卡片") {
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
	updated := openAddWorkspaceLocalDirectoryPage(t, svc, view)
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
	if len(cancelEvents) != 1 || cancelEvents[0].TargetPickerView == nil || cancelEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected cancel to restore target picker via owner-card patch, got %#v", cancelEvents)
	}
	if got := cancelEvents[0].TargetPickerView; got.LocalDirectoryPath != "" || got.CanConfirm {
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
	if len(cancelEvents) != 1 || !cancelEvents[0].InlineReplaceCurrentCard || cancelEvents[0].TargetPickerView == nil {
		t.Fatalf("expected cancel to seal the current owner card inline, got %#v", cancelEvents)
	}
	if got := cancelEvents[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageCancelled || got.StatusTitle != "已取消" {
		t.Fatalf("expected cancelled target picker terminal card, got %#v", got)
	}
}

func TestTargetPickerAddWorkspacePathPickerConfirmBackfillsKnownWorkspaceAndKeepsMainConfirmBlockedUntilCheck(t *testing.T) {
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
	updated := openAddWorkspaceLocalDirectoryPage(t, svc, view)
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
	if len(confirmEvents) != 1 || confirmEvents[0].TargetPickerView == nil || confirmEvents[0].InlineReplaceCurrentCard {
		t.Fatalf("expected path confirm to restore target card via owner-card patch, got %#v", confirmEvents)
	}
	got := confirmEvents[0].TargetPickerView
	if !testutil.SamePath(got.LocalDirectoryPath, workspaceRoot) {
		t.Fatalf("expected known workspace path to backfill, got %#v", got)
	}
	if got.CanConfirm || got.LocalDirectoryChecked || got.ConfirmLabel != "检查目标目录" {
		t.Fatalf("expected path confirm to require explicit check before continue, got %#v", got)
	}
}

func TestTargetPickerAddWorkspaceLocalDirectoryPathPickerKeepsBusyWorkspaceDirectoryVisible(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 47, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	parent := t.TempDir()
	busyDir := filepath.Join(parent, "busy")
	freeDir := filepath.Join(parent, "free")
	for _, dir := range []string{busyDir, freeDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	surface1 := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if !svc.claimWorkspace(surface1, parent) {
		t.Fatalf("expected surface-1 to claim parent workspace")
	}
	surface2 := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-2",
		ChatID:           "chat-2",
		ActorUserID:      "user-2",
	})
	if !svc.claimWorkspace(surface2, busyDir) {
		t.Fatalf("expected surface-2 to claim busy directory")
	}

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	addMode := openAddWorkspaceLocalDirectoryPage(t, svc, view)
	pathView := singlePathPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerOpenPathPicker,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          addMode.PickerID,
		TargetPickerValue: control.FeishuTargetPickerPathFieldLocalDirectory,
	}))

	var directories []string
	for _, entry := range pathView.Entries {
		if entry.Kind == control.PathPickerEntryDirectory {
			directories = append(directories, entry.Name)
		}
	}
	if !slicesContain(directories, "busy") || !slicesContain(directories, "free") {
		t.Fatalf("expected parent-directory picker to keep both busy and free directories visible, got %v", directories)
	}
}

func TestTargetPickerConfirmAddWorkspaceLocalDirectoryStartsHeadlessForUnknownWorkspace(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 48, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	addMode := openAddWorkspaceLocalDirectoryPage(t, svc, view)
	surface := svc.root.Surfaces["surface-1"]
	record := svc.activeTargetPicker(surface)
	record.LocalDirectoryPath = workspaceRoot

	checkEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         addMode.PickerID,
	})
	if surface.PendingHeadless != nil {
		t.Fatalf("expected first confirm to only validate same card, got %#v", surface.PendingHeadless)
	}
	if len(checkEvents) != 1 || checkEvents[0].TargetPickerView == nil {
		t.Fatalf("expected checked target picker card before headless start, got %#v", checkEvents)
	}
	checked := checkEvents[0].TargetPickerView
	if !checked.LocalDirectoryChecked || checked.ConfirmLabel != "接入并继续" || !checked.CanConfirm || !testutil.SamePath(checked.LocalDirectoryFinalPath, workspaceRoot) {
		t.Fatalf("expected first confirm to produce checked local-directory state, got %#v", checked)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         addMode.PickerID,
	})
	if surface.PendingHeadless == nil || !surface.PendingHeadless.PrepareNewThread || !testutil.SamePath(surface.PendingHeadless.ThreadCWD, workspaceRoot) {
		t.Fatalf("expected unknown local directory to start headless workspace preparation, got %#v", surface.PendingHeadless)
	}
	if len(confirmEvents) == 0 || confirmEvents[0].TargetPickerView == nil {
		t.Fatalf("expected processing card before headless completion, got %#v", confirmEvents)
	}
	if got := confirmEvents[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing || got.StatusTitle != "正在接入工作区" {
		t.Fatalf("expected processing target picker card for unknown local directory, got %#v", got)
	}
	var sawStart bool
	for _, event := range confirmEvents {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			sawStart = true
		}
	}
	if !sawStart {
		t.Fatalf("expected local-directory confirm to dispatch headless start, got %#v", confirmEvents)
	}

	pending := surface.PendingHeadless
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    pending.InstanceID,
		DisplayName:   "headless",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		Source:        "headless",
		Managed:       true,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	connectEvents := svc.ApplyInstanceConnected(pending.InstanceID)
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected connected local-directory workspace to enter new-thread-ready, got %#v", surface)
	}
	if len(connectEvents) != 1 || connectEvents[0].TargetPickerView == nil {
		t.Fatalf("expected same-card success after local-directory headless connect, got %#v", connectEvents)
	}
	if got := connectEvents[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded target picker card after local-directory headless connect, got %#v", got)
	}
}

func TestTargetPickerConfirmAddWorkspaceLocalDirectoryAcceptsSymlinkedKnownWorkspace(t *testing.T) {
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
	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionListInstances,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	addMode := openAddWorkspaceLocalDirectoryPage(t, svc, view)
	surface := svc.root.Surfaces["surface-1"]
	record := svc.activeTargetPicker(surface)
	record.LocalDirectoryPath = linkWorkspace

	checkEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         addMode.PickerID,
	})
	if len(checkEvents) != 1 || checkEvents[0].TargetPickerView == nil {
		t.Fatalf("expected same-card checked state before attach, got %#v", checkEvents)
	}
	if got := checkEvents[0].TargetPickerView; !got.LocalDirectoryChecked || got.ConfirmLabel != "接入并继续" {
		t.Fatalf("expected first confirm to switch symlinked workspace into checked state, got %#v", got)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         addMode.PickerID,
	})
	if surface.PendingHeadless != nil || surface.AttachedInstanceID != "inst-web" {
		t.Fatalf("expected symlinked known workspace to attach immediately without headless start, got %#v", surface)
	}
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, realWorkspace) {
		t.Fatalf("expected symlinked known workspace to enter new-thread-ready, got %#v", surface)
	}
	if len(confirmEvents) != 1 || confirmEvents[0].TargetPickerView == nil {
		t.Fatalf("expected same-card success after symlinked workspace confirm, got %#v", confirmEvents)
	}
	if got := confirmEvents[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected symlinked known workspace to finish with success, got %#v", got)
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
		Kind:             control.ActionWorkspaceNewGit,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if view.Page != control.FeishuTargetPickerPageGit {
		t.Fatalf("expected direct git page, got %#v", view)
	}
	if view.CanConfirm {
		t.Fatalf("expected git page to stay blocked without git executable, got %#v", view)
	}
	if len(view.SourceMessages) == 0 || !strings.Contains(view.SourceMessages[0].Text, "git") {
		t.Fatalf("expected direct git page to explain missing git, got %#v", view.SourceMessages)
	}
}

func TestTargetPickerWorktreeConfirmDispatchesCreateCommand(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 51, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := createTargetPickerGitRepo(t)
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
		Kind:             control.ActionWorkspaceNewWorktree,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if view.Page != control.FeishuTargetPickerPageWorktree {
		t.Fatalf("expected direct worktree page, got %#v", view)
	}
	if normalizeWorkspaceClaimKey(view.SelectedWorkspaceKey) != normalizeWorkspaceClaimKey(workspaceRoot) {
		t.Fatalf("expected worktree page to default to current workspace, got %#v", view)
	}

	blocked := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
	})
	if len(blocked) != 1 || blocked[0].TargetPickerView == nil {
		t.Fatalf("expected worktree validation refresh, got %#v", blocked)
	}
	if got := blocked[0].TargetPickerView; got.Page != control.FeishuTargetPickerPageWorktree || got.CanConfirm {
		t.Fatalf("expected worktree page to stay blocked, got %#v", got)
	}
	if got := blocked[0].TargetPickerView; len(got.Messages) == 0 && len(got.SourceMessages) == 0 {
		t.Fatalf("expected worktree validation feedback, got %#v", got)
	}

	confirmEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerConfirm,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
		RequestAnswers: map[string][]string{
			control.FeishuTargetPickerWorktreeBranchFieldName:    {"feat/login"},
			control.FeishuTargetPickerWorktreeDirectoryFieldName: {"web-login"},
		},
	})
	if len(confirmEvents) != 2 || confirmEvents[0].TargetPickerView == nil || confirmEvents[1].DaemonCommand == nil {
		t.Fatalf("expected processing card plus daemon command, got %#v", confirmEvents)
	}
	if got := confirmEvents[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing || got.StatusTitle != "正在创建 Worktree 工作区" {
		t.Fatalf("expected worktree processing card, got %#v", got)
	}
	command := confirmEvents[1].DaemonCommand
	if command.Kind != control.DaemonCommandGitWorkspaceWorktreeCreate || command.PickerID != view.PickerID {
		t.Fatalf("unexpected worktree create daemon command: %#v", command)
	}
	if !testutil.SamePath(command.WorkspaceKey, workspaceRoot) || command.BranchName != "feat/login" || command.DirectoryName != "web-login" {
		t.Fatalf("unexpected worktree create daemon command payload: %#v", command)
	}

	blockedInput := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "继续说话",
	})
	if len(blockedInput) != 1 || blockedInput[0].Notice == nil || blockedInput[0].Notice.Code != "target_picker_processing" {
		t.Fatalf("expected ordinary input to be blocked during worktree create, got %#v", blockedInput)
	}
	if got := blockedInput[0].Notice.Text; got != "当前正在创建 Worktree 工作区，请等待完成或取消；如需查看状态，可继续使用 /status。" {
		t.Fatalf("unexpected worktree processing blocker text: %q", got)
	}
}

func TestTargetPickerWorktreeOnlyListsGitWorkspaces(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 51, 30, 0, time.UTC)
	svc := newServiceForTest(&now)
	gitWorkspace := createTargetPickerGitRepo(t)
	plainWorkspace := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-git",
		DisplayName:   "git",
		WorkspaceRoot: gitWorkspace,
		WorkspaceKey:  gitWorkspace,
		ShortName:     "git",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-plain",
		DisplayName:   "plain",
		WorkspaceRoot: plainWorkspace,
		WorkspaceKey:  plainWorkspace,
		ShortName:     "plain",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     plainWorkspace,
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewWorktree,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if len(view.WorkspaceOptions) != 1 {
		t.Fatalf("expected only git workspace option, got %#v", view.WorkspaceOptions)
	}
	if !testutil.SamePath(view.WorkspaceOptions[0].Value, gitWorkspace) {
		t.Fatalf("expected git workspace option, got %#v", view.WorkspaceOptions)
	}
	if !testutil.SamePath(view.SelectedWorkspaceKey, gitWorkspace) {
		t.Fatalf("expected worktree page to fall back to git workspace, got %#v", view)
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
	gitSource := openAddWorkspaceGitPage(t, svc, view)
	if gitSource.CanConfirm {
		t.Fatalf("expected git import confirm to stay disabled until required fields are complete, got %#v", gitSource)
	}
	if !gitSource.ConfirmValidatesOnSubmit {
		t.Fatalf("expected git import page to validate on submit, got %#v", gitSource)
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
	if invalid.CanConfirm {
		t.Fatalf("expected invalid submit to keep confirm disabled after inline validation, got %#v", invalid)
	}
	if !invalid.ConfirmValidatesOnSubmit {
		t.Fatalf("expected invalid git submit to stay on submit-time validation path, got %#v", invalid)
	}
	if invalid.GitRepoURL != "https://github.com/kxn/codex-remote-feishu.git" || invalid.GitDirectoryName != "test1122" {
		t.Fatalf("expected invalid submit to preserve draft answers on main card, got %#v", invalid)
	}
	if len(invalid.SourceMessages) == 0 || invalid.SourceMessages[0].Level != control.FeishuTargetPickerMessageDanger ||
		!strings.Contains(invalid.SourceMessages[0].Text, "落地目录") {
		t.Fatalf("expected inline blocking error on main card source messages, got messages=%#v source=%#v", invalid.Messages, invalid.SourceMessages)
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
	gitSource := openAddWorkspaceGitPage(t, svc, view)
	if gitSource.CanConfirm {
		t.Fatalf("expected git source confirm to stay disabled before parent directory / repo are complete, got %#v", gitSource)
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
	if pathView.Title != "选择工作区与会话" || pathView.StageLabel != "Git/选择目录" || pathView.Question != "选择仓库要落到哪个本地父目录" || pathView.CancelLabel != "返回" {
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
	if len(confirmEvents) != 2 || confirmEvents[0].TargetPickerView == nil || confirmEvents[1].DaemonCommand == nil {
		t.Fatalf("expected processing card plus daemon command, got %#v", confirmEvents)
	}
	processing := confirmEvents[0].TargetPickerView
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
	gitSource := openAddWorkspaceGitPage(t, svc, view)
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
	if len(confirmEvents) == 0 || confirmEvents[0].TargetPickerView == nil || confirmEvents[0].TargetPickerView.Stage != control.FeishuTargetPickerStageProcessing {
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
		Source:              control.TargetPickerRequestSourceGit,
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
	if len(events) != 2 || events[0].TargetPickerView == nil || events[1].DaemonCommand == nil {
		t.Fatalf("expected cancelled same-card result plus daemon cancel, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageCancelled || got.StatusTitle != "已取消导入" || got.MessageID != "om-card-1" {
		t.Fatalf("expected cancelled git-import terminal card on original owner card, got %#v", got)
	}
	if got := events[1].DaemonCommand; got.Kind != control.DaemonCommandGitWorkspaceImportCancel || got.PickerID != record.PickerID {
		t.Fatalf("expected git import cancel daemon command, got %#v", got)
	}
	if svc.activeTargetPicker(surface) != nil || svc.activeOwnerCardFlow(surface) != nil {
		t.Fatalf("expected cancel to clear target picker runtime, got %#v", svc.SurfaceUIRuntime("surface-1"))
	}
}

func TestTargetPickerCancelWorktreeProcessingSealsCardAndDispatchesCancel(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 59, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	record := &activeTargetPickerRecord{
		PickerID:             "picker-1",
		OwnerUserID:          "user-1",
		Source:               control.TargetPickerRequestSourceWorktree,
		Stage:                control.FeishuTargetPickerStageProcessing,
		PendingKind:          targetPickerPendingWorktreeCreate,
		PendingWorkspaceKey:  "/data/dl/projects/repo-login",
		SelectedWorkspaceKey: "/data/dl/projects/repo",
		WorktreeBranchName:   "feat/login",
		WorktreeFinalPath:    "/data/dl/projects/repo-login",
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
	if len(events) != 2 || events[0].TargetPickerView == nil || events[1].DaemonCommand == nil {
		t.Fatalf("expected cancelled same-card result plus daemon cancel, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageCancelled || got.StatusTitle != "已取消创建" || got.MessageID != "om-card-1" {
		t.Fatalf("expected cancelled worktree terminal card on original owner card, got %#v", got)
	}
	if got := events[1].DaemonCommand; got.Kind != control.DaemonCommandGitWorkspaceWorktreeCancel || got.PickerID != record.PickerID {
		t.Fatalf("expected worktree cancel daemon command, got %#v", got)
	}
	if svc.activeTargetPicker(surface) != nil || svc.activeOwnerCardFlow(surface) != nil {
		t.Fatalf("expected cancel to clear target picker runtime, got %#v", svc.SurfaceUIRuntime("surface-1"))
	}
}

func openAddWorkspaceLocalDirectoryPage(t *testing.T, svc *Service, view *control.FeishuTargetPickerView) *control.FeishuTargetPickerView {
	t.Helper()
	_ = view
	return singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewDir,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
}

func openAddWorkspaceGitPage(t *testing.T, svc *Service, view *control.FeishuTargetPickerView) *control.FeishuTargetPickerView {
	t.Helper()
	_ = view
	return singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionWorkspaceNewGit,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
}

func createTargetPickerGitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable in test environment")
	}
	repoRoot := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		t.Fatalf("mkdir repo root: %v", err)
	}
	runTargetPickerGitCommand(t, repoRoot, "init", "-q")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write repo file: %v", err)
	}
	runTargetPickerGitCommand(t, repoRoot, "add", "README.md")
	runTargetPickerGitCommand(t, repoRoot, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "init")
	runTargetPickerGitCommand(t, repoRoot, "branch", "-M", "main")
	return repoRoot
}

func runTargetPickerGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GCM_INTERACTIVE=Never",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
