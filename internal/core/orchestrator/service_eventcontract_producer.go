package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontractcompat"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func legacyUIEventFromContract(
	surface *state.SurfaceConsoleRecord,
	payload eventcontract.Payload,
	semantics eventcontract.DeliverySemantics,
	inlineReplaceCurrentCard bool,
	sourceMessageID string,
	sourceMessagePreview string,
) control.UIEvent {
	target := eventcontract.TargetRef{
		GatewayID:        strings.TrimSpace(firstNonEmpty(surfaceGatewayID(surface))),
		SurfaceSessionID: strings.TrimSpace(firstNonEmpty(surfaceSessionID(surface))),
	}
	event := eventcontract.Event{
		Meta: eventcontract.EventMeta{
			Target:               target,
			SourceMessageID:      strings.TrimSpace(sourceMessageID),
			SourceMessagePreview: strings.TrimSpace(sourceMessagePreview),
			InlineReplaceMode:    inlineReplaceMode(inlineReplaceCurrentCard),
			Semantics:            semantics,
		},
		Payload: payload,
	}
	return eventcontractcompat.ToLegacyUIEvent(event)
}

func inlineReplaceMode(inline bool) eventcontract.InlineReplaceMode {
	if inline {
		return eventcontract.InlineReplaceCurrentCard
	}
	return eventcontract.InlineReplaceNone
}

func surfaceGatewayID(surface *state.SurfaceConsoleRecord) string {
	if surface == nil {
		return ""
	}
	return strings.TrimSpace(surface.GatewayID)
}

func surfaceSessionID(surface *state.SurfaceConsoleRecord) string {
	if surface == nil {
		return ""
	}
	return strings.TrimSpace(surface.SurfaceSessionID)
}

func noticeDeliverySemantics(notice control.Notice, hasThreadSelection bool) eventcontract.DeliverySemantics {
	semantics := eventcontract.DeliverySemantics{
		VisibilityClass:        eventcontract.VisibilityClassUINavigation,
		HandoffClass:           eventcontract.HandoffClassNotice,
		FirstResultDisposition: eventcontract.FirstResultDispositionDrop,
		OwnerCardDisposition:   eventcontract.OwnerCardDispositionDrop,
	}
	if noticeIsAlwaysVisible(notice) {
		semantics.VisibilityClass = eventcontract.VisibilityClassAlwaysVisible
	}
	if hasThreadSelection {
		semantics.HandoffClass = eventcontract.HandoffClassThreadSelection
	}
	return semantics
}

func navigationDeliverySemantics() eventcontract.DeliverySemantics {
	return eventcontract.DeliverySemantics{
		VisibilityClass:        eventcontract.VisibilityClassUINavigation,
		HandoffClass:           eventcontract.HandoffClassNavigation,
		FirstResultDisposition: eventcontract.FirstResultDispositionKeep,
		OwnerCardDisposition:   eventcontract.OwnerCardDispositionKeep,
	}
}

func terminalDeliverySemantics() eventcontract.DeliverySemantics {
	return eventcontract.DeliverySemantics{
		VisibilityClass:        eventcontract.VisibilityClassAlwaysVisible,
		HandoffClass:           eventcontract.HandoffClassTerminalContent,
		FirstResultDisposition: eventcontract.FirstResultDispositionKeep,
		OwnerCardDisposition:   eventcontract.OwnerCardDispositionKeep,
	}
}
