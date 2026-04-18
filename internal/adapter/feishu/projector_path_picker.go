package feishu

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func pathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	if view.Terminal {
		return terminalPathPickerElements(view)
	}
	switch view.Mode {
	case control.PathPickerModeFile:
		return fileModePathPickerElements(view, daemonLifecycleID)
	case control.PathPickerModeDirectory:
		return directoryModePathPickerElements(view, daemonLifecycleID)
	default:
		return directoryModePathPickerElements(view, daemonLifecycleID)
	}
}

func terminalPathPickerElements(view control.FeishuPathPickerView) []map[string]any {
	elements := make([]map[string]any, 0, 3)
	if title := strings.TrimSpace(view.StatusTitle); title != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + title + "**",
		})
	}
	if len(view.StatusSections) != 0 {
		elements = appendCardTextSections(elements, view.StatusSections)
		if footer := strings.TrimSpace(view.StatusFooter); footer != "" {
			if block := cardPlainTextBlockElement(footer); len(block) != 0 {
				elements = append(elements, block)
			}
		}
		return elements
	}
	if text := strings.TrimSpace(view.StatusText); text != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": text,
		})
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

func directoryModePathPickerElements(view control.FeishuPathPickerView, daemonLifecycleID string) []map[string]any {
	elements := make([]map[string]any, 0, 8)
	summaryLines := []string{
		"**允许范围**\n" + formatNeutralTextTag(view.RootPath),
		"**当前目录**\n" + formatNeutralTextTag(view.CurrentPath),
	}
	selectedPath := strings.TrimSpace(firstNonEmpty(view.SelectedPath, view.CurrentPath))
	if selectedPath != "" {
		summaryLines = append(summaryLines, "**当前选择**\n"+formatNeutralTextTag(selectedPath))
	}
	elements = append(elements, map[string]any{
		"tag":     "markdown",
		"content": strings.Join(summaryLines, "\n"),
	})

	directoryOptions := directoryModeDirectoryOptions(view)
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
	} else {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "当前目录下没有可进入的子目录。",
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
	return pathPickerDirectoryOptions(view)
}

func directoryModeDirectoryOptions(view control.FeishuPathPickerView) []map[string]any {
	return pathPickerDirectoryOptions(view)
}

func pathPickerDirectoryOptions(view control.FeishuPathPickerView) []map[string]any {
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
