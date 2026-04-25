package feishu

import (
	"encoding/json"

	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	// Feishu documents the request-body ceiling for interactive card messages as 30KB.
	// We measure against the actual serialized transport envelope instead of the raw
	// inner card JSON payload.
	feishuCardTransportLimitBytes = 30_000

	// Create-message bodies include receive_id, while reply/patch use a smaller outer
	// envelope. Use a stable placeholder here so split-time budget checks stay aligned
	// with the strictest message transport even when the concrete receive target is not
	// known yet.
	feishuBudgetMeasureReceiveID = "oc_budget_measure_receive_target_12345678901234567890"
)

func feishuInteractiveMessageTransportFits(payload map[string]any) bool {
	size, err := feishuInteractiveMessageTransportSize(payload)
	return err == nil && size <= feishuCardTransportLimitBytes
}

func feishuInteractiveMessageTransportSize(payload map[string]any) (int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	return feishuInteractiveMessageContentTransportSize(string(data))
}

func feishuInteractiveMessageContentTransportSize(content string) (int, error) {
	bodies := []any{
		larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(feishuBudgetMeasureReceiveID).
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

func feishuInlineCallbackTransportFits(payload map[string]any) bool {
	size, err := feishuInlineCallbackTransportSize(payload)
	return err == nil && size <= feishuCardTransportLimitBytes
}

func feishuInlineCallbackTransportSize(payload map[string]any) (int, error) {
	response := &larkcallback.CardActionTriggerResponse{
		Card: &larkcallback.Card{
			Type: "raw",
			Data: payload,
		},
	}
	return jsonSize(response)
}
