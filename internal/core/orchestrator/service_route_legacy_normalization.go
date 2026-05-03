package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) normalizeLegacyNormalFollowRoute(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.ProductMode != state.ProductModeNormal || surface.RouteMode != state.RouteModeFollowLocal {
		return
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	threadID := surface.SelectedThreadID
	if inst != nil && threadID != "" {
		if owner := s.threadClaimSurface(threadID); owner == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
			if s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
				AttachedInstanceID: inst.InstanceID,
				RouteMode:          state.RouteModePinned,
				SelectedThreadID:   threadID,
				ThreadClaimPolicy:  surfaceRouteThreadClaimKnown,
			}) {
				thread := s.ensureThread(inst, threadID)
				surface.LastSelection = &state.SelectionAnnouncementRecord{
					ThreadID:  threadID,
					RouteMode: string(state.RouteModePinned),
					Title:     displayThreadTitle(inst, thread, threadID),
					Preview:   threadPreview(thread),
				}
				return
			}
		}
	}
	s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID: strings.TrimSpace(surface.AttachedInstanceID),
		RouteMode:          state.RouteModeUnbound,
	})
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(state.RouteModeUnbound),
		Title:     "未选择会话",
		Preview:   "",
	}
}

func (s *Service) normalizeLegacyVSCodePreparedNewThread(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.ProductMode != state.ProductModeVSCode || surface.RouteMode != state.RouteModeNewThreadReady {
		return
	}
	if s.preparedNewThreadHasPendingCreate(surface) {
		return
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID: strings.TrimSpace(surface.AttachedInstanceID),
		RouteMode:          state.RouteModeFollowLocal,
	}) {
		return
	}

	if inst != nil {
		targetThreadID := strings.TrimSpace(inst.ObservedFocusedThreadID)
		if targetThreadID != "" && threadVisible(inst.Threads[targetThreadID]) {
			if owner := s.threadClaimSurface(targetThreadID); owner == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
				if s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
					AttachedInstanceID: inst.InstanceID,
					RouteMode:          state.RouteModeFollowLocal,
					SelectedThreadID:   targetThreadID,
					ThreadClaimPolicy:  surfaceRouteThreadClaimKnown,
				}) {
					thread := s.ensureThread(inst, targetThreadID)
					surface.LastSelection = &state.SelectionAnnouncementRecord{
						ThreadID:  targetThreadID,
						RouteMode: string(state.RouteModeFollowLocal),
						Title:     displayThreadTitle(inst, thread, targetThreadID),
						Preview:   threadPreview(thread),
					}
					return
				}
			}
		}
	}

	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(state.RouteModeFollowLocal),
		Title:     "跟随当前 VS Code（等待中）",
		Preview:   "",
	}
}
