package cardtransport

import (
	"encoding/json"
	"strings"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	InteractiveCardTransportLimitBytes = 30_000

	budgetMeasureReceiveID = "oc_budget_measure_receive_target_12345678901234567890"
)

func InteractiveMessagePayloadFits(payload map[string]any) bool {
	size, err := InteractiveMessagePayloadSize(payload)
	return err == nil && size <= InteractiveCardTransportLimitBytes
}

func InteractiveMessagePayloadSize(payload map[string]any) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	return InteractiveMessageContentSize(string(data))
}

func InteractiveMessageContentSize(content string) (int, error) {
	bodies := []any{
		larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(budgetMeasureReceiveID).
			MsgType("interactive").
			Content(content).
			Build(),
		larkim.NewReplyMessageReqBodyBuilder().
			MsgType("interactive").
			Content(content).
			Build(),
		larkim.NewPatchMessageReqBodyBuilder().
			Content(content).
			Build(),
	}
	maxSize := 0
	for _, body := range bodies {
		size, err := jsonSize(body)
		if err != nil {
			return 0, err
		}
		if size > maxSize {
			maxSize = size
		}
	}
	return maxSize, nil
}

func InlineCallbackPayloadFits(payload map[string]any) bool {
	size, err := InlineCallbackPayloadSize(payload)
	return err == nil && size <= InteractiveCardTransportLimitBytes
}

func InlineCallbackPayloadSize(payload map[string]any) (int, error) {
	response := &larkcallback.CardActionTriggerResponse{
		Card: &larkcallback.Card{
			Type: "raw",
			Data: payload,
		},
	}
	return jsonSize(response)
}

func InteractiveMessageCardFits(title, body, themeKey string, elements []map[string]any, updateMulti bool) bool {
	return InteractiveMessagePayloadFits(RenderInteractiveCardPayload(title, body, themeKey, elements, updateMulti))
}

func InteractiveMessageCardSize(title, body, themeKey string, elements []map[string]any, updateMulti bool) (int, error) {
	return InteractiveMessagePayloadSize(RenderInteractiveCardPayload(title, body, themeKey, elements, updateMulti))
}

func RenderInteractiveCardPayload(title, body, themeKey string, elements []map[string]any, updateMulti bool) map[string]any {
	renderedElements := make([]map[string]any, 0, len(elements)+1)
	if strings.TrimSpace(body) != "" {
		renderedElements = append(renderedElements, map[string]any{
			"tag":     "markdown",
			"content": strings.TrimSpace(body),
		})
	}
	for _, element := range elements {
		if len(element) == 0 {
			continue
		}
		renderedElements = append(renderedElements, cloneCardMap(element))
	}
	config := map[string]any{
		"width_mode":     "fill",
		"enable_forward": true,
	}
	if updateMulti {
		config["update_multi"] = true
	}
	title = strings.TrimSpace(title)
	return map[string]any{
		"schema": "2.0",
		"config": config,
		"header": map[string]any{
			"template": cardTemplate(themeKey, title),
			"title": map[string]any{
				"tag":     "plain_text",
				"content": title,
			},
		},
		"body": map[string]any{
			"elements": renderedElements,
		},
	}
}

func cardTemplate(themeKey, fallback string) string {
	key := strings.ToLower(strings.TrimSpace(themeKey))
	if key == "" {
		key = strings.ToLower(strings.TrimSpace(fallback))
	}
	switch {
	case key == "progress":
		return "wathet"
	case key == "plan":
		return "blue"
	case key == "final":
		return "blue"
	case key == "success", key == "approval":
		return "green"
	case key == "error" || strings.Contains(key, "error") || strings.Contains(key, "fail") || strings.Contains(key, "reject"):
		return "red"
	default:
		return "grey"
	}
}

func jsonSize(value any) (int, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return len(data), nil
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
