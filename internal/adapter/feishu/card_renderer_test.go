package feishu

import "testing"

func TestRenderOperationCardLegacyEnvelopeFromOperationFields(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "当前状态",
		CardBody:     "这是正文",
		CardThemeKey: cardThemeInfo,
		CardElements: []map[string]any{{
			"tag":     "markdown",
			"content": "**附加内容**",
		}},
	}, cardEnvelopeLegacy)

	if payload["schema"] != nil {
		t.Fatalf("expected legacy envelope without schema, got %#v", payload)
	}
	header, _ := payload["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if title["content"] != "当前状态" {
		t.Fatalf("unexpected legacy header: %#v", payload)
	}
	elements, _ := payload["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("expected body markdown plus extra element, got %#v", elements)
	}
}

func TestRenderOperationCardV2EnvelopeFromOperationFields(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "命令菜单",
		CardBody:     "当前在发送设置。",
		CardThemeKey: cardThemeInfo,
		CardElements: []map[string]any{{
			"tag":     "markdown",
			"content": "**发送设置**",
		}},
	}, cardEnvelopeV2)

	if payload["schema"] != "2.0" {
		t.Fatalf("expected v2 schema, got %#v", payload)
	}
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("expected body markdown plus extra element, got %#v", elements)
	}
	header, _ := payload["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if title["content"] != "命令菜单" {
		t.Fatalf("unexpected v2 header: %#v", payload)
	}
}

func TestRenderOperationCardPrefersStructuredDocument(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "legacy title",
		CardBody:     "legacy body",
		CardThemeKey: cardThemeError,
		card: newCardDocument(
			"doc title",
			cardThemeSuccess,
			cardMarkdownComponent{Content: "doc body"},
		),
	}, cardEnvelopeV2)

	header, _ := payload["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if title["content"] != "doc title" {
		t.Fatalf("expected structured card title to win, got %#v", payload)
	}
	if header["template"] != "green" {
		t.Fatalf("expected structured card theme to win, got %#v", payload)
	}
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["content"] != "doc body" {
		t.Fatalf("expected structured card body to win, got %#v", payload)
	}
}

func TestRenderOperationCardV2ConvertsLegacyActionRowToButton(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind: OperationSendCard,
		card: legacyCompatibleCardDocument("命令菜单", "", cardThemeInfo, []map[string]any{{
			"tag": "action",
			"actions": []map[string]any{{
				"tag":  "button",
				"type": "primary",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": "查看实例",
				},
				"value": map[string]any{
					"kind":         "run_command",
					"command_text": "/list",
				},
			}},
		}}),
	}, cardEnvelopeV2)

	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["tag"] != "button" {
		t.Fatalf("expected v2 action row to render as a button, got %#v", payload)
	}
	if elements[0]["value"] != nil {
		t.Fatalf("expected legacy button value to be removed in v2, got %#v", elements[0])
	}
	behaviors, _ := elements[0]["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected v2 button behaviors callback, got %#v", elements[0])
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	if value["kind"] != "run_command" || value["command_text"] != "/list" {
		t.Fatalf("unexpected v2 callback payload: %#v", behaviors[0])
	}
}

func TestRenderOperationCardV2ConvertsLegacyMultiButtonRowToColumnSet(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind: OperationSendCard,
		card: legacyCompatibleCardDocument("命令菜单", "", cardThemeInfo, []map[string]any{{
			"tag": "action",
			"actions": []map[string]any{
				{
					"tag":  "button",
					"type": "default",
					"text": map[string]any{"tag": "plain_text", "content": "返回"},
					"value": map[string]any{
						"kind":         "run_command",
						"command_text": "/menu",
					},
				},
				{
					"tag":  "button",
					"type": "primary",
					"text": map[string]any{"tag": "plain_text", "content": "查看实例"},
					"value": map[string]any{
						"kind":         "run_command",
						"command_text": "/list",
					},
				},
			},
		}}),
	}, cardEnvelopeV2)

	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["tag"] != "column_set" {
		t.Fatalf("expected multi-button action row to render as v2 column_set, got %#v", payload)
	}
	columns, _ := elements[0]["columns"].([]map[string]any)
	if len(columns) != 2 {
		t.Fatalf("expected two button columns, got %#v", elements[0])
	}
	firstElements, _ := columns[0]["elements"].([]map[string]any)
	secondElements, _ := columns[1]["elements"].([]map[string]any)
	if len(firstElements) != 1 || len(secondElements) != 1 {
		t.Fatalf("expected one button per v2 column, got %#v", elements[0])
	}
	if firstElements[0]["tag"] != "button" || secondElements[0]["tag"] != "button" {
		t.Fatalf("expected button elements inside v2 columns, got %#v", elements[0])
	}
}
