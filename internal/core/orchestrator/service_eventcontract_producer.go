package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func surfaceEventFromPayload(
	surface *state.SurfaceConsoleRecord,
	payload eventcontract.Payload,
	meta eventcontract.EventMeta,
) eventcontract.Event {
	target := meta.Target.Normalized()
	if target.GatewayID == "" {
		target.GatewayID = strings.TrimSpace(firstNonEmpty(surfaceGatewayID(surface)))
	}
	if target.SurfaceSessionID == "" {
		target.SurfaceSessionID = strings.TrimSpace(firstNonEmpty(surfaceSessionID(surface)))
	}
	meta.Target = target
	return eventcontract.NewEventFromPayload(
		payload,
		meta,
	)
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
