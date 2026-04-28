package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestModeCommandSwitchesDetachedSurfaceToClaude(t *testing.T) {
	now := time.Date(2026, 4, 28, 6, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode claude",
	})

	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected product mode normal, got %q", surface.ProductMode)
	}
	if surface.Backend != agentproto.BackendClaude {
		t.Fatalf("expected claude backend after switch, got %q", surface.Backend)
	}
	if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected detached unbound surface after claude switch, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected prompt override to be cleared, got %#v", surface.PromptOverride)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "claude") {
		t.Fatalf("expected claude switch notice, got %#v", events[0].Notice)
	}
}

func TestModeCommandNormalAliasReturnsSurfaceToCodex(t *testing.T) {
	now := time.Date(2026, 4, 28, 6, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionStatus, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode claude",
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		Text:             "/mode normal",
	})

	surface := svc.root.Surfaces["surface-1"]
	if surface.ProductMode != state.ProductModeNormal {
		t.Fatalf("expected product mode normal, got %q", surface.ProductMode)
	}
	if surface.Backend != agentproto.BackendCodex {
		t.Fatalf("expected normal alias to restore codex backend, got %q", surface.Backend)
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice, got %#v", events)
	}
	if !strings.Contains(events[0].Notice.Text, "codex") {
		t.Fatalf("expected codex switch notice, got %#v", events[0].Notice)
	}
}
