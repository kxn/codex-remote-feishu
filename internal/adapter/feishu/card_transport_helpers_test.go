package feishu

import (
	"encoding/json"
	"strings"

	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
)

// Test helpers shared by feishu package tests. These exercise transport-size
// budgeting, card construction, and git-status parsing. Production code no
// longer references them, so they live with the tests that use them instead of
// the production files.

const feishuCardTransportLimitBytes = cardtransport.InteractiveCardTransportLimitBytes

func feishuInteractiveMessageTransportSize(payload map[string]any) (int, error) {
	return cardtransport.InteractiveMessagePayloadSize(payload)
}

func feishuInteractiveMessageContentTransportSize(content string) (int, error) {
	return cardtransport.InteractiveMessageContentSize(content)
}

func feishuInlineCallbackTransportSize(payload map[string]any) (int, error) {
	return cardtransport.InlineCallbackPayloadSize(payload)
}

func newCardDocument(title, themeKey string, components ...cardComponent) *cardDocument {
	return newCardDocumentWithHeader(title, cardTextTagPlainText, "", "", themeKey, components...)
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

func cardFormSubmitButtonElement(label string, value map[string]any) map[string]any {
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

func jsonSize(value any) (int, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}

func parseGitStatusPaths(output string) []string {
	return gitmeta.ParseStatusPaths(output)
}

func parseGitWorktreeSummary(output string) *gitWorktreeSummary {
	status := gitmeta.ParseStatusSummary(output)
	return &gitWorktreeSummary{
		Dirty:          status.Dirty,
		Files:          status.Files,
		ModifiedCount:  status.ModifiedCount,
		UntrackedCount: status.UntrackedCount,
	}
}
