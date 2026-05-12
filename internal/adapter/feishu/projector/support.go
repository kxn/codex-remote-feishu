package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	cardThemeInfo  = "info"
	cardThemeError = "error"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cardPlainText(content string) map[string]any {
	return map[string]any{
		"tag":     "plain_text",
		"content": strings.TrimSpace(content),
	}
}

func cardCallbackButtonElement(label, buttonType string, value map[string]any, disabled bool, width string) map[string]any {
	label = strings.TrimSpace(label)
	if label == "" {
		return nil
	}
	buttonType = strings.TrimSpace(buttonType)
	if buttonType == "" {
		buttonType = "default"
	}
	button := map[string]any{
		"tag":      "button",
		"type":     buttonType,
		"text":     cardPlainText(label),
		"disabled": disabled,
	}
	if strings.TrimSpace(width) != "" {
		button["width"] = strings.TrimSpace(width)
	}
	if len(value) != 0 {
		button["behaviors"] = []map[string]any{{
			"type":  "callback",
			"value": cloneCardMap(value),
		}}
	}
	return button
}

func cardOpenURLButtonElement(label, buttonType, openURL string, disabled bool, width string) map[string]any {
	openURL = strings.TrimSpace(openURL)
	if openURL == "" {
		return nil
	}
	button := cardCallbackButtonElement(label, buttonType, nil, disabled, width)
	if len(button) == 0 {
		return nil
	}
	button["behaviors"] = []map[string]any{{
		"type":        "open_url",
		"default_url": openURL,
	}}
	return button
}

func cardFormSubmitButtonElement(label string, value map[string]any) map[string]any {
	// Feishu does not provide a reliable live-validation loop for text inputs, so
	// generic form submits stay clickable and let the server reject invalid drafts.
	button := cardFormActionButtonElement(label, "primary", value, false, "")
	if len(button) == 0 {
		return nil
	}
	return button
}

func cardFormActionButtonElement(label, buttonType string, value map[string]any, disabled bool, width string) map[string]any {
	button := cardCallbackButtonElement(label, buttonType, value, disabled, width)
	if len(button) == 0 {
		return nil
	}
	button["name"] = "submit"
	button["form_action_type"] = "submit"
	return button
}

func cardButtonGroupElement(buttons []map[string]any) map[string]any {
	filtered := make([]map[string]any, 0, len(buttons))
	for _, button := range buttons {
		if len(button) == 0 {
			continue
		}
		filtered = append(filtered, cloneCardMap(button))
	}
	switch len(filtered) {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		columns := make([]map[string]any, 0, len(filtered))
		for _, button := range filtered {
			columns = append(columns, map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "top",
				"elements":       []map[string]any{button},
			})
		}
		return map[string]any{
			"tag":                "column_set",
			"flex_mode":          "flow",
			"horizontal_spacing": "small",
			"columns":            columns,
		}
	}
}

func cardDividerElement() map[string]any {
	return map[string]any{
		"tag": "hr",
	}
}

func appendCardFooterButtonGroup(elements []map[string]any, buttons []map[string]any) []map[string]any {
	group := cardButtonGroupElement(buttons)
	if len(group) == 0 {
		return elements
	}
	if len(elements) != 0 {
		elements = append(elements, cardDividerElement())
	}
	elements = append(elements, group)
	return elements
}

func cardPlainTextBlockElement(content string) map[string]any {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "plain_text",
			"content": content,
		},
	}
}

func appendCardTextSections(elements []map[string]any, sections []control.FeishuCardTextSection) []map[string]any {
	for _, section := range sections {
		normalized := section.Normalized()
		if normalized.Label == "" && len(normalized.Lines) == 0 {
			continue
		}
		if normalized.Label != "" {
			elements = append(elements, map[string]any{
				"tag":     "markdown",
				"content": "**" + normalized.Label + "**",
			})
		}
		if block := cardPlainTextBlockElement(strings.Join(normalized.Lines, "\n")); len(block) != 0 {
			elements = append(elements, block)
		}
	}
	return elements
}

func cloneCardMap(value map[string]any) map[string]any {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]any, len(value))
	for key, raw := range value {
		out[key] = cloneCardAny(raw)
	}
	return out
}

func cloneCardAny(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneCardMap(typed)
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneCardAny(item))
		}
		return out
	default:
		return typed
	}
}

func cardStringValue(raw any) string {
	value, _ := raw.(string)
	return value
}
