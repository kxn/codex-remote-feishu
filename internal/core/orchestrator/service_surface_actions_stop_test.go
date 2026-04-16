package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStopInterruptsActiveTurnAndDiscardsQueuedMessages(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		ActiveThreadID:          "thread-1",
		ActiveTurnID:            "turn-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.root.Surfaces["surface-1"].QueuedQueueItemIDs = []string{"queue-1"}
	svc.root.Surfaces["surface-1"].QueueItems["queue-1"] = &state.QueueItemRecord{
		ID:              "queue-1",
		SourceMessageID: "msg-1",
		Status:          state.QueueItemQueued,
	}
	svc.root.Surfaces["surface-1"].StagedImages["img-1"] = &state.StagedImageRecord{
		ImageID:         "img-1",
		SourceMessageID: "msg-img",
		State:           state.ImageStaged,
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 4 {
		t.Fatalf("expected interrupt + 2 discard events + notice, got %#v", events)
	}
	if events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnInterrupt {
		t.Fatalf("expected interrupt command, got %#v", events[0])
	}
	if events[3].Notice == nil || events[3].Notice.Code != "stop_requested" {
		t.Fatalf("expected stop_requested notice, got %#v", events[3])
	}
}

func TestUnknownSideTurnLifecycleDoesNotDisturbTrackedRemoteTurn(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-main",
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线程", CWD: "/data/dl/droid"},
			"thread-side": {ThreadID: "thread-side", Name: "旁路线程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-main"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-main",
		Text:             "开始主任务",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-main",
		TurnID:    "turn-main",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	surface := svc.root.Surfaces["surface-1"]
	activeQueueID := surface.ActiveQueueItemID
	if activeQueueID == "" {
		t.Fatalf("expected tracked remote turn to keep an active queue item")
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-side",
		TurnID:    "turn-side",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if svc.root.Instances["inst-1"].ActiveTurnID != "turn-main" || svc.root.Instances["inst-1"].ActiveThreadID != "thread-main" {
		t.Fatalf("expected side turn start not to steal tracked remote turn, got thread=%q turn=%q", svc.root.Instances["inst-1"].ActiveThreadID, svc.root.Instances["inst-1"].ActiveTurnID)
	}
	if surface.ActiveQueueItemID != activeQueueID {
		t.Fatalf("expected side turn start not to disturb active queue item, before=%q after=%q", activeQueueID, surface.ActiveQueueItemID)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-side",
		TurnID:    "turn-side",
		Status:    "failed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if svc.root.Instances["inst-1"].ActiveTurnID != "turn-main" || svc.root.Instances["inst-1"].ActiveThreadID != "thread-main" {
		t.Fatalf("expected side turn completion not to clear tracked remote turn, got thread=%q turn=%q", svc.root.Instances["inst-1"].ActiveThreadID, svc.root.Instances["inst-1"].ActiveTurnID)
	}
	if binding := svc.activeRemote["inst-1"]; binding == nil || binding.TurnID != "turn-main" || binding.ThreadID != "thread-main" {
		t.Fatalf("expected active remote binding to remain on main turn, got %#v", binding)
	}
}

func TestStopUsesBoundRemoteTurnWhenInstanceActiveTurnMissing(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-main",
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-main"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-main",
		Text:             "开始主任务",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-main",
		TurnID:    "turn-main",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})

	svc.root.Instances["inst-1"].ActiveTurnID = ""
	svc.root.Instances["inst-1"].ActiveThreadID = ""

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 2 {
		t.Fatalf("expected interrupt command + notice, got %#v", events)
	}
	if events[0].Command == nil || events[0].Command.Kind != agentproto.CommandTurnInterrupt {
		t.Fatalf("expected interrupt command, got %#v", events[0])
	}
	if events[0].Command.Target.ThreadID != "thread-main" || events[0].Command.Target.TurnID != "turn-main" {
		t.Fatalf("expected stop to target bound remote turn, got %#v", events[0].Command.Target)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "stop_requested" {
		t.Fatalf("expected stop_requested notice, got %#v", events)
	}
}

func TestStopWithoutActiveTurnReturnsNotice(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-2",
		Threads: map[string]*state.ThreadRecord{
			"thread-2": {ThreadID: "thread-2", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "stop_no_active_turn" {
		t.Fatalf("expected stop_no_active_turn notice, got %#v", events)
	}
}
