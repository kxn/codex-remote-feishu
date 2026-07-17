package feishu

import (
	"strings"

	projectorpkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/projector"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (p *Projector) projectThreadHistory(chatID string, event eventcontract.Event, view control.FeishuThreadHistoryView) []Operation {
	title := strings.TrimSpace(view.Title)
	if title == "" {
		title = "历史记录"
	}
	elements := projectorpkg.ThreadHistoryElements(view, event.Meta.DaemonLifecycleID)
	theme := projectorpkg.ThreadHistoryTheme(view)
	operation := Operation{
		Kind:             OperationSendCard,
		GatewayID:        event.GatewayID,
		SurfaceSessionID: event.SurfaceSessionID,
		ChatID:           chatID,
		CardTitle:        title,
		CardBody:         "",
		CardThemeKey:     theme,
		CardUpdateMulti:  true,
		CardElements:     elements,
		cardEnvelope:     cardEnvelopeV2,
		card:             rawCardDocument(title, "", theme, elements),
	}
	if messageID := strings.TrimSpace(view.MessageID); messageID != "" {
		operation.Kind = OperationUpdateCard
		operation.MessageID = messageID
		operation.ReplyToMessageID = ""
	}
	if operation.Kind == OperationSendCard {
		operation = applyReplyLaneToNewOperation(event, operation)
	}
	return []Operation{operation}
}
