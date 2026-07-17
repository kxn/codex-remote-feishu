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
	return []Operation{newEventCardOperation(chatID, event, eventCardOperationSpec{
		Title:          title,
		ThemeKey:       theme,
		Elements:       elements,
		MessageID:      view.MessageID,
		UpdateMulti:    true,
		ApplyReplyLane: true,
	})}
}
