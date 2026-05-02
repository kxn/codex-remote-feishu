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

	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-reply-accepted-1")
	accepted := svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{CommandID: "cmd-steer-reply-accepted-1", Accepted: true})
	if len(accepted) != 2 {
		t.Fatalf("expected supplement text plus pending-input acknowledgement, got %#v", accepted)
	}
	if accepted[0].TimelineText == nil || accepted[0].TimelineText.Type != control.TimelineTextSteerUserSupplement || accepted[0].TimelineText.Text != "用户补充：请重点看最后一段" {
		t.Fatalf("unexpected reply-auto-steer supplement: %#v", accepted[0])
	}
	if accepted[0].TimelineText.ReplyToMessageID != "msg-active" {
		t.Fatalf("expected supplement to reply to active turn anchor, got %#v", accepted[0].TimelineText)
	}
	if accepted[1].PendingInput == nil || !accepted[1].PendingInput.QueueOff || !accepted[1].PendingInput.ThumbsUp || accepted[1].PendingInput.Status != string(state.QueueItemSteered) {
		t.Fatalf("unexpected reply-auto-steer acknowledgement: %#v", accepted[1])
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
		if image.SourceMessageID != "msg-reply-image-2" || image.State != state.ImageStaged || image.ActorUserID != "user-1" {
			t.Fatalf("unexpected staged image fallback: %#v", image)
		}
	}
	if len(rejected) == 0 || rejected[0].Notice == nil || rejected[0].Notice.Code != "steer_failed" {
		t.Fatalf("expected steer_failed notice, got %#v", rejected)
	}
}

func TestReplyImageSteerRejectedRestoredStageRespectsActorIsolation(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 16, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionImageMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-image-3",
		TargetMessageID:  "msg-active",
		ActorUserID:      "user-1",
		LocalPath:        "/tmp/reply-3.png",
		MIMEType:         "image/png",
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputLocalImage, Path: "/tmp/reply-3.png", MIMEType: "image/png"},
		},
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected steer dispatch, got %#v", events)
	}
	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-reply-image-2")

	rejected := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-steer-reply-image-2",
		Accepted:  false,
		Problem: &agentproto.ErrorInfo{
			Code:    "steer_rejected",
			Message: "running turn already closed for steering",
		},
	})
	if len(rejected) == 0 || rejected[0].Notice == nil || rejected[0].Notice.Code != "steer_failed" {
		t.Fatalf("expected steer_failed notice, got %#v", rejected)
	}

	otherUserEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-other-user",
		ActorUserID:      "user-2",
		Text:             "别人的文字",
	})
	if len(otherUserEvents) == 0 {
		t.Fatalf("expected queued events for other user, got %#v", otherUserEvents)
	}
	surface := svc.root.Surfaces["surface-1"]
	otherItem := surface.QueueItems["queue-3"]
	if otherItem == nil {
		t.Fatalf("expected other user queue item, got %#v", surface.QueueItems)
	}
	if len(otherItem.Inputs) != 1 || otherItem.Inputs[0].Type != agentproto.InputText || otherItem.Inputs[0].Text != "别人的文字" {
		t.Fatalf("expected other user not to consume restored image, got %#v", otherItem.Inputs)
	}
	if image := surface.StagedImages["img-1"]; image == nil || image.State != state.ImageStaged || image.ActorUserID != "user-1" {
		t.Fatalf("expected restored image to remain staged for original actor, got %#v", image)
	}

	ownerEvents := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-owner-user",
		ActorUserID:      "user-1",
		Text:             "我的文字",
	})
	if len(ownerEvents) == 0 {
		t.Fatalf("expected queued events for owner, got %#v", ownerEvents)
	}
	ownerItem := surface.QueueItems["queue-4"]
	if ownerItem == nil {
		t.Fatalf("expected owner queue item, got %#v", surface.QueueItems)
	}
	if len(ownerItem.Inputs) != 2 || ownerItem.Inputs[0].Type != agentproto.InputLocalImage || ownerItem.Inputs[0].Path != "/tmp/reply-3.png" || ownerItem.Inputs[1].Type != agentproto.InputText || ownerItem.Inputs[1].Text != "我的文字" {
		t.Fatalf("expected owner to consume restored image on next text, got %#v", ownerItem.Inputs)
	}
	if image := surface.StagedImages["img-1"]; image == nil || image.State != state.ImageBound {
		t.Fatalf("expected restored image to bind after owner consumes it, got %#v", image)
	}
}

func TestRenderProcessAssistantTextUsesTurnReplyAnchor(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 18, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	events := svc.renderTextItemWithSummary("inst-1", "thread-1", "turn-1", "item-1", "我先看一下目录结构。", false, nil, nil, nil)
	if len(events) != 1 || events[0].Kind != eventcontract.KindBlockCommitted || events[0].Block == nil {
		t.Fatalf("expected one process block event, got %#v", events)
	}
	if events[0].SourceMessageID != "msg-active" {
		t.Fatalf("expected process block to reuse turn reply anchor, got %#v", events[0])
	}
}

func TestReplyAutoSteerSupplementCountsStructuredFilesWithoutLeakingTaggedText(t *testing.T) {
	now := time.Date(2026, 4, 14, 13, 20, 0, 0, time.UTC)
	svc := newReplyAutoSteerServiceFixture(&now)
	startReplyAutoSteerTurn(svc)

	forwardedBundle := "<forwarded_chat_bundle_v1>\n" +
		"{\n" +
		"  \"schema\": \"forwarded_chat_bundle.v1\",\n" +
		"  \"root\": {\n" +
		"    \"kind\": \"bundle\",\n" +
		"    \"items\": [\n" +
		"      {\"kind\": \"message\", \"message_type\": \"file\"},\n" +
		"      {\"kind\": \"message\", \"message_type\": \"file\"}\n" +
		"    ]\n" +
		"  }\n" +
		"}\n" +
		"</forwarded_chat_bundle_v1>"

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		MessageID:        "msg-reply-structured",
		TargetMessageID:  "msg-active",
		Inputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: forwardedBundle},
			{Type: agentproto.InputLocalImage, Path: "/tmp/reply-structured.png", MIMEType: "image/png"},
		},
		SteerInputs: []agentproto.Input{
			{Type: agentproto.InputText, Text: forwardedBundle},
			{Type: agentproto.InputLocalImage, Path: "/tmp/reply-structured.png", MIMEType: "image/png"},
		},
	})
	if len(events) != 2 || events[1].Command == nil || events[1].Command.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected structured reply to auto-steer, got %#v", events)
	}

	svc.BindPendingRemoteCommand("surface-1", "cmd-steer-reply-structured-1")
	accepted := svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{CommandID: "cmd-steer-reply-structured-1", Accepted: true})
	if len(accepted) != 2 {
		t.Fatalf("expected supplement text plus pending-input acknowledgement, got %#v", accepted)
	}
	if accepted[0].TimelineText == nil || accepted[0].TimelineText.Text != "用户补充（追加 1 张图片，2 个文件）" {
		t.Fatalf("unexpected structured supplement event: %#v", accepted[0])
	}
	if strings.Contains(accepted[0].TimelineText.Text, "forwarded_chat_bundle") {
		t.Fatalf("expected structured bundle tag not to leak into supplement text, got %#v", accepted[0].TimelineText)
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
