package feishu

import "strings"

func targetPickerHeaderElements(stageLabel, question string) []map[string]any {
	elements := make([]map[string]any, 0, 2)
	if stageLabel = strings.TrimSpace(stageLabel); stageLabel != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": formatNeutralTextTag(stageLabel),
		})
	}
	if question = strings.TrimSpace(question); question != "" {
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": "**" + renderSystemInlineTags(question) + "**",
		})
	}
	return elements
}

func targetPickerMinorLabelElement(label string) map[string]any {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	return map[string]any{
		"tag":     "markdown",
		"content": formatNeutralTextTag(label),
	}
}
