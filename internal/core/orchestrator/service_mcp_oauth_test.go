package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func TestMCPOAuthCommandProducesDaemonCommand(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionMCPOAuthCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "msg-1",
		Text:             "/mcpoauth docs",
		CommandID:        control.FeishuCommandMCPOAuth,
	})
	if len(events) != 1 {
		t.Fatalf("expected one event, got %#v", events)
	}
	event := events[0]
	if event.CanonicalKind() != eventcontract.KindDaemonCommand || event.DaemonCommand == nil {
		t.Fatalf("expected daemon command event, got %#v", event)
	}
	command := event.DaemonCommand
	if command.Kind != control.DaemonCommandMCPOAuthLogin || command.Text != "/mcpoauth docs" || command.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected daemon command: %#v", command)
	}
}
