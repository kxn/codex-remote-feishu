package daemon

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func ownerCardFlowMessageID(messageID string) string {
	return strings.TrimSpace(messageID)
}

func ownerCardFlowTrackingKey(flowID, messageID string) string {
	if strings.TrimSpace(messageID) != "" {
		return ""
	}
	return strings.TrimSpace(flowID)
}

func ownerCardPageEvent(surfaceID, messageID, trackingKey, title, theme string, bodySections, noticeSections []control.FeishuCardTextSection, buttons []control.CommandCatalogButton, sealed bool) eventcontract.Event {
	interactive := len(buttons) > 0 && !sealed
	view := control.NormalizeFeishuPageView(control.FeishuPageView{
		Title:          strings.TrimSpace(title),
		MessageID:      ownerCardFlowMessageID(messageID),
		TrackingKey:    strings.TrimSpace(trackingKey),
		ThemeKey:       strings.TrimSpace(theme),
		Patchable:      true,
		BodySections:   append([]control.FeishuCardTextSection(nil), bodySections...),
		NoticeSections: append([]control.FeishuCardTextSection(nil), noticeSections...),
		Interactive:    interactive,
		Sealed:         sealed,
		RelatedButtons: append([]control.CommandCatalogButton(nil), buttons...),
	})
	return surfacePagePayloadEvent(surfaceID, eventcontract.PagePayload{View: view}, false)
}
