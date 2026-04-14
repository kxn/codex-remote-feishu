package daemon

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDaemonAutoSteerReplyAddsQueueReactionThenThumbsUp(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{})

	var commands []agentproto.Command
	app.sendAgentCommand = func(instanceID string, command agentproto.Command) error {
		if instanceID != "inst-1" {
			t.Fatalf("unexpected command target: %s", instanceID)
		}
		commands = append(commands, command)
		return nil
	}

	app.service.UpsertInstance(&state.InstanceRecord{
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
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionUseThread,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		ThreadID:         "thread-1",
	})
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-active",
		Text:             "先开始",
	})
	app.onEvents(context.Background(), "inst-1", []agentproto.Event{{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	}})

	beforeReply := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-reply",
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

	if len(commands) < 2 {
		t.Fatalf("expected steer command to be dispatched, got %#v", commands)
	}
	steer := commands[len(commands)-1]
	if steer.Kind != agentproto.CommandTurnSteer {
		t.Fatalf("expected last command to be turn.steer, got %#v", steer)
	}
	replyOps := gateway.operations[beforeReply:]
	if len(replyOps) != 1 || replyOps[0].Kind != feishu.OperationAddReaction || replyOps[0].MessageID != "msg-reply" || replyOps[0].EmojiType != "OneSecond" {
		t.Fatalf("expected reply message to receive pending queue reaction, got %#v", replyOps)
	}

	beforeAck := len(gateway.operations)
	app.onCommandAck(context.Background(), "inst-1", agentproto.CommandAck{
		CommandID: steer.CommandID,
		Accepted:  true,
	})
	ackOps := gateway.operations[beforeAck:]
	if len(ackOps) != 2 {
		t.Fatalf("expected queue-off + thumbs-up after accepted steer, got %#v", ackOps)
	}
	if ackOps[0].Kind != feishu.OperationRemoveReaction || ackOps[0].MessageID != "msg-reply" || ackOps[0].EmojiType != "OneSecond" {
		t.Fatalf("expected first op to remove pending queue reaction, got %#v", ackOps)
	}
	if ackOps[1].Kind != feishu.OperationAddReaction || ackOps[1].MessageID != "msg-reply" || ackOps[1].EmojiType != "THUMBSUP" {
		t.Fatalf("expected second op to add thumbs up, got %#v", ackOps)
	}
}
