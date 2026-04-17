package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func targetPickerElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 18)
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
		if group := targetPickerSourceButtons(view, daemonLifecycleID); len(group) != 0 {
			elements = append(elements, group)
		}
		switch view.SelectedSource {
		case control.FeishuTargetPickerSourceLocalDirectory:
			elements = append(elements, targetPickerLocalDirectoryElements(view, daemonLifecycleID)...)
		case control.FeishuTargetPickerSourceGitURL:
			elements = append(elements, targetPickerGitURLElements(view, daemonLifecycleID)...)
		}
		if messages := targetPickerMessageElements(view.SourceMessages); len(messages) != 0 {
			elements = append(elements, messages...)
		}
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	if targetPickerUsesInlineGitForm(view) {
		elements = append(elements, targetPickerInlineGitFormTerminalButtons(view, daemonLifecycleID)...)
		return elements
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement("取消", "default", stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerCancel, view.PickerID), daemonLifecycleID), false, ""),
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

func targetPickerSourceButtons(view control.FeishuTargetPickerView, daemonLifecycleID string) map[string]any {
	buttons := make([]map[string]any, 0, len(view.SourceOptions))
	for _, option := range view.SourceOptions {
		label := strings.TrimSpace(option.Label)
		value := strings.TrimSpace(string(option.Value))
		if label == "" || value == "" {
			continue
		}
		buttonType := "default"
		if option.Value == view.SelectedSource {
			buttonType = "primary"
		}
		buttons = append(buttons, cardCallbackButtonElement(
			label,
			buttonType,
			stampActionValue(actionPayloadTargetPickerValue(cardActionKindTargetPickerSelectSource, view.PickerID, value), daemonLifecycleID),
			false,
			"fill",
		))
	}
	return cardButtonGroupElement(buttons)
}

func targetPickerLocalDirectoryElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	elements := []map[string]any{
		{
			"tag":     "markdown",
			"content": targetPickerFieldMarkdown("目录路径", strings.TrimSpace(view.LocalDirectoryPath), "未选择"),
		},
		{
			"tag":     "markdown",
			"content": "选择本机上已有的目录，并将它接入为工作区。",
		},
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement(
			"选择目录",
			"default",
			stampActionValue(actionPayloadTargetPickerValue(cardActionKindTargetPickerOpenPathPicker, view.PickerID, control.FeishuTargetPickerPathFieldLocalDirectory), daemonLifecycleID),
			false,
			"",
		),
	}))
	return elements
}

func targetPickerGitURLElements(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	elements := []map[string]any{
		{
			"tag":     "markdown",
			"content": targetPickerGitParentDirMarkdown(view),
		},
	}
	if form := targetPickerGitURLFormElement(view, daemonLifecycleID); len(form) != 0 {
		elements = append(elements, form)
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": targetPickerFieldMarkdown("最终路径", strings.TrimSpace(view.GitFinalPath), "待补充"),
	})
	return elements
}

func targetPickerGitURLFormElement(view control.FeishuTargetPickerView, daemonLifecycleID string) map[string]any {
	elements := make([]map[string]any, 0, 4)
	openPathButton := cardFormActionButtonElement(
		"选择目录",
		"default",
		stampActionValue(actionPayloadTargetPickerValue(cardActionKindTargetPickerOpenPathPicker, view.PickerID, control.FeishuTargetPickerPathFieldGitParentDir), daemonLifecycleID),
		false,
		"",
	)
	if len(openPathButton) != 0 {
		openPathButton["name"] = "target_picker_open_path"
		elements = append(elements, openPathButton)
	}
	elements = append(elements, targetPickerInputElement(
		control.FeishuTargetPickerGitRepoURLFieldName,
		"Git 仓库地址",
		"支持 HTTPS 或 SSH，例如 https://github.com/org/repo.git",
		strings.TrimSpace(view.GitRepoURL),
	))
	elements = append(elements, targetPickerInputElement(
		control.FeishuTargetPickerGitDirectoryNameFieldName,
		"本地目录名（可选）",
		"不填写时，将根据仓库地址自动生成",
		strings.TrimSpace(view.GitDirectoryName),
	))
	confirmButton := cardFormActionButtonElement(
		strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "克隆并继续")),
		"primary",
		stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerConfirm, view.PickerID), daemonLifecycleID),
		!view.CanConfirm,
		"",
	)
	if len(confirmButton) != 0 {
		confirmButton["name"] = "target_picker_confirm"
		elements = append(elements, confirmButton)
	}
	return map[string]any{
		"tag":      "form",
		"name":     "target_picker_git_form_" + strings.TrimSpace(view.PickerID),
		"elements": elements,
	}
}

func targetPickerInlineGitFormTerminalButtons(view control.FeishuTargetPickerView, daemonLifecycleID string) []map[string]any {
	buttons := []map[string]any{
		cardCallbackButtonElement(
			"取消",
			"default",
			stampActionValue(actionPayloadTargetPicker(cardActionKindTargetPickerCancel, view.PickerID), daemonLifecycleID),
			false,
			"",
		),
	}
	if group := cardButtonGroupElement(buttons); len(group) != 0 {
		return []map[string]any{group}
	}
	return nil
}

func targetPickerInputElement(name, label, placeholder, value string) map[string]any {
	input := map[string]any{
		"tag":  "input",
		"name": strings.TrimSpace(name),
		"label": map[string]any{
			"tag":     "plain_text",
			"content": strings.TrimSpace(label),
		},
		"label_position": "left",
	}
	if strings.TrimSpace(placeholder) != "" {
		input["placeholder"] = map[string]any{
			"tag":     "plain_text",
			"content": strings.TrimSpace(placeholder),
		}
	}
	if strings.TrimSpace(value) != "" {
		input["default_value"] = strings.TrimSpace(value)
	}
	return input
}

func targetPickerUsesInlineGitForm(view control.FeishuTargetPickerView) bool {
	return view.SelectedMode == control.FeishuTargetPickerModeAddWorkspace && view.SelectedSource == control.FeishuTargetPickerSourceGitURL
}

func targetPickerGitParentDirMarkdown(view control.FeishuTargetPickerView) string {
	content := targetPickerFieldMarkdown("落地目录", strings.TrimSpace(view.GitParentDir), "未选择")
	return content + "\n" + renderSystemInlineTags("选择仓库要克隆到哪个本地父目录。仓库会在这里创建一个新的子目录。")
}

func targetPickerFieldMarkdown(label, value, placeholder string) string {
	if strings.TrimSpace(value) == "" {
		value = strings.TrimSpace(firstNonEmpty(placeholder, "未填写"))
	}
	return fmt.Sprintf("**%s**\n%s", strings.TrimSpace(label), formatNeutralTextTag(value))
}

func targetPickerMessageElements(messages []control.FeishuTargetPickerMessage) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	elements := make([]map[string]any, 0, len(messages))
	for _, message := range messages {
		content := targetPickerMessageMarkdown(message)
		if content == "" {
			continue
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": content,
		})
	}
	return elements
}

func targetPickerMessageMarkdown(message control.FeishuTargetPickerMessage) string {
	text := strings.TrimSpace(message.Text)
	if text == "" {
		return ""
	}
	rendered := renderSystemInlineTags(text)
	if message.Level != control.FeishuTargetPickerMessageDanger {
		return rendered
	}
	return "<font color='red'>" + rendered + "</font>"
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
