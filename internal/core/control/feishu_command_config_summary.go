package control

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// BuildFeishuCommandConfigSummarySections converts command-config runtime view
// state into adapter-owned summary sections, so dynamic values no longer need
// to pass through markdown summary strings.
func BuildFeishuCommandConfigSummarySections(def FeishuCommandDefinition, view FeishuCatalogConfigView) []FeishuCardTextSection {
	return BuildFeishuCommandConfigBodySections(def, view)
}

func BuildFeishuCommandConfigBodySections(_ FeishuCommandDefinition, view FeishuCatalogConfigView) []FeishuCardTextSection {
	base := commandConfigBaseSummarySections(view)
	sections := make([]FeishuCardTextSection, 0, len(base))
	for _, section := range base {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		sections = append(sections, normalized)
	}
	return sections
}

func BuildFeishuCommandConfigNoticeSections(def FeishuCommandDefinition, view FeishuCatalogConfigView) []FeishuCardTextSection {
	sections := make([]FeishuCardTextSection, 0, 2)
	if feedback, ok := commandConfigFeedbackSection(view); ok {
		sections = append(sections, feedback)
	}
	if view.Sealed {
		if command := strings.TrimSpace(def.CanonicalSlash); command != "" {
			sections = append(sections, FeishuCardTextSection{
				Label: "下一步",
				Lines: []string{"如需再次调整，请重新发送 " + command + "。"},
			})
		}
	}
	if len(sections) == 0 {
		return nil
	}
	return sections
}

func commandConfigBaseSummarySections(view FeishuCatalogConfigView) []FeishuCardTextSection {
	if view.RequiresAttachment {
		return []FeishuCardTextSection{{
			Lines: []string{"还没接管目标。先开始或继续工作，再回来调整这个参数。"},
		}}
	}
	switch strings.TrimSpace(view.CommandID) {
	case FeishuCommandMode:
		return []FeishuCardTextSection{
			singleValueCardSection("当前模式", commandDisplayValue(view.CurrentValue, "未设置")),
			singleValueCardSection("兼容说明", "`/mode normal` 仍兼容，但它等价于 `/mode codex`。"),
		}
	case FeishuCommandCodexProvider:
		return []FeishuCardTextSection{
			singleValueCardSection("当前配置", commandCatalogOptionLabel(view.FormOptions, view.CurrentValue, commandDisplayValue(view.CurrentValue, state.DefaultCodexProviderName))),
			singleValueCardSection("切换方式", "切换后会重启当前工作区，并按新的 Codex Provider 重新准备当前会话。"),
		}
	case FeishuCommandClaudeProfile:
		return []FeishuCardTextSection{
			singleValueCardSection("当前配置", commandCatalogOptionLabel(view.FormOptions, view.CurrentValue, commandDisplayValue(view.CurrentValue, state.DefaultClaudeProfileName))),
			singleValueCardSection("切换方式", "切换后会重启当前工作区，并恢复该配置最近一次的推理临时覆盖。"),
		}
	case FeishuCommandAutoWhip:
		return []FeishuCardTextSection{singleValueCardSection("当前", autoWhipDisplayValue(view.CurrentValue))}
	case FeishuCommandAutoContinue:
		return []FeishuCardTextSection{
			singleValueCardSection("当前", autoContinueDisplayValue(view.CurrentValue)),
			singleValueCardSection("作用范围", "只处理上游可重试失败，不影响 AutoWhip"),
		}
	case FeishuCommandReasoning:
		return promptValueCardSections(view)
	case FeishuCommandAccess:
		return promptValueCardSections(view)
	case FeishuCommandPlan:
		sections := planValueCardSections(view)
		if observed := strings.TrimSpace(view.EffectiveValue); observed != "" {
			sections = append(sections, singleValueCardSection("会话最近本地模式", planModeDisplayValue(observed)))
		}
		return sections
	case FeishuCommandModel:
		sections := promptValueCardSections(view)
		if value := strings.TrimSpace(view.OverrideExtraValue); value != "" {
			sections = append(sections, singleValueCardSection("附带推理覆盖", value))
		}
		return sections
	case FeishuCommandVerbose:
		return []FeishuCardTextSection{singleValueCardSection("当前", commandDisplayValue(view.CurrentValue, "normal"))}
	default:
		return nil
	}
}

func promptValueCardSections(view FeishuCatalogConfigView) []FeishuCardTextSection {
	currentLabel := "下条消息"
	if view.UsesLocalRequestedOverrides {
		currentLabel = sharedAuthorityCurrentValueLabel(view.EffectiveValueSource)
	}
	return dualValueCardSections(
		currentLabel,
		commandDisplayValue(view.EffectiveValue, "未设置"),
		"飞书覆盖",
		promptOverrideDisplayValue(view),
	)
}

func sharedAuthorityCurrentValueLabel(source string) string {
	switch strings.TrimSpace(source) {
	case "thread", "cwd_default", "workspace_default":
		return "当前观察到"
	default:
		return "当前参考"
	}
}

func promptOverrideDisplayValue(view FeishuCatalogConfigView) string {
	if strings.TrimSpace(view.OverrideValue) != "" {
		return strings.TrimSpace(view.OverrideValue)
	}
	if view.UsesLocalRequestedOverrides {
		return "无（跟随 VS Code 当前状态）"
	}
	return "无"
}

func planValueCardSections(view FeishuCatalogConfigView) []FeishuCardTextSection {
	if view.UsesLocalRequestedOverrides {
		sections := []FeishuCardTextSection{
			singleValueCardSection("飞书覆盖", planOverrideDisplayValue(view)),
			singleValueCardSection("作用范围", "只影响后续新 turn"),
		}
		return sections
	}
	return []FeishuCardTextSection{
		singleValueCardSection("下条消息", planModeDisplayValue(view.CurrentValue)),
		singleValueCardSection("作用范围", "只影响后续新 turn"),
	}
}

func planOverrideDisplayValue(view FeishuCatalogConfigView) string {
	if !view.PlanModeOverrideSet {
		return "无（跟随 VS Code 当前状态）"
	}
	return planModeDisplayValue(view.CurrentValue)
}

func commandConfigFeedbackSection(view FeishuCatalogConfigView) (FeishuCardTextSection, bool) {
	text := normalizeCommandFeedbackText(view.StatusText)
	if text == "" {
		return FeishuCardTextSection{}, false
	}
	label := "状态"
	switch strings.TrimSpace(view.StatusKind) {
	case "error":
		label = "错误"
	case "info":
		label = "说明"
	}
	return FeishuCardTextSection{
		Label: label,
		Lines: []string{text},
	}, true
}

func singleValueCardSection(label, value string) FeishuCardTextSection {
	value = strings.TrimSpace(value)
	if value == "" {
		return FeishuCardTextSection{Label: strings.TrimSpace(label)}
	}
	return FeishuCardTextSection{
		Label: strings.TrimSpace(label),
		Lines: []string{value},
	}
}

func dualValueCardSections(firstLabel, firstValue, secondLabel, secondValue string) []FeishuCardTextSection {
	return []FeishuCardTextSection{
		singleValueCardSection(firstLabel, firstValue),
		singleValueCardSection(secondLabel, secondValue),
	}
}

func commandDisplayValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(value)
}

func autoWhipDisplayValue(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "on") {
		return "开启"
	}
	return "关闭"
}

func autoContinueDisplayValue(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "on") {
		return "开启"
	}
	return "关闭"
}

func planModeDisplayValue(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "on") {
		return "开启"
	}
	return "关闭"
}

func normalizeCommandFeedbackText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	return strings.ReplaceAll(text, "`", "")
}
