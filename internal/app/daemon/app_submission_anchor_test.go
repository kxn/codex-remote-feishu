package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestHandleGatewayActionReplacesMenuCardWithSubmittedAnchorAndKeepsStatusAppend(t *testing.T) {
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
}
