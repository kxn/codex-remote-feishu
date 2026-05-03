package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestTargetPickerConfirmPersistedCrossWorkspaceThreadStartsHeadlessRestore(t *testing.T) {
	now := time.Date(2026, 5, 3, 15, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "profile-a", "", "")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-claude-a",
		DisplayName:     "repo-a",
		WorkspaceRoot:   "/data/dl/repo-a",
		WorkspaceKey:    "/data/dl/repo-a",
		ShortName:       "repo-a",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: "profile-a",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-a": {ThreadID: "thread-a", Name: "当前会话", CWD: "/data/dl/repo-a", Loaded: true},
		},
	})
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recentByBackend: map[agentproto.Backend][]state.ThreadRecord{
			agentproto.BackendClaude: {
				{
					ThreadID:   "thread-b",
					Name:       "其他工作区会话",
					Preview:    "来自 persisted catalog",
					CWD:        "/data/dl/repo-b",
					Loaded:     true,
					LastUsedAt: now.Add(-1 * time.Minute),
				},
			},
		},
		byIDByBackend: map[agentproto.Backend]map[string]state.ThreadRecord{
			agentproto.BackendClaude: {
				"thread-b": {
					ThreadID:   "thread-b",
					Name:       "其他工作区会话",
					Preview:    "来自 persisted catalog",
					CWD:        "/data/dl/repo-b",
					Loaded:     true,
					LastUsedAt: now.Add(-1 * time.Minute),
				},
			},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		WorkspaceKey:     "/data/dl/repo-a",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-a",
	})

	view := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowAllThreads,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	}))
	if _, ok := targetPickerWorkspaceOption(view, "/data/dl/repo-b"); !ok {
		t.Fatalf("expected target picker to expose recoverable repo-b workspace, got %#v", view.WorkspaceOptions)
	}
	updated := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerSelectWorkspace,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         view.PickerID,
		WorkspaceKey:     "/data/dl/repo-b",
	}))
	if _, ok := targetPickerSessionOption(updated, targetPickerThreadValue("thread-b")); !ok {
		t.Fatalf("expected target picker to expose recoverable repo-b thread after workspace switch, got %#v", updated.SessionOptions)
	}
	events := svc.ApplySurfaceAction(control.Action{
		Kind:              control.ActionTargetPickerConfirm,
		SurfaceSessionID:  "surface-1",
		ChatID:            "chat-1",
		ActorUserID:       "user-1",
		PickerID:          updated.PickerID,
		WorkspaceKey:      "/data/dl/repo-b",
		TargetPickerValue: targetPickerThreadValue("thread-b"),
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.PendingHeadless == nil {
		t.Fatalf("expected target picker cross-workspace restore to wait on a new headless instance, got %#v", surface)
	}
	if surface.PendingHeadless.ThreadID != "thread-b" || !testutil.SamePath(surface.PendingHeadless.ThreadCWD, "/data/dl/repo-b") {
		t.Fatalf("expected target picker restore to carry repo-b thread target, got %#v", surface.PendingHeadless)
	}
	if len(events) == 0 || events[0].TargetPickerView == nil {
		t.Fatalf("expected target picker processing state before headless restore, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing || got.StatusTitle != "正在切换工作区 / 会话" {
		t.Fatalf("expected target picker to stay in processing until matching headless comes up, got %#v", got)
	}
	var sawStartHeadless bool
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			sawStartHeadless = true
			break
		}
	}
	if !sawStartHeadless {
		t.Fatalf("expected target picker cross-workspace restore to start headless launch, got %#v", events)
	}
}
