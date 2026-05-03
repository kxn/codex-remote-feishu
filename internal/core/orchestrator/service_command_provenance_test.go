package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestResolveCatalogActionFromSurfaceContextUsesCurrentSurfaceForRawSlashCommand(t *testing.T) {
	now := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	svc.root.Surfaces["surface-1"].AttachedInstanceID = "inst-1"
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		WorkspaceRoot: "/tmp/workspace",
		WorkspaceKey:  "/tmp/workspace",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	action, ok := control.ParseFeishuTextActionWithoutCatalog("/mode claude")
	if !ok {
		t.Fatal("expected /mode claude to parse")
	}
	resolved := svc.resolveCatalogActionFromSurfaceContext(svc.root.Surfaces["surface-1"], action)
	if resolved.CatalogFamilyID != control.FeishuCommandMode {
		t.Fatalf("catalog family id = %q, want %q", resolved.CatalogFamilyID, control.FeishuCommandMode)
	}
	if resolved.CatalogVariantID != "mode.claude.normal" {
		t.Fatalf("catalog variant id = %q, want %q", resolved.CatalogVariantID, "mode.claude.normal")
	}
	if resolved.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("catalog backend = %q, want %q", resolved.CatalogBackend, agentproto.BackendClaude)
	}
}

func TestApplySurfaceActionRejectsCardCommandWithoutStrictProvenance(t *testing.T) {
	now := time.Date(2026, 5, 3, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionVerboseCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-1",
		Text:             "/verbose quiet",
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-1"},
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected stale notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_entry_expired" {
		t.Fatalf("notice code = %q, want %q", events[0].Notice.Code, "command_entry_expired")
	}
}

func TestApplySurfaceActionRejectsCardCommandWhenSurfaceContextChanged(t *testing.T) {
	now := time.Date(2026, 5, 3, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "profile-a", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionModeCommand,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-2",
		Text:             "/mode normal",
		CatalogFamilyID:  control.FeishuCommandMode,
		CatalogVariantID: "mode.codex.normal",
		CatalogBackend:   agentproto.BackendCodex,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-2"},
	})
	if len(events) != 1 || events[0].Notice == nil {
		t.Fatalf("expected stale notice, got %#v", events)
	}
	if events[0].Notice.Code != "command_entry_expired" {
		t.Fatalf("notice code = %q, want %q", events[0].Notice.Code, "command_entry_expired")
	}
}

func TestApplySurfaceActionDoesNotRejectLocalPageActionWithoutCatalogProvenance(t *testing.T) {
	now := time.Date(2026, 5, 3, 10, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionShowCommandMenu,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-card-3",
		Text:             "/menu send_settings",
		LocalPageAction:  true,
		Inbound:          &control.ActionInboundMeta{CardDaemonLifecycleID: "life-3"},
	})
	if len(events) == 1 && events[0].Notice != nil && events[0].Notice.Code == "command_entry_expired" {
		t.Fatalf("expected local page action to bypass command provenance rejection, got %#v", events)
	}
}
