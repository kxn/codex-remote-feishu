package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestDaemonRejectsOldTextMessageBeforeQueueing(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedSelectedThreadSurfaceForInboundTests(app)

	var sent []agentproto.Command
	app.sendAgentCommand = func(_ string, command agentproto.Command) error {
		sent = append(sent, command)
		return nil
	}

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-old",
		Text:             "这是一条重启前的旧消息",
		Inbound: &control.ActionInboundMeta{
			MessageCreateTime: startedAt.Add(-3 * time.Minute),
		},
	})

	if len(sent) != 0 {
		t.Fatalf("expected old text message not to dispatch commands, got %#v", sent)
	}
	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Dispatch.QueuedCount != 0 || snapshot.Dispatch.ActiveItemStatus != "" {
		t.Fatalf("expected old text message not to queue input, got %#v", snapshot)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧动作已忽略", "重新发送消息、命令或重新点击菜单")
	if delta[0].Kind != feishu.OperationSendCard {
		t.Fatalf("expected old text rejection to send only notice card, got %#v", delta)
	}
	if !strings.Contains(delta[0].CardBody, "这是一条重启前的旧消息") {
		t.Fatalf("expected old text rejection to mention message preview, got %#v", delta)
	}
}

func TestDaemonRejectsOldTextDetachCommandAndKeepsAttachment(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedAttachedSurfaceForInboundTests(app)

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/detach",
		Inbound: &control.ActionInboundMeta{
			MessageCreateTime: startedAt.Add(-3 * time.Minute),
		},
	})

	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" {
		t.Fatalf("expected old detach command not to detach surface, got %#v", snapshot)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧动作已忽略", "重新发送消息、命令或重新点击菜单")
	if !strings.Contains(delta[0].CardBody, "/detach") {
		t.Fatalf("expected old detach notice to mention /detach, got %#v", delta)
	}
}

func TestDaemonRejectsOldCardDetachAndShowsExpiredNotice(t *testing.T) {
	gateway := &recordingGateway{}
	startedAt := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{PID: 42, StartedAt: startedAt})

	seedAttachedSurfaceForInboundTests(app)

	before := len(gateway.operations)
	app.HandleAction(context.Background(), control.Action{
		Kind:             control.ActionDetach,
		SurfaceSessionID: "feishu:chat:1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: "older-life",
		},
	})

	snapshot := app.service.SurfaceSnapshot("feishu:chat:1")
	if snapshot == nil || snapshot.Attachment.InstanceID != "inst-1" {
		t.Fatalf("expected old card callback not to detach surface, got %#v", snapshot)
	}
	delta := gateway.operations[before:]
	assertSingleRejectedNotice(t, delta, "旧卡片已过期", "重新发送对应命令获取新卡片")
	if !strings.Contains(delta[0].CardBody, "/detach") {
		t.Fatalf("expected expired card notice to mention /detach, got %#v", delta)
	}
}
