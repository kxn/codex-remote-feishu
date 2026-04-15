package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSteerAllCommandNoEligibleQueueReturnsNoopNotice(t *testing.T) {
	now := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "steer_all_noop" {
		t.Fatalf("expected steer_all_noop notice, got %#v", events)
	}
}

func TestSteerAllCommandDispatchesSingleSteerWithAllEligibleQueuedInputs(t *testing.T) {
	now := time.Date(2026, 4, 14, 10, 5, 0, 0, time.UTC)
	svc := newSteerAllServiceFixture(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-steer-all",
	})

	if len(events) != 2 || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected notice + single steer command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "steer_all_requested" {
		t.Fatalf("expected steer_all_requested notice, got %#v", events[0])
	}
	command := events[1].Command
	if command.Target.ThreadID != "thread-1" || command.Target.TurnID != "turn-1" {
		t.Fatalf("unexpected steer target: %#v", command.Target)
	}
	if len(command.Prompt.Inputs) != 2 || command.Prompt.Inputs[0].Text != "补充信息一" || command.Prompt.Inputs[1].Text != "补充信息二" {
		t.Fatalf("expected merged queued inputs, got %#v", command.Prompt.Inputs)
	}

	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected queued items removed after steer-all, got %#v", surface.QueuedQueueItemIDs)
	}
	binding := svc.pendingSteers["queue-2"]
	if binding == nil {
		t.Fatalf("expected steer binding for primary queued item, got %#v", svc.pendingSteers)
	}
	if len(binding.QueueItemIDs) != 2 || binding.QueueItemIDs[0] != "queue-2" || binding.QueueItemIDs[1] != "queue-3" {
		t.Fatalf("unexpected steer binding queue ids: %#v", binding)
	}
}

func TestSteerAllCommandAcceptedMarksAllQueuedItemsSteered(t *testing.T) {
	now := time.Date(2026, 4, 14, 10, 10, 0, 0, time.UTC)
	svc := newSteerAllServiceFixture(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected steer command, got %#v", events)
	}

	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-all-1")
	accepted := svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{CommandID: "cmd-steer-all-1", Accepted: true})
	if len(accepted) != 2 {
		t.Fatalf("expected two pending-input acknowledgements, got %#v", accepted)
	}
	for _, event := range accepted {
		if event.PendingInput == nil || !event.PendingInput.QueueOff || !event.PendingInput.ThumbsUp || event.PendingInput.Status != string(state.QueueItemSteered) {
			t.Fatalf("unexpected steer-all accepted projection: %#v", accepted)
		}
	}

	surface := svc.root.Surfaces["surface-1"]
	if surface.QueueItems["queue-2"].Status != state.QueueItemSteered || surface.QueueItems["queue-3"].Status != state.QueueItemSteered {
		t.Fatalf("expected all steered, got queue-2=%#v queue-3=%#v", surface.QueueItems["queue-2"], surface.QueueItems["queue-3"])
	}
	if len(svc.pendingSteers) != 0 {
		t.Fatalf("expected pending steer bindings cleared, got %#v", svc.pendingSteers)
	}
}

func TestSteerAllCommandRejectedRestoresOriginalQueueOrder(t *testing.T) {
	now := time.Date(2026, 4, 14, 10, 15, 0, 0, time.UTC)
	svc := newSteerAllServiceFixture(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected steer command, got %#v", events)
	}

	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-all-2")
	rejected := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-steer-all-2",
		Accepted:  false,
		Error:     "steer rejected",
	})
	if len(rejected) == 0 || rejected[0].Notice == nil || rejected[0].Notice.Code != "steer_failed" {
		t.Fatalf("expected steer_failed notice, got %#v", rejected)
	}

	surface := svc.root.Surfaces["surface-1"]
	if got := surface.QueuedQueueItemIDs; len(got) != 2 || got[0] != "queue-2" || got[1] != "queue-3" {
		t.Fatalf("expected queue order restored, got %#v", got)
	}
	if surface.QueueItems["queue-2"].Status != state.QueueItemQueued || surface.QueueItems["queue-3"].Status != state.QueueItemQueued {
		t.Fatalf("expected queued statuses restored, got queue-2=%#v queue-3=%#v", surface.QueueItems["queue-2"], surface.QueueItems["queue-3"])
	}
}

func TestSteerAllPendingDisconnectRestoresOriginalQueueOrder(t *testing.T) {
	now := time.Date(2026, 4, 14, 10, 20, 0, 0, time.UTC)
	svc := newSteerAllServiceFixture(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected steer command, got %#v", events)
	}

	disconnect := svc.ApplyInstanceDisconnected("inst-1")
	if len(svc.pendingSteers) != 0 {
		t.Fatalf("expected pending steer bindings cleared on disconnect, got %#v", svc.pendingSteers)
	}

	surface := svc.root.Surfaces["surface-1"]
	if got := surface.QueuedQueueItemIDs; len(got) != 2 || got[0] != "queue-2" || got[1] != "queue-3" {
		t.Fatalf("expected steer-all queue order restored on disconnect, got %#v", got)
	}
	if surface.QueueItems["queue-2"].Status != state.QueueItemQueued || surface.QueueItems["queue-3"].Status != state.QueueItemQueued {
		t.Fatalf("expected queued statuses restored on disconnect, got queue-2=%#v queue-3=%#v", surface.QueueItems["queue-2"], surface.QueueItems["queue-3"])
	}

	gotOffline := false
	for _, event := range disconnect {
		if event.Notice != nil && event.Notice.Code == "attached_instance_offline" {
			gotOffline = true
			break
		}
	}
	if !gotOffline {
		t.Fatalf("expected offline notice on disconnect, got %#v", disconnect)
	}
}

func newSteerAllServiceFixture(now *time.Time) *Service {
	svc := newServiceForTest(now)
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
			"thread-2": {ThreadID: "thread-2", Name: "另一个会话", CWD: "/data/dl/droid"},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-active",
		Text:             "先开始",
	})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-queued-1",
		Text:             "补充信息一",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-queued-2",
		Text:             "补充信息二",
	})
	return svc
}
