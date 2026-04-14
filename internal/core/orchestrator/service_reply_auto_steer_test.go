package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestReplyToActiveRunningSourceAutoSteersText(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 0, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-1",
		TargetMessageID:  "msg-active",
		Text:             "请重点看最后一段",
		Inputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "<被引用内容>\n原始消息\n</被引用内容>"},
			{Type: agentproto.InputText, Text: "请重点看最后一段"},
		},
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "请重点看最后一段"},
		},
	})

	if len(events) != 2 {
		t.Fatalf("expected queue-on + steer command, got %#v", events)
	}
	if events[0].PendingInput == nil || !events[0].PendingInput.QueueOn || events[0].PendingInput.SourceMessageID != "msg-reply-1" {
		t.Fatalf("unexpected pending input event: %#v", events[0])
	}
	if events[1].Command == nil || events[1].Command.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected steer command, got %#v", events)
	}
	if len(events[1].Command.Prompt.Inputs) != 1 || events[1].Command.Prompt.Inputs[0].Text != "请重点看最后一段" {
		t.Fatalf("expected steer command to use current reply only, got %#v", events[1].Command.Prompt.Inputs)
	}

	surface := svc.root.Surfaces["surface-1"]
	if got := surface.QueuedQueueItemIDs; len(got) != 0 {
		t.Fatalf("expected no normal queue item added, got %#v", got)
	}
	item := surface.QueueItems["queue-2"]
	if item == nil || item.Status != state.QueueItemSteering {
		t.Fatalf("expected synthetic steering item, got %#v", item)
	}
	if len(item.Inputs) != 2 || item.Inputs[0].Text != "<被引用内容>\n原始消息\n</被引用内容>" {
		t.Fatalf("expected fallback queue item to keep ordinary reply inputs, got %#v", item)
	}
	if len(item.SteerInputs) != 1 || item.SteerInputs[0].Text != "请重点看最后一段" {
		t.Fatalf("expected steering item to keep current-only steer inputs, got %#v", item)
	}
}

func TestReplyToActiveRunningSourceSteerRejectedRestoresQueueOrder(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 5, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-queued",
		Text:             "正常排队的下一条",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-2",
		TargetMessageID:  "msg-active",
		Text:             "补充说明",
		Inputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "<被引用内容>\n原始消息\n</被引用内容>"},
			{Type: agentproto.InputText, Text: "补充说明"},
		},
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: "补充说明"},
		},
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected steer dispatch, got %#v", events)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-reply-1")

	rejected := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-steer-reply-1",
		Accepted:  false,
		Problem: &agentproto.ErrorInfo{
			Code:    "steer_rejected",
			Message: "running turn already closed for steering",
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	if got := surface.QueuedQueueItemIDs; len(got) != 2 || got[0] != "queue-2" || got[1] != "queue-3" {
		t.Fatalf("expected ordinary queued item order to be preserved after reject, got %#v", got)
	}
	if item := surface.QueueItems["queue-3"]; item == nil || item.Status != state.QueueItemQueued {
		t.Fatalf("expected auto-steer reply to fall back to queued item, got %#v", item)
	}
	if len(rejected) == 0 || rejected[0].Notice == nil || rejected[0].Notice.Code != "steer_failed" {
		t.Fatalf("expected steer_failed notice, got %#v", rejected)
	}
}

func TestReplyImageToActiveRunningSourceAutoSteersWithoutStaging(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 10, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-image",
		TargetMessageID:  "msg-active",
		LocalPath:        "/tmp/reply.png",
		MIMEType:         "image/png",
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/reply.png", MIMEType: "image/png"},
		},
	})

	if len(events) != 2 || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected image reply to auto-steer, got %#v", events)
	}
	if len(events[1].Command.Prompt.Inputs) != 1 || events[1].Command.Prompt.Inputs[0].Type != agentproto.InputLocalImage {
		t.Fatalf("expected image steer input, got %#v", events[1].Command.Prompt.Inputs)
	}
	surface := svc.root.Surfaces["surface-1"]
	if len(surface.StagedImages) != 0 {
		t.Fatalf("expected auto-steer image reply not to enter staged-image state, got %#v", surface.StagedImages)
	}
}

func TestReplyImageSteerRejectedFallsBackToStagedImage(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 15, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-image-2",
		TargetMessageID:  "msg-active",
		LocalPath:        "/tmp/reply-2.png",
		MIMEType:         "image/png",
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/reply-2.png", MIMEType: "image/png"},
		},
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected steer dispatch, got %#v", events)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-reply-image-1")

	rejected := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-steer-reply-image-1",
		Accepted:  false,
		Problem: &agentproto.ErrorInfo{
			Code:    "steer_rejected",
			Message: "running turn already closed for steering",
		},
	})

	surface := svc.root.Surfaces["surface-1"]
	if len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected rejected image reply not to enter normal queue, got %#v", surface.QueuedQueueItemIDs)
	}
	if len(surface.StagedImages) != 1 {
		t.Fatalf("expected rejected image reply to fall back to staged image, got %#v", surface.StagedImages)
	}
	for _, image := range surface.StagedImages {
		if image.SourceMessageID != "msg-reply-image-2" || image.State != state.ImageStaged {
			t.Fatalf("unexpected staged image fallback: %#v", image)
		}
	}
	if len(rejected) == 0 || rejected[0].Notice == nil || rejected[0].Notice.Code != "steer_failed" {
		t.Fatalf("expected steer_failed notice, got %#v", rejected)
	}
}

func newReplyAutoSteerServiceFixture(now *time.Time) *Service {
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
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-1"})
	return svc
}

func startReplyAutoSteerTurn(svc *Service) {
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
}
