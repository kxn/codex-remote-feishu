package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) PauseSurfaceDispatch(surfaceID string) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	s.pauseSurfaceDispatchForLocal(surface, time.Time{})
}

func (s *Service) ResumeSurfaceDispatch(surfaceID string, notice *control.Notice) []eventcontract.Event {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil
	}
	s.restoreSurfaceDispatchNormal(surface)
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
