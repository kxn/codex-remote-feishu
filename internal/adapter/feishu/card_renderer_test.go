package feishu

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

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
	assertRenderedCardPayloadBasicInvariants(t, payload)

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
	assertRenderedCardPayloadBasicInvariants(t, payload)

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

func TestRenderOperationCardV2EnvelopeCanEnableSharedCardUpdates(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:            OperationUpdateCard,
		CardTitle:       "执行中",
		CardBody:        "`npm test`",
		CardThemeKey:    cardThemeInfo,
		CardUpdateMulti: true,
	}, cardEnvelopeV2)
	assertRenderedCardPayloadBasicInvariants(t, payload)

	config, _ := payload["config"].(map[string]any)
	if config["update_multi"] != true {
		t.Fatalf("expected update_multi=true for patchable card, got %#v", payload)
	}
}

func TestRenderOperationCardV2EnvelopeFromOperationFieldsPreservesNativeV2InteractiveElements(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "需要确认",
		CardThemeKey: cardThemeApproval,
		CardElements: []map[string]any{
			cardCallbackButtonElement("查看实例", "primary", map[string]any{
				"kind":        "page_action",
				"action_kind": string(control.ActionListInstances),
			}, false, ""),
			map[string]any{
				"tag":  "form",
				"name": "request_form_req_1",
				"elements": []map[string]any{
					{
						"tag":  "input",
						"name": "notes",
					},
					cardFormSubmitButtonElement("提交答案", map[string]any{
						"kind":       "submit_request_form",
						"request_id": "req-1",
					}),
				},
			},
		},
	}, cardEnvelopeV2)
	assertRenderedCardPayloadBasicInvariants(t, payload)

	if payload["schema"] != "2.0" {
		t.Fatalf("expected v2 schema, got %#v", payload)
	}
	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 2 {
		t.Fatalf("expected native v2 button and form, got %#v", elements)
	}
	if elements[0]["tag"] != "button" || elements[0]["value"] != nil {
		t.Fatalf("expected native v2 button to stay button+behaviors, got %#v", elements[0])
	}
	behaviors, _ := elements[0]["behaviors"].([]map[string]any)
	if len(behaviors) != 1 || behaviors[0]["type"] != "callback" {
		t.Fatalf("expected native v2 button behaviors callback, got %#v", elements[0])
	}
	value, _ := behaviors[0]["value"].(map[string]any)
	assertPageActionPayloadMatchesCommand(t, value, "/list")
	formElements, _ := elements[1]["elements"].([]map[string]any)
	if len(formElements) != 2 {
		t.Fatalf("expected native v2 form to keep input and submit button, got %#v", elements[1])
	}
	if formElements[1]["action_type"] != nil || formElements[1]["form_action_type"] != "submit" {
		t.Fatalf("expected native v2 submit button to keep form_action_type, got %#v", formElements[1])
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
	assertRenderedCardPayloadBasicInvariants(t, payload)

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

func TestRenderOperationCardV2DoesNotTranslateLegacyActionRow(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "命令菜单",
		CardThemeKey: cardThemeInfo,
		CardElements: []map[string]any{{
			"tag": "action",
			"actions": []map[string]any{{
				"tag":  "button",
				"type": "primary",
				"text": map[string]any{
					"tag":     "plain_text",
					"content": "查看实例",
				},
				"value": map[string]any{
					"kind":        "page_action",
					"action_kind": string(control.ActionListInstances),
				},
			}},
		}},
	}, cardEnvelopeV2)
	assertRenderedCardPayloadBasicInvariants(t, payload)

	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["tag"] != "action" {
		t.Fatalf("expected renderer to preserve raw legacy action row instead of translating it, got %#v", payload)
	}
}

func TestRenderOperationCardV2DoesNotTranslateLegacyFormSubmitButton(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "输入参数",
		CardThemeKey: cardThemeApproval,
		CardElements: []map[string]any{{
			"tag":  "form",
			"name": "request_form_req_1",
			"elements": []map[string]any{
				{
					"tag":  "input",
					"name": "notes",
				},
				{
					"tag":  "button",
					"type": "primary",
					"text": map[string]any{
						"tag":     "plain_text",
						"content": "提交答案",
					},
					"action_type": "form_submit",
					"name":        "submit",
					"value": map[string]any{
						"kind":       "submit_request_form",
						"request_id": "req-1",
					},
				},
			},
		}},
	}, cardEnvelopeV2)
	assertRenderedCardPayloadBasicInvariants(t, payload)

	body, _ := payload["body"].(map[string]any)
	elements, _ := body["elements"].([]map[string]any)
	if len(elements) != 1 || elements[0]["tag"] != "form" {
		t.Fatalf("expected v2 form container, got %#v", payload)
	}
	formElements, _ := elements[0]["elements"].([]map[string]any)
	if len(formElements) != 2 {
		t.Fatalf("expected input and submit button inside v2 form, got %#v", elements[0])
	}
	submitButton := formElements[1]
	if submitButton["action_type"] != "form_submit" || submitButton["form_action_type"] != nil {
		t.Fatalf("expected renderer to preserve raw legacy form submit button instead of translating it, got %#v", submitButton)
	}
}
