package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestQueuedUserMessageRepliesWhenDispatchStartsAfterWaiting(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupQueuedStartNoticeSurface(t, svc)

	first := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		ChatID:           surface.ChatID,
		ActorUserID:      surface.ActorUserID,
		MessageID:        "msg-1",
		Text:             "先跑第一条",
	})
	if event := findQueuedStartNotice(first); event != nil {
		t.Fatalf("expected immediate dispatch not to send queued-start notice, got %#v", event)
	}
	if command := findPromptSendCommand(first); command == nil {
		t.Fatalf("expected first prompt to dispatch immediately, got %#v", first)
	}

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	if len(started) == 0 {
		t.Fatal("expected first turn start to acknowledge pending input")
	}

	second := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: surface.SurfaceSessionID,
		ChatID:           surface.ChatID,
		ActorUserID:      surface.ActorUserID,
		MessageID:        "msg-2",
		Text:             "再跑这条",
	})
	if event := findQueuedStartNotice(second); event != nil {
		t.Fatalf("expected enqueue-only event not to send queued-start notice, got %#v", event)
	}
	if command := findPromptSendCommand(second); command != nil {
		t.Fatalf("expected second prompt to wait behind active turn, got %#v", second)
	}

	finished := completeRemoteTurnWithFinalText(t, svc, "turn-1", "completed", "", "", nil)
	notice := findQueuedStartNotice(finished)
	if notice == nil {
		t.Fatalf("expected queued-start timeline text when second queued prompt dispatches, got %#v", finished)
	}
	if notice.SourceMessageID != "msg-2" || notice.TimelineText.ReplyToMessageID != "msg-2" {
		t.Fatalf("expected queued-start notice to reply to second message, got %#v", notice)
	}
	if notice.TimelineText.Text != "开始执行这条排队消息。" {
		t.Fatalf("unexpected queued-start notice text: %#v", notice.TimelineText)
	}
	if command := findPromptSendCommand(finished); command == nil || command.Origin.MessageID != "msg-2" {
		t.Fatalf("expected second prompt command to dispatch after notice, got %#v", finished)
	}
}

func TestAutoWhipQueueItemDoesNotSendQueuedStartReply(t *testing.T) {
	now := time.Date(2026, 8, 9, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupQueuedStartNoticeSurface(t, svc)
	surface.QueueItems["queue-active"] = &state.QueueItemRecord{
		ID:               "queue-active",
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceKind:       state.QueueItemSourceUser,
		Status:           state.QueueItemRunning,
	}
	surface.ActiveQueueItemID = "queue-active"

	events := svc.enqueueAutoWhipQueueItem(
		surface,
		"msg-auto-anchor",
		"上一轮输出",
		[]agentproto.Input{{Type: agentproto.InputText, Text: "自动追问"}},
		"thread-1",
		"/data/dl/droid",
		state.RouteModePinned,
		state.ModelConfigRecord{},
		false,
	)
	if event := findQueuedStartNotice(events); event != nil {
		t.Fatalf("expected auto-whip enqueue not to send queued-start notice, got %#v", event)
	}

	active := surface.QueueItems["queue-active"]
	dispatched := svc.failSurfaceActiveQueueItem(surface, active, nil, true)
	if event := findQueuedStartNotice(dispatched); event != nil {
		t.Fatalf("expected auto-whip dispatch not to send queued-start notice, got %#v", event)
	}
	if command := findPromptSendCommand(dispatched); command == nil {
		t.Fatalf("expected auto-whip queue item to dispatch, got %#v", dispatched)
	}
}

func setupQueuedStartNoticeSurface(t *testing.T, svc *Service) *state.SurfaceConsoleRecord {
	t.Helper()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected attached surface")
	}
	return surface
}

func findQueuedStartNotice(events []eventcontract.Event) *eventcontract.Event {
	for i := range events {
		event := &events[i]
		if event.TimelineText != nil && event.TimelineText.Type == control.TimelineTextQueuedMessageStarted {
			return event
		}
	}
	return nil
}

func findPromptSendCommand(events []eventcontract.Event) *agentproto.Command {
	for i := range events {
		if command := events[i].Command; command != nil && command.Kind == agentproto.CommandPromptSend {
			return command
		}
	}
	return nil
}
