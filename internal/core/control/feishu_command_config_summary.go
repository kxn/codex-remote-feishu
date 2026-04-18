package control

import "strings"

// BuildFeishuCommandConfigSummarySections converts command-config runtime view
// state into adapter-owned summary sections, so dynamic values no longer need
// to pass through markdown summary strings.
func BuildFeishuCommandConfigSummarySections(def FeishuCommandDefinition, view FeishuCommandConfigView) []FeishuCardTextSection {
	base := commandConfigBaseSummarySections(view)
	sections := make([]FeishuCardTextSection, 0, len(base)+2)
	if feedback, ok := commandConfigFeedbackSection(view); ok {
		sections = append(sections, feedback)
	}
	for _, section := range base {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		sections = append(sections, normalized)
	}
	if view.Sealed {
		if command := strings.TrimSpace(def.CanonicalSlash); command != "" {
			sections = append(sections, FeishuCardTextSection{
				Label: "下一步",
				Lines: []string{"如需再次调整，请重新发送 " + command + "。"},
			})
		}
	}
	return sections
}

func commandConfigBaseSummarySections(view FeishuCommandConfigView) []FeishuCardTextSection {
	if view.RequiresAttachment {
		return []FeishuCardTextSection{{
			Lines: []string{"还没接管目标。先开始或继续工作，再回来调整这个参数。"},
		}}
	}
	switch strings.TrimSpace(view.CommandID) {
	case FeishuCommandMode:
		return []FeishuCardTextSection{singleValueCardSection("当前模式", commandDisplayValue(view.CurrentValue, "未设置"))}
	case FeishuCommandAutoContinue:
		return []FeishuCardTextSection{singleValueCardSection("当前", autoContinueDisplayValue(view.CurrentValue))}
	case FeishuCommandReasoning:
		return dualValueCardSections(
			"当前", commandDisplayValue(view.EffectiveValue, "未设置"),
			"飞书覆盖", commandDisplayValue(view.OverrideValue, "无"),
		)
	case FeishuCommandAccess:
		return dualValueCardSections(
			"当前", commandDisplayValue(view.EffectiveValue, "未设置"),
			"飞书覆盖", commandDisplayValue(view.OverrideValue, "无"),
		)
	case FeishuCommandModel:
		sections := dualValueCardSections(
			"当前模型", commandDisplayValue(view.EffectiveValue, "未设置"),
			"飞书覆盖", commandDisplayValue(view.OverrideValue, "无"),
		)
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

func commandConfigFeedbackSection(view FeishuCommandConfigView) (FeishuCardTextSection, bool) {
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

func autoContinueDisplayValue(value string) string {
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
