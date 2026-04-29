package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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

func TestBuildConfigCommandViewStatePopulatesClaudeProfileOptions(t *testing.T) {
	now := time.Date(2026, 4, 29, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResume("surface-1", "", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendClaude, "devseek", "", "")
	svc.MaterializeClaudeProfiles([]state.ClaudeProfileRecord{
		{ID: "devseek", Name: "DevSeek"},
		{ID: "devseek-max", Name: "DevSeek"},
	})

	flow, ok := control.FeishuConfigFlowDefinitionByCommandID(control.FeishuCommandClaudeProfile)
	if !ok {
		t.Fatal("expected claude profile config flow")
	}
	view := svc.buildConfigCommandViewState(svc.root.Surfaces["surface-1"], flow, control.FeishuCatalogConfigView{})
	if view.Config == nil {
		t.Fatal("expected config view")
	}
	if view.Config.CurrentValue != "devseek" {
		t.Fatalf("current value = %q, want %q", view.Config.CurrentValue, "devseek")
	}
	if view.Config.FormDefaultValue != "devseek" {
		t.Fatalf("default value = %q, want %q", view.Config.FormDefaultValue, "devseek")
	}
	if got := view.Config.FormOptions; len(got) != 3 {
		t.Fatalf("expected default + 2 custom profiles, got %#v", got)
	} else {
		if got[0].Label != state.DefaultClaudeProfileName || got[0].Value != state.DefaultClaudeProfileID {
			t.Fatalf("unexpected built-in default option: %#v", got[0])
		}
		if got[1].Label != "DevSeek" || got[1].Value != "devseek" {
			t.Fatalf("unexpected first custom option: %#v", got[1])
		}
		if got[2].Label != "DevSeek（devseek-max）" || got[2].Value != "devseek-max" {
			t.Fatalf("unexpected second custom option: %#v", got[2])
		}
	}
}
