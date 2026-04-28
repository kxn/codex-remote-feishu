package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestBuildConfigCommandViewStateRewritesLegacyVariantToSurfaceContext(t *testing.T) {
	now := time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.Backend = agentproto.BackendClaude

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandMode)
	if !ok {
		t.Fatal("expected mode config flow")
	}
	view := svc.buildConfigCommandViewState(surface, flow, control.FeishuCatalogConfigView{
		CatalogFamilyID:  control.FeishuCommandMode,
		CatalogVariantID: "mode.default",
	})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if view.Config.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("catalog backend = %q, want %q", view.Config.CatalogBackend, agentproto.BackendClaude)
	}
	if view.Config.CatalogVariantID != "mode.claude.normal" {
		t.Fatalf("catalog variant id = %q, want %q", view.Config.CatalogVariantID, "mode.claude.normal")
	}
}
