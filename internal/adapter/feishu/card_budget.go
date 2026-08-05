package feishu

import (
	cardtransport "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardtransport"
)

func feishuInteractiveMessageTransportFits(payload map[string]any) bool {
	return cardtransport.InteractiveMessagePayloadFits(payload)
}

func feishuInlineCallbackTransportFits(payload map[string]any) bool {
	return cardtransport.InlineCallbackPayloadFits(payload)
}
