package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestApplyFeishuUIIntentRejectsBlockedConfigCatalogByBackend(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "", "", "")

	events := svc.ApplyFeishuUIIntent(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/codexprovider",
	}, control.FeishuUIIntent{
		Kind:    control.FeishuUIIntentShowCodexProviderCatalog,
		RawText: "/codexprovider",
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected a single rejection notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_rejected" || !strings.Contains(events[0].Notice.Text, "/mode codex") {
		t.Fatalf("unexpected rejection notice: %#v", events[0].Notice)
	}
}
