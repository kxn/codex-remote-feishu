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
	if resolved.VariantID != "mode.claude.normal" {
		t.Fatalf("variant id = %q", resolved.VariantID)
	}
	if resolved.Backend != agentproto.BackendClaude {
		t.Fatalf("backend = %q, want %q", resolved.Backend, agentproto.BackendClaude)
	}
	if resolved.Action.CatalogFamilyID != FeishuCommandMode || resolved.Action.CatalogVariantID != resolved.VariantID || resolved.Action.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("unexpected action provenance: %#v", resolved.Action)
	}
}

func TestParseFeishuTextActionDoesNotFreezeCatalogProvenance(t *testing.T) {
	action, ok := ParseFeishuTextActionWithoutCatalog("/mode claude")
	if !ok {
		t.Fatal("expected /mode claude to parse")
	}
	if action.Kind != ActionModeCommand || action.CommandID != FeishuCommandMode {
		t.Fatalf("unexpected parsed action: %#v", action)
	}
	if action.CatalogFamilyID != "" || action.CatalogVariantID != "" || action.CatalogBackend != "" {
		t.Fatalf("expected raw parser to leave catalog provenance unset, got %#v", action)
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

func TestResolveFeishuActionCatalogRewritesLegacyDefaultVariantToContextualIdentity(t *testing.T) {
	resolved, ok := ResolveFeishuActionCatalog(CatalogContext{Backend: agentproto.BackendClaude}, Action{
		Kind:             ActionModeCommand,
		CatalogFamilyID:  FeishuCommandMode,
		CatalogVariantID: defaultFeishuCommandDisplayVariantID(FeishuCommandMode),
		CatalogBackend:   agentproto.BackendClaude,
	})
	if !ok {
		t.Fatal("expected legacy catalog payload to resolve")
	}
	if resolved.FamilyID != FeishuCommandMode {
		t.Fatalf("family id = %q, want %q", resolved.FamilyID, FeishuCommandMode)
	}
	if resolved.VariantID != "mode.claude.normal" {
		t.Fatalf("variant id = %q, want %q", resolved.VariantID, "mode.claude.normal")
	}
	if resolved.Action.CatalogVariantID != "mode.claude.normal" {
		t.Fatalf("action catalog variant = %q, want %q", resolved.Action.CatalogVariantID, "mode.claude.normal")
	}
}

func TestResolveFeishuActionCatalogFallsBackToPatchRollbackRoute(t *testing.T) {
	resolved, ok := ResolveFeishuActionCatalog(CatalogContext{Backend: agentproto.BackendClaude}, Action{
		Kind: ActionTurnPatchRollback,
	})
	if !ok {
		t.Fatal("expected patch rollback action kind fallback to resolve")
	}
	if resolved.FamilyID != FeishuCommandPatch {
		t.Fatalf("family id = %q, want %q", resolved.FamilyID, FeishuCommandPatch)
	}
	if resolved.Action.CatalogBackend != agentproto.BackendClaude {
		t.Fatalf("backend = %q, want %q", resolved.Action.CatalogBackend, agentproto.BackendClaude)
	}
	if got := BuildFeishuActionText(ActionTurnPatchRollback, "patch-thread-1-1"); got != "/bendtomywill rollback patch-thread-1-1" {
		t.Fatalf("BuildFeishuActionText(rollback) = %q", got)
	}
}
