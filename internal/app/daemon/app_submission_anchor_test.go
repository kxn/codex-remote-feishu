package daemon

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleGatewayActionReplacesMenuCardWithStatusCard(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
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
		t.Fatalf("expected status replacement result, got %#v", result)
	}
	if result.ReplaceCurrentCard.CardTitle != "当前状态" {
		t.Fatalf("unexpected status replacement card: %#v", result.ReplaceCurrentCard)
	}
	if !strings.Contains(operationCardText(*result.ReplaceCurrentCard), "当前模式") {
		t.Fatalf("expected status replacement card to render snapshot content, got %#v", result.ReplaceCurrentCard.CardElements)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended gateway operations, got %#v", gateway.operations)
	}
	if operationHasActionValue(*result.ReplaceCurrentCard, "run_command", "command_text", "/menu") {
		t.Fatalf("did not expect status replacement to use submitted-anchor reopen menu affordance, got %#v", result.ReplaceCurrentCard.CardElements)
	}
}

func TestHandleGatewayActionKeepsTypedStatusAppendOnly(t *testing.T) {
	gateway := &recordingGateway{}
	app := New(":0", ":0", gateway, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 10, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")

	result := app.HandleGatewayAction(context.Background(), control.Action{
		Kind:             control.ActionStatus,
		GatewayID:        "app-1",
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/status",
	})

	if result != nil {
		t.Fatalf("expected typed /status to stay append-only, got %#v", result)
	}
	if len(gateway.operations) != 1 || gateway.operations[0].CardTitle != "当前状态" {
		t.Fatalf("expected one appended status card, got %#v", gateway.operations)
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
	if result.ReplaceCurrentCard.CardTitle != "升级" {
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
	if result.ReplaceCurrentCard.CardTitle != "定时任务" {
		t.Fatalf("expected in-place cron card, got %#v", result.ReplaceCurrentCard)
	}
	if len(gateway.operations) != 0 {
		t.Fatalf("expected no appended card for bare cron continuation, got %#v", gateway.operations)
	}
}
