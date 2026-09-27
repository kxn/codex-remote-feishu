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

func TestPlanConfigPageShowsNoLocalOverrideState(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:                   FeishuCommandPlan,
		CatalogBackend:              agentproto.BackendCodex,
		CurrentValue:                "off",
		EffectiveValue:              "on",
		UsesLocalRequestedOverrides: true,
		PlanModeOverrideSet:         false,
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "飞书覆盖\n无（跟随底层当前状态）") {
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

func TestAccessConfigPageShowsUnmappedObservedThreadAccess(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:            FeishuCommandAccess,
		CatalogBackend:       agentproto.BackendClaude,
		CurrentValue:         "dontAsk（当前无本地精确映射）",
		EffectiveValue:       agentproto.AccessModeConfirm,
		EffectiveValueSource: "thread",
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "当前会话权限（最近观察）\ndontAsk（当前无本地精确映射）") {
		t.Fatalf("expected access page to show raw native mode for unmapped projection, got %q", text)
	}
}

func TestCodexModelCardShowsProfileDefaultFallbackWithoutOverride(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:      FeishuCommandModel,
		CatalogBackend: agentproto.BackendCodex,
	})
	text := configPageSummaryText(page)
	if !strings.Contains(text, "下条消息\n跟随 Codex/Profile 默认") {
		t.Fatalf("expected Codex default fallback, got %q", text)
	}
	if !strings.Contains(text, "话题覆盖\n无") {
		t.Fatalf("expected empty topic override, got %q", text)
	}
}

func TestCodexModelCardShowsSuspendedTopicOverrideForFixedProfile(t *testing.T) {
	page := BuildFeishuCommandConfigPageView(FeishuCatalogConfigView{
		CommandID:          FeishuCommandModel,
		CatalogBackend:     agentproto.BackendCodex,
		EffectiveValue:     "provider-custom",
		OverrideValue:      "gpt-5.6-terra",
		OverrideExtraValue: "high",
		StatusKind:         "info",
		StatusText:         "当前固定 Codex Profile 下，话题覆盖已暂停；切回动态 Profile 后会重新生效。",
	})
	body := configPageSummaryText(page)
	if !strings.Contains(body, "下条消息\nprovider-custom") || !strings.Contains(body, "话题覆盖\ngpt-5.6-terra") || !strings.Contains(body, "附带推理覆盖\nhigh") {
		t.Fatalf("expected effective and dormant topic values, got %q", body)
	}
	notice := configPageNoticeText(page)
	if !strings.Contains(notice, "话题覆盖已暂停") {
		t.Fatalf("expected suspended notice, got %q", notice)
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

func configPageNoticeText(page FeishuPageView) string {
	var parts []string
	for _, section := range page.NoticeSections {
		if section.Label != "" {
			parts = append(parts, section.Label)
		}
		parts = append(parts, section.Lines...)
	}
	return strings.Join(parts, "\n")
}
