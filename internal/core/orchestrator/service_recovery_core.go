package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) clearSurfaceDispatchWaits(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
}

func (s *Service) resetSurfaceExecutionGates(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActiveTurnOrigin = ""
	s.restoreSurfaceDispatchNormal(surface)
	surface.Abandoning = false
	delete(s.abandoningUntil, surface.SurfaceSessionID)
}

func (s *Service) prepareSurfaceForExecutionReattach(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.prepareSurfaceForExecutionReattachWithOverlayCleanup(surface, surfaceOverlayRouteCleanupOptions{})
}

func (s *Service) prepareSurfaceForExecutionReattachWithOverlayCleanup(surface *state.SurfaceConsoleRecord, cleanup surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	events := s.discardDrafts(surface)
	if strings.TrimSpace(surface.AttachedInstanceID) != "" {
		events = append(events, s.finalizeDetachedSurfaceWithOverlayCleanup(surface, cleanup)...)
	} else {
		events = append(events, s.cleanupContextBoundSurfaceOverlays(surface, "当前工作目标已变化", surfaceOverlayRouteCleanupOptions{
			PreserveTargetPicker:  cleanup.PreserveTargetPicker,
			ForceClearReviewState: true,
		})...)
		clearAutoContinueRuntime(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
	}
	surface.PromptOverride = state.ModelConfigRecord{}
	s.consumeSurfacePendingHeadlessLaunch(surface, "")
	s.clearSurfaceActiveQueueItem(surface, "")
	s.resetSurfaceExecutionGates(surface)
	return events
}

func (s *Service) pendingSurfaceHeadlessLaunch(surface *state.SurfaceConsoleRecord, instanceID string) *state.HeadlessLaunchRecord {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID != "" && strings.TrimSpace(surface.PendingHeadless.InstanceID) != instanceID {
		return nil
	}
	return surface.PendingHeadless
}

func (s *Service) adoptSurfacePendingHeadlessLaunch(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) {
	if surface == nil {
		return
	}
	s.resetSurfaceExecutionGates(surface)
	surface.PendingHeadless = pending
}

func (s *Service) consumeSurfacePendingHeadlessLaunch(surface *state.SurfaceConsoleRecord, instanceID string) *state.HeadlessLaunchRecord {
	pending := s.pendingSurfaceHeadlessLaunch(surface, instanceID)
	if pending == nil {
		return nil
	}
	surface.PendingHeadless = nil
	return pending
}

func (s *Service) setSurfaceDetachAbandoning(surface *state.SurfaceConsoleRecord, until time.Time) {
	if surface == nil {
		return
	}
	surface.Abandoning = true
	if !until.IsZero() {
		s.abandoningUntil[surface.SurfaceSessionID] = until
	}
}
