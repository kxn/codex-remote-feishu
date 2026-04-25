package feishu

import (
	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
)

const (
	// Feishu documents the request-body ceiling for interactive card messages as 30KB.
	// We measure against the actual serialized transport envelope instead of the raw
	// inner card JSON payload.
	feishuCardTransportLimitBytes = cardtransport.InteractiveCardTransportLimitBytes
)

func feishuInteractiveMessageTransportFits(payload map[string]any) bool {
	return cardtransport.InteractiveMessagePayloadFits(payload)
}

func feishuInteractiveMessageTransportSize(payload map[string]any) (int, error) {
	return cardtransport.InteractiveMessagePayloadSize(payload)
}

func feishuInteractiveMessageContentTransportSize(content string) (int, error) {
	return cardtransport.InteractiveMessageContentSize(content)
}

func feishuInlineCallbackTransportFits(payload map[string]any) bool {
	return cardtransport.InlineCallbackPayloadFits(payload)
}

func feishuInlineCallbackTransportSize(payload map[string]any) (int, error) {
	return cardtransport.InlineCallbackPayloadSize(payload)
}
