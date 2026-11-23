package gateway

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
)

func predictionIDFromPage(kind control.ActionKind, value map[string]any) string {
	if kind != control.ActionPredictionAccept && kind != control.ActionPredictionSend {
		return ""
	}
	return strings.TrimSpace(stringMapValue(value, frontstagecontract.CardActionPayloadKeyRequestID))
}

func predictionPageText(kind control.ActionKind, argument string) string {
	if kind == control.ActionPredictionSend {
		return strings.TrimSpace(argument)
	}
	return control.BuildFeishuActionText(kind, argument)
}
