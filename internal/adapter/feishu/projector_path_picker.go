package feishu

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func pathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	if view.Mode == control.PathPickerModeFile {
		return fileModePathPickerElements(view, daemonLifecycleID)
	}
	return defaultPathPickerElements(view, daemonLifecycleID)
}

func defaultPathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, len(view.Entries)*2+8)
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**允许范围**\n" + formatNeutralTextTag(view.RootPath),
	})
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**当前目录**\n" + formatNeutralTextTag(view.CurrentPath),
	})
	if selected := strings.TrimSpace(view.SelectedPath); selected != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**当前选择**\n" + formatNeutralTextTag(selected),
		})
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement("上一级", "default", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerUp, view.PickerID, ""), daemonLifecycleID), !view.CanGoUp, ""),
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "确认")), "primary", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerConfirm, view.PickerID, ""), daemonLifecycleID), !view.CanConfirm, ""),
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.CancelLabel, "取消")), "default", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerCancel, view.PickerID, ""), daemonLifecycleID), false, ""),
	}))
	if len(view.Entries) == 0 {
		if hint := strings.TrimSpace(view.Hint); hint != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": renderSystemInlineTags(hint),
			})
		}
		return elements
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": "**目录内容**",
	})
	for index, entry := range view.Entries {
		meta := fmt.Sprintf("%d. %s", index+1, pathPickerEntryLine(entry))
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(meta),
		})
		if button := pathPickerEntryButton(view, entry, daemonLifecycleID); len(button) != 0 {
			elements = append(elements, cardButtonGroupElement([]map[string]any{button}))
		}
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	return elements
}

func fileModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 10)
	summaryLines := []string{
		"**当前目录**\n" + formatNeutralTextTag(view.CurrentPath),
		"**允许范围**\n" + formatNeutralTextTag(view.RootPath),
	}
	selectedPath := strings.TrimSpace(view.SelectedPath)
	if selectedPath != "" {
		summaryLines = append(summaryLines, "**待发送文件**\n"+formatNeutralTextTag(selectedPath))
	} else {
		summaryLines = append(summaryLines, "**待发送文件**\n"+formatNeutralTextTag("未选择"))
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(summaryLines, "\n"),
	})

	directoryOptions := fileModeDirectoryOptions(view)
	if len(directoryOptions) != 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**进入目录**",
		})
		elements = append(elements, pathPickerSelectStaticElement(
			cardPathPickerDirectorySelectFieldName,
			".. 返回上一级，或选择子目录",
			stampActionValue(pathPickerFieldActionPayload(cardActionKindPathPickerEnter, view.PickerID, cardPathPickerDirectorySelectFieldName), daemonLifecycleID),
			directoryOptions,
			"",
		))
	}

	fileOptions, selectedOption := pathPickerSelectStaticOptions(view, control.PathPickerEntryFile)
	if len(fileOptions) != 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**选择文件**",
		})
		elements = append(elements, pathPickerSelectStaticElement(
			cardPathPickerFileSelectFieldName,
			"选择待发送文件",
			stampActionValue(pathPickerFieldActionPayload(cardActionKindPathPickerSelect, view.PickerID, cardPathPickerFileSelectFieldName), daemonLifecycleID),
			fileOptions,
			selectedOption,
		))
	}

	if len(directoryOptions) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可进入的子目录。",
		})
	}
	if len(fileOptions) == 0 {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可发送文件。",
		})
	}
	if hint := strings.TrimSpace(view.Hint); hint != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": renderSystemInlineTags(hint),
		})
	}
	elements = append(elements, cardButtonGroupElement([]map[string]any{
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.ConfirmLabel, "确认")), "primary", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerConfirm, view.PickerID, ""), daemonLifecycleID), !view.CanConfirm, ""),
		cardCallbackButtonElement(strings.TrimSpace(firstNonEmpty(view.CancelLabel, "取消")), "default", stampActionValue(actionPayloadPathPicker(cardActionKindPathPickerCancel, view.PickerID, ""), daemonLifecycleID), false, ""),
	}))
	return elements
}

func fileModeDirectoryOptions(view control.FeishuPathPickerView) []map[string]any {
	options, _ := pathPickerSelectStaticOptions(view, control.PathPickerEntryDirectory)
	if !view.CanGoUp {
		return options
	}
	parentOption := map[string]any{
		"text":  cardPlainText(".."),
		"value": "..",
	}
	options = append([]map[string]any{parentOption}, options...)
	return options
}

func pathPickerEntryLine(entry control.FeishuPathPickerEntry) string {
	name := strings.TrimSpace(firstNonEmpty(entry.Label, entry.Name))
	kind := "文件"
	if entry.Kind == control.PathPickerEntryDirectory {
		kind = "目录"
	}
	parts := []string{name, formatNeutralTextTag(kind)}
	if entry.Selected {
		parts = append(parts, "[已选]")
	}
	if reason := strings.TrimSpace(entry.DisabledReason); reason != "" {
		parts = append(parts, reason)
	}
	return strings.Join(parts, " · ")
}

func pathPickerEntryButton(view control.FeishuPathPickerView, entry control.FeishuPathPickerEntry, daemonLifecycleID string) map[string]any {
	if entry.Disabled || entry.ActionKind == control.PathPickerEntryActionNone {
		return nil
	}
	buttonType := "default"
	label := "选择"
	kind := cardActionKindPathPickerSelect
	switch entry.ActionKind {
	case control.PathPickerEntryActionEnter:
		buttonType = "primary"
		label = "进入 · " + filepath.Base(strings.TrimSpace(firstNonEmpty(entry.Label, entry.Name)))
		kind = cardActionKindPathPickerEnter
	case control.PathPickerEntryActionSelect:
		buttonType = "primary"
		if entry.Selected {
			buttonType = "default"
		}
		label = "选择 · " + filepath.Base(strings.TrimSpace(firstNonEmpty(entry.Label, entry.Name)))
	}
	return cardCallbackButtonElement(label, buttonType, stampActionValue(actionPayloadPathPicker(kind, view.PickerID, entry.Name), daemonLifecycleID), false, "fill")
}

func pathPickerSelectStaticOptions(view control.FeishuPathPickerView, kind control.PathPickerEntryKind) ([]map[string]any, string) {
	options := make([]map[string]any, 0, len(view.Entries))
	initialOption := ""
	for _, entry := range view.Entries {
		if entry.Disabled || entry.Kind != kind {
			continue
		}
		value := strings.TrimSpace(entry.Name)
		if value == "" {
			continue
		}
		options = append(options, map[string]any{
			"text":  cardPlainText(pathPickerSelectStaticLabel(entry)),
			"value": value,
		})
		if entry.Selected {
			initialOption = value
		}
	}
	return options, initialOption
}

func pathPickerSelectStaticLabel(entry control.FeishuPathPickerEntry) string {
	label := strings.TrimSpace(firstNonEmpty(entry.Label, entry.Name))
	if entry.Kind == control.PathPickerEntryDirectory {
		return label + "/"
	}
	return label
}

func pathPickerSelectStaticElement(name, placeholder string, payload map[string]any, options []map[string]any, initialOption string) map[string]any {
	element := map[string]any{
		"tag":         "select_static",
		"name":        strings.TrimSpace(name),
		"placeholder": cardPlainText(placeholder),
		"options":     options,
		"behaviors": []map[string]any{{
			"type":  "callback",
			"value": cloneCardMap(payload),
		}},
	}
	if strings.TrimSpace(initialOption) != "" {
		element["initial_option"] = strings.TrimSpace(initialOption)
	}
	return element
}

func pathPickerFieldActionPayload(kind, pickerID, fieldName string) map[string]any {
	payload := actionPayloadPathPicker(kind, pickerID, "")
	if strings.TrimSpace(fieldName) != "" {
		payload[cardActionPayloadKeyFieldName] = strings.TrimSpace(fieldName)
	}
	return payload
}
