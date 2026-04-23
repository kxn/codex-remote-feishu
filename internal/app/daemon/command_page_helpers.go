package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func commandPageEvent(surfaceID string, view control.FeishuPageView) eventcontract.Event {
	page := control.FeishuPageViewFromCommandPageView(control.NormalizeFeishuPageView(view))
	return eventcontract.Event{
		Kind:             eventcontract.EventFeishuPageView,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		PageView:         &page,
	}
}

func commandPageEvents(surfaceID string, view control.FeishuPageView) []eventcontract.Event {
	return []eventcontract.Event{commandPageEvent(surfaceID, view)}
}

func commandArgumentText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	idx := strings.IndexAny(text, " \t")
	if idx < 0 || idx+1 >= len(text) {
		return ""
	}
	return strings.TrimSpace(text[idx+1:])
}
