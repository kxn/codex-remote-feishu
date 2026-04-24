package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) PauseSurfaceDispatch(surfaceID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
	if surface.DispatchMode == state.DispatchModeNormal {
		surface.DispatchMode = state.DispatchModePausedForLocal
	}
}

func (s *Service) ResumeSurfaceDispatch(surfaceID string, notice *control.Notice) []eventcontract.Event {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil
	}
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
	if surface.DispatchMode == state.DispatchModePausedForLocal {
		surface.DispatchMode = state.DispatchModeNormal
	}
	var events []eventcontract.Event
	if notice != nil && (strings.TrimSpace(notice.Code) != "" || strings.TrimSpace(notice.Title) != "" || strings.TrimSpace(notice.Text) != "") {
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	events = append(events, s.dispatchNext(surface)...)
	return s.filterEventsForSurfaceVisibility(events)
}
