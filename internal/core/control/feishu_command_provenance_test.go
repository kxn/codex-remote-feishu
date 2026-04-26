package control

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestResolveFeishuTextCommandCarriesCatalogProvenance(t *testing.T) {
	resolved, ok := ResolveFeishuTextCommand(CatalogContext{Backend: agentproto.BackendClaude}, "/mode vscode")
	if !ok {
		t.Fatal("expected /mode vscode to resolve")
	}
	if resolved.FamilyID != FeishuCommandMode {
		t.Fatalf("family id = %q, want %q", resolved.FamilyID, FeishuCommandMode)
	}
	if resolved.VariantID != defaultFeishuCommandDisplayVariantID(FeishuCommandMode) {
		t.Fatalf("variant id = %q", resolved.VariantID)
	}
	if resolved.Backend != agentproto.BackendClaude {
		t.Fatalf("backend = %q, want %q", resolved.Backend, agentproto.BackendClaude)
	}
	if resolved.Action.CatalogFamilyID != FeishuCommandMode || resolved.Action.CatalogVariantID != resolved.VariantID || resolved.Action.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("unexpected action provenance: %#v", resolved.Action)
	}
}

func TestResolveFeishuActionCatalogFallsBackToActionKind(t *testing.T) {
	resolved, ok := ResolveFeishuActionCatalog(CatalogContext{Backend: agentproto.BackendClaude}, Action{
		Kind: ActionShowCommandHelp,
	})
	if !ok {
		t.Fatal("expected action kind fallback to resolve")
	}
	if resolved.FamilyID != FeishuCommandHelp {
		t.Fatalf("family id = %q, want %q", resolved.FamilyID, FeishuCommandHelp)
	}
	if resolved.Action.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("backend = %q, want %q", resolved.Action.CatalogBackend, agentproto.BackendClaude)
	}
}
