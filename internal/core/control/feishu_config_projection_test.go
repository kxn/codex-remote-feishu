package control

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeProfileConfigCopyClaimsAccessButNotPlanMemory(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:      FeishuCommandClaudeProfile,
		CurrentValue:   "devseek",
		FormOptions:    []CommandCatalogFormFieldOption{{Label: "DevSeek", Value: "devseek"}},
		CatalogBackend: agentproto.BackendClaude,
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "推理与权限临时覆盖") {
		t.Fatalf("expected claude profile copy to mention reasoning/access override, got %q", text)
	}
	if strings.Contains(text, "Plan 记忆") {
		t.Fatalf("claude profile copy must not claim plan memory, got %q", text)
	}
}

func TestPlanConfigPageShowsVSCodeNoOverrideState(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:                   FeishuCommandPlan,
		CatalogBackend:              agentproto.BackendCodex,
		CurrentValue:                "off",
		EffectiveValue:              "on",
		UsesLocalRequestedOverrides: true,
		PlanModeOverrideSet:         false,
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "飞书覆盖\n无（跟随 VS Code 当前状态）") {
		t.Fatalf("expected plan page to show no local override, got %q", text)
	}
	if !strings.Contains(text, "当前会话模式（最近观察）\n开启") {
		t.Fatalf("expected plan page to keep observed backend state, got %q", text)
	}
}

func TestAccessConfigPageShowsObservedThreadAccess(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:            FeishuCommandAccess,
		CatalogBackend:       agentproto.BackendClaude,
		CurrentValue:         agentproto.AccessModeConfirm,
		EffectiveValue:       agentproto.AccessModeConfirm,
		EffectiveValueSource: "thread",
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "当前会话权限（最近观察）\nconfirm") {
		t.Fatalf("expected access page to show observed thread access, got %q", text)
	}
}

func configPageSummaryText(page FeishuPageView) string {
	var parts []string
	for _, section := range page.SummarySections {
		if section.Label != "" {
			parts = append(parts, section.Label)
		}
		parts = append(parts, section.Lines...)
	}
	return strings.Join(parts, "\n")
}
