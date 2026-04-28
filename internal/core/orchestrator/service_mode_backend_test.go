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

func TestModeCommandSwitchesCurrentWorkspaceToClaudeAndPreparesHeadless(t *testing.T) {
	now := time.Date(2026, 4, 29, 3, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-codex",
		DisplayName:   "repo",
		WorkspaceRoot: "/data/dl/repo",
		WorkspaceKey:  "/data/dl/repo",
		ShortName:     "repo",
		Backend:       agentproto.BackendCodex,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录", CWD: "/data/dl/repo", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionAttachInstance,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		InstanceID:       "inst-codex",
	})
	surface := svc.root.Surfaces["surface-1"]
	surface.PromptOverride = state.ModelConfigRecord{Model: "gpt-5.4"}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		Text:             "/mode claude",
	})

	if surface.ProductMode != state.ProductModeNormal || surface.Backend != agentproto.BackendClaude {
		t.Fatalf("expected normal claude surface after switch, got %#v", surface)
	}
	if surface.AttachedInstanceID != "" || surface.RouteMode != state.RouteModeUnbound {
		t.Fatalf("expected codex attachment cleared before claude prep, got %#v", surface)
	}
	if surface.PendingHeadless == nil || !strings.EqualFold(surface.PendingHeadless.ThreadCWD, "/data/dl/repo") {
		t.Fatalf("expected claude mode switch to prepare workspace headless, got %#v", surface.PendingHeadless)
	}
	if !strings.EqualFold(surface.ClaimedWorkspaceKey, "/data/dl/repo") {
		t.Fatalf("expected workspace claim to be preserved, got %#v", surface)
	}
	if surface.PromptOverride != (state.ModelConfigRecord{}) {
		t.Fatalf("expected prompt override to be cleared, got %#v", surface.PromptOverride)
	}
	if len(events) != 3 {
		t.Fatalf("expected switch notice + workspace prep notice + daemon command, got %#v", events)
	}
	if events[0].Notice == nil || events[0].Notice.Code != "surface_mode_switched" {
		t.Fatalf("expected surface_mode_switched notice first, got %#v", events)
	}
	if events[1].Notice == nil || events[1].Notice.Code != "workspace_create_starting" {
		t.Fatalf("expected workspace_create_starting notice second, got %#v", events)
	}
	if events[2].DaemonCommand == nil || events[2].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected start headless daemon command third, got %#v", events)
	}
}
