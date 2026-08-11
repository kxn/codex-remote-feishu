package projector

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
)

const (
	cardThemeInfo  = "info"
	cardThemeError = "error"
)

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
		"text":     cardkit.PlainText(label),
		"disabled": disabled,
	}
	if strings.TrimSpace(width) != "" {
		button["width"] = strings.TrimSpace(width)
	}
	if len(value) != 0 {
		button["behaviors"] = []map[string]any{{
			"type":  "callback",
			"value": cardkit.CloneMap(value),
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

func cardDividerElement() map[string]any {
	return map[string]any{
		"tag": "hr",
	}
}

func appendCardFooterButtonGroup(elements []map[string]any, buttons []map[string]any) []map[string]any {
	group := cardkit.ButtonGroupElement(buttons)
	if len(group) == 0 {
		return elements
	}
	if len(elements) != 0 {
		elements = append(elements, cardDividerElement())
	}
	elements = append(elements, group)
	return elements
}
