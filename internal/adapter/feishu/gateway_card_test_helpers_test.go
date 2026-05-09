package feishu

import (
	"fmt"
	"strings"
	"testing"
)

func markdownContent(element map[string]any) string {
	if cardStringValue(element["tag"]) != "markdown" {
		return ""
	}
	return cardStringValue(element["content"])
}

func plainTextContent(element map[string]any) string {
	if cardStringValue(element["tag"]) != "div" {
		return ""
	}
	text, _ := element["text"].(map[string]any)
	if cardStringValue(text["tag"]) != "plain_text" {
		return ""
	}
	return cardStringValue(text["content"])
}

func cardTextContent(element map[string]any) string {
	if content := markdownContent(element); content != "" {
		return content
	}
	return plainTextContent(element)
}

func renderedV2CardHeader(t *testing.T, operation Operation) map[string]any {
	t.Helper()
	payload := renderOperationCard(operation, cardEnvelopeV2)
	header, ok := payload["header"].(map[string]any)
	if !ok {
		t.Fatalf("expected rendered card header, got %#v", payload)
	}
	return header
}

func headerTextTag(header map[string]any, key string) string {
	text, _ := header[key].(map[string]any)
	return cardStringValue(text["tag"])
}

func headerTextContent(header map[string]any, key string) string {
	text, _ := header[key].(map[string]any)
	return cardStringValue(text["content"])
}

func lastButtonLabel(elements []map[string]any) string {
	for i := len(elements) - 1; i >= 0; i-- {
		if cardStringValue(elements[i]["tag"]) != "button" {
			continue
		}
		text, _ := elements[i]["text"].(map[string]any)
		if label := cardStringValue(text["content"]); label != "" {
			return label
		}
	}
	return ""
}

func containsButtonLabel(elements []map[string]any, want string) bool {
	for _, element := range elements {
		if cardStringValue(element["tag"]) != "button" {
			continue
		}
		text, _ := element["text"].(map[string]any)
		if cardStringValue(text["content"]) == want {
			return true
		}
	}
	return false
}

func containsMarkdownWithPrefix(elements []map[string]any, prefix string) bool {
	for _, element := range elements {
		if strings.HasPrefix(markdownContent(element), prefix) {
			return true
		}
	}
	return false
}

func containsMarkdownExact(elements []map[string]any, want string) bool {
	for _, element := range elements {
		if markdownContent(element) == want {
			return true
		}
	}
	return false
}

func containsCardTextExact(elements []map[string]any, want string) bool {
	for _, element := range elements {
		if cardTextContent(element) == want {
			return true
		}
	}
	return false
}

func lastMarkdownWithPrefix(elements []map[string]any, prefix string) string {
	for i := len(elements) - 1; i >= 0; i-- {
		if content := markdownContent(elements[i]); strings.HasPrefix(content, prefix) {
			return content
		}
	}
	return ""
}

func parseWorkspaceIndexFromLabel(t *testing.T, label string) int {
	t.Helper()
	var index int
	if _, err := fmt.Sscanf(label, "查看全部 · ws-%03d", &index); err != nil {
		t.Fatalf("parse workspace label %q: %v", label, err)
	}
	return index
}

func parseWorkspaceIndexFromRestoreLabel(t *testing.T, label string) int {
	t.Helper()
	var index int
	if _, err := fmt.Sscanf(label, "恢复 · ws-%03d", &index); err != nil {
		t.Fatalf("parse workspace label %q: %v", label, err)
	}
	return index
}

func cardActionsFromElements(elements []map[string]any) []map[string]any {
	var actions []map[string]any
	for _, element := range elements {
		switch cardStringValue(element["tag"]) {
		case "button":
			actions = append(actions, element)
		case "select_static":
			actions = append(actions, element)
		case "action":
			for _, button := range cardButtonArray(element["actions"]) {
				actions = append(actions, button)
			}
		case "column_set":
			for _, button := range cardButtonsFromColumnSet(element) {
				actions = append(actions, button)
			}
		case "form":
			actions = append(actions, cardActionsFromElements(cardMapArray(element["elements"]))...)
		}
	}
	return actions
}

func cardMapArray(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			entry, ok := item.(map[string]any)
			if ok {
				out = append(out, entry)
			}
		}
		return out
	default:
		return nil
	}
}

func cardButtonArray(raw any) []map[string]any {
	switch typed := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			button, ok := item.(map[string]any)
			if ok {
				out = append(out, button)
			}
		}
		return out
	case []map[string]any:
		return typed
	default:
		return nil
	}
}

func cardValueMap(action map[string]any) map[string]any {
	if action == nil {
		return nil
	}
	if typed, ok := action["value"].(map[string]any); ok {
		return typed
	}
	if typed, ok := action["behaviors"].([]map[string]any); ok && len(typed) != 0 {
		if value, ok := typed[0]["value"].(map[string]any); ok {
			return value
		}
	}
	if typed, ok := action["behaviors"].([]any); ok && len(typed) != 0 {
		if behavior, ok := typed[0].(map[string]any); ok {
			if value, ok := behavior["value"].(map[string]any); ok {
				return value
			}
		}
	}
	return nil
}

func cardButtonsFromColumnSet(element map[string]any) []map[string]any {
	rawColumns, _ := element["columns"].([]map[string]any)
	if len(rawColumns) == 0 {
		if typed, ok := element["columns"].([]any); ok {
			rawColumns = make([]map[string]any, 0, len(typed))
			for _, item := range typed {
				column, ok := item.(map[string]any)
				if ok {
					rawColumns = append(rawColumns, column)
				}
			}
		}
	}
	var buttons []map[string]any
	for _, column := range rawColumns {
		for _, button := range cardButtonArray(column["elements"]) {
			if cardStringValue(button["tag"]) == "button" {
				buttons = append(buttons, button)
			}
		}
	}
	return buttons
}
