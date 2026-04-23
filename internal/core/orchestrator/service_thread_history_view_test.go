package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestApplySurfaceActionHistoryStartsQueryForCurrentThread(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.RouteMode = state.RouteModePinned
	surface.SelectedThreadID = "thread-1"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowHistory,
		SurfaceSessionID: "surface-1",
		Text:             "/history",
	})
	if len(events) != 2 {
		t.Fatalf("expected loading view + daemon command, got %#v", events)
	}
	if events[0].Kind != eventcontract.EventFeishuThreadHistory || events[0].ThreadHistoryView == nil || !events[0].ThreadHistoryView.Loading {
		t.Fatalf("expected loading history view, got %#v", events[0])
	}
	if len(events[0].ThreadHistoryView.NoticeSections) != 1 {
		t.Fatalf("expected loading history view to expose one notice section, got %#v", events[0].ThreadHistoryView)
	}
	if events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandThreadHistoryRead || events[1].DaemonCommand.ThreadID != "thread-1" {
		t.Fatalf("expected history daemon command, got %#v", events[1])
	}
	if record := svc.activeThreadHistory(surface); record == nil || record.ThreadID != "thread-1" {
		t.Fatalf("expected active history record, got %#v", record)
	}
	if flow := svc.activeOwnerCardFlow(surface); flow == nil || flow.Kind != ownerCardFlowKindThreadHistory || flow.FlowID == "" || flow.Phase != ownerCardFlowPhaseLoading {
		t.Fatalf("expected active owner flow for history, got %#v", flow)
	}
}

func TestHandleSurfaceThreadHistoryLoadedBuildsNewestFirstList(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:   "inst-1",
		WorkspaceKey: "/data/dl/droid",
		ShortName:    "droid",
		Online:       true,
		ActiveTurnID: "turn-3",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.RouteMode = state.RouteModePinned
	surface.SelectedThreadID = "thread-1"
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindThreadHistory, "history-1", "user-1", now, time.Minute, ownerCardFlowPhaseLoading))
	svc.setActiveThreadHistory(surface, &activeThreadHistoryRecord{
		ThreadID: "thread-1",
		ViewMode: control.FeishuThreadHistoryViewList,
	})
	svc.RecordSurfaceThreadHistory("surface-1", agentproto.ThreadHistoryRecord{
		Thread: agentproto.ThreadSnapshotRecord{ThreadID: "thread-1", Name: "修复登录流程"},
		Turns: []agentproto.ThreadHistoryTurnRecord{
			{
				TurnID:      "turn-1",
				Status:      "completed",
				CompletedAt: now.Add(-3 * time.Minute),
				Items: []agentproto.ThreadHistoryItemRecord{
					{Kind: "user_message", Text: "第一轮输入"},
					{Kind: "agent_message", Text: "第一轮回复"},
				},
			},
			{
				TurnID:       "turn-2",
				Status:       "failed",
				CompletedAt:  now.Add(-2 * time.Minute),
				ErrorMessage: "第二轮失败",
				Items: []agentproto.ThreadHistoryItemRecord{
					{Kind: "user_message", Text: "第二轮输入"},
				},
			},
			{
				TurnID:    "turn-3",
				Status:    "running",
				StartedAt: now.Add(-time.Minute),
				Items: []agentproto.ThreadHistoryItemRecord{
					{Kind: "user_message", Text: "第三轮输入"},
				},
			},
		},
	})

	events := svc.HandleSurfaceThreadHistoryLoaded("surface-1")
	if len(events) != 1 || events[0].ThreadHistoryView == nil {
		t.Fatalf("expected one resolved history view, got %#v", events)
	}
	view := events[0].ThreadHistoryView
	if view.Loading || view.Detail != nil || view.TurnCount != 3 {
		t.Fatalf("unexpected history list view: %#v", view)
	}
	if view.CurrentTurnLabel != "第 3 轮" {
		t.Fatalf("current turn label = %q, want %q", view.CurrentTurnLabel, "第 3 轮")
	}
	if len(view.TurnOptions) != 3 {
		t.Fatalf("expected three turn options, got %#v", view.TurnOptions)
	}
	if view.TurnOptions[0].TurnID != "turn-3" || !strings.Contains(view.TurnOptions[0].Label, "#3") || !view.TurnOptions[0].Current {
		t.Fatalf("expected newest turn first with original ordinal, got %#v", view.TurnOptions[0])
	}
	if flow := svc.activeOwnerCardFlow(surface); flow == nil || flow.Phase != ownerCardFlowPhaseResolved || flow.Revision < 2 {
		t.Fatalf("expected resolved owner flow after load, got %#v", flow)
	}
}

func TestHandleSurfaceThreadHistoryLoadedBuildsDetailNavigation(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:   "inst-1",
		WorkspaceKey: "/data/dl/droid",
		ShortName:    "droid",
		Online:       true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	surface.RouteMode = state.RouteModePinned
	surface.SelectedThreadID = "thread-1"
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindThreadHistory, "history-1", "user-1", now, time.Minute, ownerCardFlowPhaseLoading))
	svc.setActiveThreadHistory(surface, &activeThreadHistoryRecord{
		ThreadID: "thread-1",
		ViewMode: control.FeishuThreadHistoryViewDetail,
		TurnID:   "turn-2",
	})
	svc.RecordSurfaceThreadHistory("surface-1", agentproto.ThreadHistoryRecord{
		Thread: agentproto.ThreadSnapshotRecord{ThreadID: "thread-1", Name: "修复登录流程"},
		Turns: []agentproto.ThreadHistoryTurnRecord{
			{
				TurnID:      "turn-1",
				Status:      "completed",
				CompletedAt: now.Add(-3 * time.Minute),
				Items: []agentproto.ThreadHistoryItemRecord{
					{Kind: "user_message", Text: "第一轮输入"},
					{Kind: "agent_message", Text: "第一轮回复"},
				},
			},
			{
				TurnID:       "turn-2",
				Status:       "failed",
				CompletedAt:  now.Add(-2 * time.Minute),
				ErrorMessage: "第二轮失败",
				Items: []agentproto.ThreadHistoryItemRecord{
					{Kind: "user_message", Text: "第二轮输入"},
				},
			},
			{
				TurnID:      "turn-3",
				Status:      "completed",
				CompletedAt: now.Add(-time.Minute),
				Items: []agentproto.ThreadHistoryItemRecord{
					{Kind: "user_message", Text: "第三轮输入"},
					{Kind: "agent_message", Text: "第三轮回复"},
				},
			},
		},
	})

	events := svc.HandleSurfaceThreadHistoryLoaded("surface-1")
	if len(events) != 1 || events[0].ThreadHistoryView == nil || events[0].ThreadHistoryView.Detail == nil {
		t.Fatalf("expected one detail history view, got %#v", events)
	}
	detail := events[0].ThreadHistoryView.Detail
	if detail.TurnID != "turn-2" || detail.Ordinal != 2 {
		t.Fatalf("unexpected detail view: %#v", detail)
	}
	if detail.PrevTurnID != "turn-3" || detail.NextTurnID != "turn-1" || detail.ReturnPage != 0 {
		t.Fatalf("unexpected detail navigation: %#v", detail)
	}
	if len(detail.Inputs) != 1 || detail.Inputs[0] != "第二轮输入" || detail.ErrorText != "第二轮失败" {
		t.Fatalf("unexpected detail payload: %#v", detail)
	}
}
