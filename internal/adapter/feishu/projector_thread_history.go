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
	elements := threadHistoryElements(view, event.Meta.DaemonLifecycleID)
	operation := Operation{
		Kind:             OperationSendCard,
		GatewayID:        event.GatewayID(),
		SurfaceSessionID: event.SurfaceSessionID(),
		ChatID:           chatID,
		CardTitle:        title,
		CardBody:         "",
		CardThemeKey:     threadHistoryTheme(view),
		CardUpdateMulti:  true,
		CardElements:     elements,
		cardEnvelope:     cardEnvelopeV2,
		card:             rawCardDocument(title, "", threadHistoryTheme(view), elements),
	}
	if messageID := strings.TrimSpace(view.MessageID); messageID != "" {
		operation.Kind = OperationUpdateCard
		operation.MessageID = messageID
		operation.ReplyToMessageID = ""
	}
	return []Operation{operation}
}

func threadHistoryTheme(view control.FeishuThreadHistoryView) string {
	return projectorpkg.ThreadHistoryTheme(view)
}

func threadHistoryElements(view control.FeishuThreadHistoryView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.ThreadHistoryElements(view, daemonLifecycleID)
}

func threadHistoryListElements(view control.FeishuThreadHistoryView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.ThreadHistoryListElements(view, daemonLifecycleID)
}

func threadHistoryDetailElements(view control.FeishuThreadHistoryView, daemonLifecycleID string) []map[string]any {
	return projectorpkg.ThreadHistoryDetailElements(view, daemonLifecycleID)
}
