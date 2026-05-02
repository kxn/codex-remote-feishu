package control

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestResolveFeishuCommandSupportDefaultsCodexToVisibleNative(t *testing.T) {
	support, ok := ResolveFeishuCommandSupport(CatalogContext{}, FeishuCommandCompact)
	if !ok {
		t.Fatal("expected compact support to resolve")
	}
	if support.Kind != FeishuCommandSupportNative || !support.Visible || !support.DispatchAllowed {
		t.Fatalf("unexpected codex support: %#v", support)
	}
}

func TestResolveFeishuCommandSupportAppliesClaudeProfile(t *testing.T) {
	tests := []struct {
		familyID         string
		wantKind         FeishuCommandSupportKind
		wantVisible      bool
		wantDispatch     bool
		wantNoteContains string
	}{
		{familyID: FeishuCommandHistory, wantKind: FeishuCommandSupportNative, wantVisible: true, wantDispatch: true},
		{familyID: FeishuCommandSendFile, wantKind: FeishuCommandSupportNative, wantVisible: true, wantDispatch: true},
		{familyID: FeishuCommandCompact, wantKind: FeishuCommandSupportPassthrough, wantVisible: false, wantDispatch: false, wantNoteContains: "passthrough"},
		{familyID: FeishuCommandNew, wantKind: FeishuCommandSupportApproximation, wantVisible: true, wantDispatch: true, wantNoteContains: "route contract"},
		{familyID: FeishuCommandList, wantKind: FeishuCommandSupportApproximation, wantVisible: true, wantDispatch: true, wantNoteContains: "route contract"},
		{familyID: FeishuCommandUse, wantKind: FeishuCommandSupportApproximation, wantVisible: true, wantDispatch: true, wantNoteContains: "route contract"},
		{familyID: FeishuCommandDetach, wantKind: FeishuCommandSupportNative, wantVisible: true, wantDispatch: true},
		{familyID: FeishuCommandReview, wantKind: FeishuCommandSupportApproximation, wantVisible: false, wantDispatch: false, wantNoteContains: "隐藏"},
		{familyID: FeishuCommandPatch, wantKind: FeishuCommandSupportApproximation, wantVisible: false, wantDispatch: false, wantNoteContains: "隐藏"},
		{familyID: FeishuCommandSteerAll, wantKind: FeishuCommandSupportReject, wantVisible: false, wantDispatch: false, wantNoteContains: "same-turn steer"},
	}
	for _, tt := range tests {
		t.Run(tt.familyID, func(t *testing.T) {
			support, ok := ResolveFeishuCommandSupport(CatalogContext{Backend: agentproto.BackendClaude}, tt.familyID)
			if !ok {
				t.Fatalf("expected %s support to resolve", tt.familyID)
			}
			if support.Kind != tt.wantKind || support.Visible != tt.wantVisible || support.DispatchAllowed != tt.wantDispatch {
				t.Fatalf("unexpected support: %#v", support)
			}
			if tt.wantNoteContains != "" && !containsNormalized(support.Note, tt.wantNoteContains) {
				t.Fatalf("support note = %q, want substring %q", support.Note, tt.wantNoteContains)
			}
		})
	}
}

func TestResolveFeishuCommandSupportRejectsWrongProviderSwitcherForBackend(t *testing.T) {
	codexSupport, ok := ResolveFeishuCommandSupport(CatalogContext{}, FeishuCommandClaudeProfile)
	if !ok {
		t.Fatal("expected claude profile support to resolve in codex")
	}
	if codexSupport.DispatchAllowed || codexSupport.Visible {
		t.Fatalf("expected codex backend to reject claude profile command, got %#v", codexSupport)
	}
	if !containsNormalized(codexSupport.Note, "/mode claude") {
		t.Fatalf("expected claude mode guidance, got %q", codexSupport.Note)
	}

	claudeSupport, ok := ResolveFeishuCommandSupport(CatalogContext{Backend: agentproto.BackendClaude}, FeishuCommandCodexProvider)
	if !ok {
		t.Fatal("expected codex provider support to resolve in claude")
	}
	if claudeSupport.DispatchAllowed || claudeSupport.Visible {
		t.Fatalf("expected claude backend to reject codex provider command, got %#v", claudeSupport)
	}
	if !containsNormalized(claudeSupport.Note, "/mode codex") {
		t.Fatalf("expected codex mode guidance, got %q", claudeSupport.Note)
	}
}

func TestResolveFeishuActionSupportUsesResolvedFamily(t *testing.T) {
	support, ok := ResolveFeishuActionSupport(CatalogContext{Backend: agentproto.BackendClaude}, Action{
		Kind: ActionSteerAll,
		Text: "/steerall",
	})
	if !ok {
		t.Fatal("expected steer action support to resolve")
	}
	if support.FamilyID != FeishuCommandSteerAll || support.DispatchAllowed {
		t.Fatalf("unexpected resolved action support: %#v", support)
	}
}

func containsNormalized(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
