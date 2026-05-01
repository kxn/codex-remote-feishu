package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestClaudeRejectsSteerAllBeforeCommandHandler(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionSteerAll,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/steerall",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected single rejection notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_rejected" || !strings.Contains(events[0].Notice.Text, "same-turn steer") {
		t.Fatalf("unexpected rejection notice: %#v", events[0].Notice)
	}
}

func TestBareCodexProviderIntentRejectedInClaudeBeforeOpeningCatalog(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", "normal", agentproto.BackendClaude, "", "", "")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprovider",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected single rejection notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_rejected" || !strings.Contains(events[0].Notice.Text, "/mode codex") {
		t.Fatalf("unexpected rejection notice: %#v", events[0].Notice)
	}
}
