package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestApplySurfaceActionPlanCommandUpdatesSurface(t *testing.T) {
	now := time.Date(2026, 4, 3, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPlanCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/plan on",
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_plan_mode_updated" {
		t.Fatalf("expected plan mode updated notice, got %#v", events)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface == nil {
		t.Fatal("expected surface to exist")
	}
	if surface.PlanMode != state.PlanModeSettingOn {
		t.Fatalf("expected surface plan mode on, got %q", surface.PlanMode)
	}
}
