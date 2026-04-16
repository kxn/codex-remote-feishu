package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 12)
	showWorkspaceSelect := view.ShowWorkspaceSelect || (!view.ShowSourceSelect && len(view.WorkspaceOptions) != 0)
	showSessionSelect := view.ShowSessionSelect || (!view.ShowSourceSelect && len(view.SessionOptions) != 0)
	showSourceSelect := view.ShowSourceSelect
	if summary := targetPickerSummaryMarkdown(view); summary != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": summary,
		})
	}
	if view.ShowModeSwitch && len(view.ModeOptions) != 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**模式**",
		})
		if group := targetPickerModeButtons(view, daemonLifecycleID); len(group) != 0 {
			elements = append(elements, group)
		}
	}
	if showWorkspaceSelect {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**工作区**",
		})
		elements = append(elements, pathPickerSelectStaticElement(
			cardTargetPickerWorkspaceFieldName,
			firstNonEmpty(strings.TrimSpace(view.WorkspacePlaceholder), "选择工作区"),
			stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerSelectWorkspace, view.PickerID), daemonLifecycleID),
			targetPickerWorkspaceOptions(view.WorkspaceOptions),
			strings.TrimSpace(view.SelectedWorkspaceKey),
		))
	}
	if showSessionSelect {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**会话**",
		})
		elements = append(elements, pathPickerSelectStaticElement(
			cardTargetPickerSessionFieldName,
			firstNonEmpty(strings.TrimSpace(view.SessionPlaceholder), "选择会话"),
			stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerSelectSession, view.PickerID), daemonLifecycleID),
			targetPickerSessionOptions(view.SessionOptions),
			strings.TrimSpace(view.SelectedSessionValue),
		))
	}
	if showSourceSelect {
		if summary := strings.TrimSpace(view.AddModeSummary); summary != "" {
			content := "**添加工作区**\n" + formatNeutralTextTag(summary)
			if detail := strings.TrimSpace(view.AddModeDetail); detail != "" {
				content += "\n" + renderSystemInlineTags(detail)
			}
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": content,
			})
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**工作区来源**",
		})
		elements = append(elements, pathPickerSelectStaticElement(
			cardTargetPickerSourceFieldName,
			firstNonEmpty(strings.TrimSpace(view.SourcePlaceholder), "选择工作区来源"),
			stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerSelectSource, view.PickerID), daemonLifecycleID),
			targetPickerSourceOptions(view.SourceOptions),
			string(view.SelectedSource),
		))
		if hint := strings.TrimSpace(view.SourceUnavailableHint); hint != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": renderSystemInlineTags(hint),
			})
		}
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "确认")), "primary", stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerConfirm, view.PickerID), daemonLifecycleID), !view.CanConfirm, "fill"),
	}))
	return elements
}

func targetPickerSummaryMarkdown(view control.FeishuTargetPickerView) string {
	if !(view.ShowWorkspaceSelect || (!view.ShowSourceSelect && len(view.WorkspaceOptions) != 0)) {
		return ""
	}
	lines := make([]string, 0, 2)
	if label := strings.TrimSpace(view.SelectedWorkspaceLabel); label != "" {
		line := "**当前工作区**\n" + formatNeutralTextTag(label)
		if meta := strings.TrimSpace(view.SelectedWorkspaceMeta); meta != "" {
			line += "\n" + renderSystemInlineTags(meta)
		}
		lines = append(lines, line)
	}
	if label := strings.TrimSpace(view.SelectedSessionLabel); label != "" {
		line := "**当前会话**\n" + formatNeutralTextTag(label)
		if meta := strings.TrimSpace(view.SelectedSessionMeta); meta != "" {
			line += "\n" + renderSystemInlineTags(meta)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func targetPickerModeButtons(view control.FeishuTargetPickerView, daemonLifecycleID string) map[string]any {
	buttons := make([]map[string]any, 0, len(view.ModeOptions))
	for _, option := range view.ModeOptions {
		label := strings.TrimSpace(option.Label)
		if label == "" || option.Value == "" {
			continue
		}
		buttonType := "default"
		if option.Selected {
			buttonType = "primary"
		}
		buttons = append(buttons, cardCallbackButtonElement(
			label,
			buttonType,
			stampActionValue(actionPayloadTargetPickerValue(cardActionKindTargetPickerSelectMode, view.PickerID, string(option.Value)), daemonLifecycleID),
			false,
			"",
		))
	}
	return cardButtonGroupElement(buttons)
}

func targetPickerWorkspaceOptions(options []control.FeishuTargetPickerWorkspaceOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":  cardPlainText(targetPickerOptionLabel(option.Label, option.MetaText)),
			"value": value,
		})
	}
	return result
}

func targetPickerSourceOptions(options []control.FeishuTargetPickerSourceOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(string(option.Value))
		if value == "" {
			continue
		}
		meta := strings.TrimSpace(option.MetaText)
		if !option.Available && strings.TrimSpace(option.UnavailableReason) != "" {
			meta = firstNonEmpty(meta, strings.TrimSpace(option.UnavailableReason))
		}
		result = append(result, map[string]any{
			"text":  cardPlainText(targetPickerOptionLabel(option.Label, meta)),
			"value": value,
		})
	}
	return result
}

func targetPickerSessionOptions(options []control.FeishuTargetPickerSessionOption) []map[string]any {
	result := make([]map[string]any, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" {
			continue
		}
		result = append(result, map[string]any{
			"text":  cardPlainText(targetPickerOptionLabel(option.Label, option.MetaText)),
			"value": value,
		})
	}
	return result
}

func targetPickerOptionLabel(label, meta string) string {
	label = strings.TrimSpace(label)
	meta = strings.TrimSpace(meta)
	if label == "" {
		return meta
	}
	if meta == "" {
		return label
	}
	if line := strings.TrimSpace(strings.Split(meta, "\n")[0]); line != "" {
		return label + " · " + line
	}
	return label
}
