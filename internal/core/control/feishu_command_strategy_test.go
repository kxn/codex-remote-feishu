package control

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestResolveFeishuCommandStrategyDefaultsCodexToVisibleNative(t *testing.T) {
	strategy, ok := ResolveFeishuCommandStrategy(CatalogContext{}, FeishuCommandCompact)
	if !ok {
		t.Fatal("expected compact strategy to resolve")
	}
	if strategy.Kind != FeishuCommandStrategyNative || !strategy.Visible || !strategy.DispatchAllowed {
		t.Fatalf("unexpected codex strategy: %#v", strategy)
	}
}

func TestResolveFeishuCommandStrategyAppliesClaudeMatrix(t *testing.T) {
	tests := []struct {
		familyID         string
		wantKind         FeishuCommandStrategyKind
		wantVisible      bool
		wantDispatch     bool
		wantNoteContains string
	}{
		{familyID: FeishuCommandHistory, wantKind: FeishuCommandStrategyNative, wantVisible: true, wantDispatch: true},
		{familyID: FeishuCommandCompact, wantKind: FeishuCommandStrategyPassthrough, wantVisible: false, wantDispatch: false, wantNoteContains: "passthrough"},
		{familyID: FeishuCommandNew, wantKind: FeishuCommandStrategyApproximation, wantVisible: true, wantDispatch: true, wantNoteContains: "route contract"},
		{familyID: FeishuCommandList, wantKind: FeishuCommandStrategyApproximation, wantVisible: true, wantDispatch: true, wantNoteContains: "route contract"},
		{familyID: FeishuCommandUse, wantKind: FeishuCommandStrategyApproximation, wantVisible: true, wantDispatch: true, wantNoteContains: "route contract"},
		{familyID: FeishuCommandDetach, wantKind: FeishuCommandStrategyNative, wantVisible: true, wantDispatch: true},
		{familyID: FeishuCommandReview, wantKind: FeishuCommandStrategyApproximation, wantVisible: false, wantDispatch: false, wantNoteContains: "隐藏"},
		{familyID: FeishuCommandPatch, wantKind: FeishuCommandStrategyApproximation, wantVisible: false, wantDispatch: false, wantNoteContains: "隐藏"},
		{familyID: FeishuCommandSteerAll, wantKind: FeishuCommandStrategyReject, wantVisible: false, wantDispatch: false, wantNoteContains: "same-turn steer"},
	}
	for _, tt := range tests {
		t.Run(tt.familyID, func(t *testing.T) {
			strategy, ok := ResolveFeishuCommandStrategy(CatalogContext{Backend: agentproto.BackendClaude}, tt.familyID)
			if !ok {
				t.Fatalf("expected %s strategy to resolve", tt.familyID)
			}
			if strategy.Kind != tt.wantKind || strategy.Visible != tt.wantVisible || strategy.DispatchAllowed != tt.wantDispatch {
				t.Fatalf("unexpected strategy: %#v", strategy)
			}
			if tt.wantNoteContains != "" && !containsNormalized(strategy.Note, tt.wantNoteContains) {
				t.Fatalf("strategy note = %q, want substring %q", strategy.Note, tt.wantNoteContains)
			}
		})
	}
}

func TestResolveFeishuCommandStrategyRejectsWrongProviderSwitcherForBackend(t *testing.T) {
	codexStrategy, ok := ResolveFeishuCommandStrategy(CatalogContext{}, FeishuCommandClaudeProfile)
	if !ok {
		t.Fatal("expected claude profile strategy to resolve in codex")
	}
	if codexStrategy.DispatchAllowed || codexStrategy.Visible {
		t.Fatalf("expected codex backend to reject claude profile command, got %#v", codexStrategy)
	}
	if !containsNormalized(codexStrategy.Note, "/mode claude") {
		t.Fatalf("expected claude mode guidance, got %q", codexStrategy.Note)
	}

	claudeStrategy, ok := ResolveFeishuCommandStrategy(CatalogContext{Backend: agentproto.BackendClaude}, FeishuCommandCodexProvider)
	if !ok {
		t.Fatal("expected codex provider strategy to resolve in claude")
	}
	if claudeStrategy.DispatchAllowed || claudeStrategy.Visible {
		t.Fatalf("expected claude backend to reject codex provider command, got %#v", claudeStrategy)
	}
	if !containsNormalized(claudeStrategy.Note, "/mode codex") {
		t.Fatalf("expected codex mode guidance, got %q", claudeStrategy.Note)
	}
}

func TestResolveFeishuActionStrategyUsesResolvedFamily(t *testing.T) {
	strategy, ok := ResolveFeishuActionStrategy(CatalogContext{Backend: agentproto.BackendClaude}, Action{
		Kind: ActionSteerAll,
		Text: "/steerall",
	})
	if !ok {
		t.Fatal("expected steer action strategy to resolve")
	}
	if strategy.FamilyID != FeishuCommandSteerAll || strategy.DispatchAllowed {
		t.Fatalf("unexpected resolved action strategy: %#v", strategy)
	}
}

func containsNormalized(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
