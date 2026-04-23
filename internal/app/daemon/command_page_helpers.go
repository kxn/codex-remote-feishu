package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func commandPageEvent(surfaceID string, view control.FeishuPageView) eventcontract.Event {
	page := control.FeishuPageViewFromCommandPageView(control.NormalizeFeishuPageView(view))
	return surfacePagePayloadEvent(surfaceID, eventcontract.PagePayload{View: page}, false)
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

func surfacePagePayloadEvent(surfaceID string, payload eventcontract.PagePayload, inlineReplace bool) eventcontract.Event {
	inlineReplaceMode := eventcontract.InlineReplaceNone
	if inlineReplace {
		inlineReplaceMode = eventcontract.InlineReplaceCurrentCard
	}
	return eventcontract.NewEventFromPayload(
		payload,
		eventcontract.EventMeta{
			Target: eventcontract.TargetRef{
				SurfaceSessionID: strings.TrimSpace(surfaceID),
			},
			InlineReplaceMode: inlineReplaceMode,
		},
	)
}
