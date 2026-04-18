package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleGatewayActionReplacesMenuCardWithSubmittedAnchorAndKeepsStatusAppend(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.commandAnchorRecallDelay = 10 * time.Millisecond
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-1",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected submitted-anchor replacement result, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "命令已提交" {
		t.Fatalf("unexpected submitted-anchor card: %#v", result.ReplaceCurrentCard)
	}
	if !operationHasActionValue(*result.ReplaceCurrentCard, "run_command", "command_text", "/menu") {
		t.Fatalf("expected submitted-anchor card to include reopen menu action, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 1 {
		t.Fatalf("expected appended status result card, got %#v", gateway.operations)
	}
	if gateway.operations[0].CardTitle != "当前状态" {
		t.Fatalf("unexpected appended status card: %#v", gateway.operations[0])
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(gateway.operations) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(gateway.operations) != 2 {
		t.Fatalf("expected delayed delete op after status append, got %#v", gateway.operations)
	}
	if gateway.operations[1].Kind != feishu.OperationDeleteMessage || gateway.operations[1].MessageID != "om-card-1" {
		t.Fatalf("unexpected delayed delete op: %#v", gateway.operations[1])
	}
}

func TestHandleGatewayActionContinuesBareUpgradeInPlace(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionUpgradeCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/upgrade",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline continuation replacement result, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "Upgrade" {
		t.Fatalf("expected in-place upgrade card, got %#v", result.ReplaceCurrentCard)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended card for bare upgrade continuation, got %#v", gateway.operations)
	}
}

func TestHandleGatewayActionContinuesBareCronInPlace(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionCronCommand,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/cron",
		Inbound: &control.ActionInboundMeta{
			CardDaemonLifecycleID: app.daemonLifecycleID,
		},
	})

	if result == nil || result.ReplaceCurrentCard == nil {
		t.Fatalf("expected inline continuation replacement result, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "Cron" {
		t.Fatalf("expected in-place cron card, got %#v", result.ReplaceCurrentCard)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended card for bare cron continuation, got %#v", gateway.operations)
	}
}
