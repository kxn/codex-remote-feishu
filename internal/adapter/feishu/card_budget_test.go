package feishu

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestInteractiveMessageTransportAddsEnvelopeOverRawCardJSON(t *testing.T) {
	payload := renderOperationCard(Operation{
		Kind:         OperationSendCard,
		CardTitle:    "预算测试",
		CardThemeKey: cardThemeInfo,
		CardElements: []map[string]any{
			{
				"tag":     "markdown",
				"content": "**说明**",
			},
			{
				"tag":     "markdown",
				"content": strings.Repeat("x", 2048),
			},
		},
	}, cardEnvelopeV2)
	rawSize, err := jsonSize(payload)
	if err != nil {
		t.Fatalf("measure raw payload: %v", err)
	}
	transportSize, err := feishuInteractiveMessageTransportSize(payload)
	if err != nil {
		t.Fatalf("measure message transport payload: %v", err)
	}
	if transportSize <= rawSize {
		t.Fatalf("expected transport envelope (%d) to exceed raw payload (%d)", transportSize, rawSize)
	}
}

func TestMessageTransportBudgetIsTighterThanInlineCallbackBudget(t *testing.T) {
	payload := findCallbackOnlyBudgetPayload(t)
	messageSize, err := feishuInteractiveMessageTransportSize(payload)
	if err != nil {
		t.Fatalf("measure message transport payload: %v", err)
	}
	callbackSize, err := feishuInlineCallbackTransportSize(payload)
	if err != nil {
		t.Fatalf("measure callback transport payload: %v", err)
	}
	if messageSize <= callbackSize {
		t.Fatalf("expected message transport (%d) to exceed callback transport (%d)", messageSize, callbackSize)
	}
	if feishuInteractiveMessageTransportFits(payload) {
		t.Fatalf("expected payload to overflow message transport budget")
	}
	if !feishuInlineCallbackTransportFits(payload) {
		t.Fatalf("expected payload to still fit callback transport budget")
	}

	messageTrimmed := trimCardPayloadForMessageTransport(payload)
	if reflect.DeepEqual(messageTrimmed, payload) {
		t.Fatalf("expected message transport trim to reduce payload")
	}
	if !feishuInteractiveMessageTransportFits(messageTrimmed) {
		trimmedSize, sizeErr := feishuInteractiveMessageTransportSize(messageTrimmed)
		t.Fatalf("expected trimmed message payload to fit transport budget, got size=%d err=%v", trimmedSize, sizeErr)
	}

	callbackTrimmed := trimCardPayloadForInlineCallback(payload)
	if !reflect.DeepEqual(callbackTrimmed, payload) {
		t.Fatalf("expected callback transport to keep full payload when it still fits")
	}
}

func findCallbackOnlyBudgetPayload(t *testing.T) map[string]any {
	t.Helper()

	elements := []map[string]any{{
		"tag":     "markdown",
		"content": "**工作区列表**",
	}}
	for i := 1; i <= 400; i++ {
		workspace := fmt.Sprintf("ws-%03d", i)
		elements = append(elements,
			cardCallbackButtonElement("恢复 · "+workspace, "default", map[string]any{
				"kind":          "show_workspace_threads",
				"workspace_key": workspace,
			}, false, "fill"),
			map[string]any{
				"tag":     "markdown",
				"content": fmt.Sprintf("说明-%03d %s", i, strings.Repeat("x", 80)),
			},
		)
		payload := renderOperationCard(Operation{
			Kind:         OperationSendCard,
			CardTitle:    "工作区列表",
			CardThemeKey: cardThemeInfo,
			CardElements: elements,
		}, cardEnvelopeV2)
		if feishuInlineCallbackTransportFits(payload) && !feishuInteractiveMessageTransportFits(payload) {
			return payload
		}
	}
	t.Fatal("failed to construct callback-only budget payload")
	return nil
}
