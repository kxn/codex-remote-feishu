package control

import (
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestClaudeProfileConfigCopyDoesNotClaimAccessPlanMemory(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:      FeishuCommandClaudeProfile,
		CurrentValue:   "devseek",
		FormOptions:    []CommandCatalogFormFieldOption{{Label: "DevSeek", Value: "devseek"}},
		CatalogBackend: agentproto.BackendClaude,
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "推理临时覆盖") {
		t.Fatalf("expected claude profile copy to mention reasoning override, got %q", text)
	}
	if strings.Contains(text, "权限") || strings.Contains(text, "Plan 记忆") {
		t.Fatalf("claude profile copy must not claim access/plan memory, got %q", text)
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
	if !strings.Contains(text, "会话最近本地模式\n开启") {
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
	if !strings.Contains(text, "会话最近本地权限\nconfirm") {
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
