package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDetachAfterTransportDegradedDetachesImmediately(t *testing.T) {
	now := time.Date(2026, 4, 4, 12, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyInstanceTransportDegraded("inst-1", true)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.Abandoning {
		t.Fatalf("expected degraded offline detach to finalize immediately, got %#v", surface)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected detach to release instance claim, got %#v", claim)
	}
	var sawDetached, sawInterrupt bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "detached" {
			sawDetached = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandTurnInterrupt {
			sawInterrupt = true
		}
	}
	if !sawDetached || sawInterrupt {
		t.Fatalf("expected immediate detach notice without interrupt, got %#v", events)
	}
}

func TestDetachDuringPreStartRemoteDispatchDetachesImmediatelyAndClearsResidualState(t *testing.T) {
	now := time.Date(2026, 4, 29, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	dispatched := false
	for _, event := range first {
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			dispatched = true
			break
		}
	}
	if !dispatched {
		t.Fatalf("expected initial prompt dispatch, got %#v", first)
	}

	surface := svc.root.Surfaces["surface-1"]
	activeID := surface.ActiveQueueItemID
	if activeID == "" {
		t.Fatalf("expected dispatching active queue item, got %#v", surface)
	}
	if binding := svc.turns.pendingRemote["inst-1"]; binding == nil || binding.SurfaceSessionID != "surface-1" || binding.TurnID != "" {
		t.Fatalf("expected pending pre-start remote binding without turn ID, got %#v", binding)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "surface-1",
	})

	surface = svc.root.Surfaces["surface-1"]
	if surface.AttachedInstanceID != "" || surface.Abandoning || surface.ActiveQueueItemID != "" {
		t.Fatalf("expected immediate detach from pre-start dispatch, got %#v", surface)
	}
	if claim := svc.instanceClaims["inst-1"]; claim != nil {
		t.Fatalf("expected detach to release instance claim, got %#v", claim)
	}
	if svc.turns.pendingRemote["inst-1"] != nil || svc.turns.activeRemote["inst-1"] != nil {
		t.Fatalf("expected remote ownership to clear, pending=%#v active=%#v", svc.turns.pendingRemote["inst-1"], svc.turns.activeRemote["inst-1"])
	}
	item := surface.QueueItems[activeID]
	if item == nil || item.Status != state.QueueItemFailed {
		t.Fatalf("expected active queue item to fail during detach cleanup, got %#v", item)
	}

	var sawDetached, sawInterrupt, sawFailedPending bool
	for _, event := range events {
		if event.Notice != nil && event.Notice.Code == "detached" {
			sawDetached = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandTurnInterrupt {
			sawInterrupt = true
		}
		if event.PendingInput != nil && event.PendingInput.QueueItemID == activeID && event.PendingInput.Status == string(state.QueueItemFailed) {
			sawFailedPending = true
		}
	}
	if !sawDetached || sawInterrupt || !sawFailedPending {
		t.Fatalf("expected failed pending state + detached notice without interrupt, got %#v", events)
	}

	modeEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode claude",
	})
	if len(modeEvents) != 1 || modeEvents[0].Notice == nil || modeEvents[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected follow-up mode switch to be unblocked after detach, got %#v", modeEvents)
	}
}

func TestStopWhileTransportDegradedReportsInstanceOffline(t *testing.T) {
	now := time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-1",
		Text:             "你好",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplyInstanceTransportDegraded("inst-1", true)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionStop,
		SurfaceSessionID: "surface-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "stop_instance_offline" {
		t.Fatalf("expected stop_instance_offline notice, got %#v", events)
	}
	if strings.Contains(events[0].Notice.Text, "已发送停止请求") {
		t.Fatalf("expected offline stop notice instead of sent interrupt, got %#v", events[0].Notice)
	}
}
