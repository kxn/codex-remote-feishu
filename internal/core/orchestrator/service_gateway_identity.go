package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PurgeGatewayIdentityState removes state owned by one bot identity while
// retaining room-owned workspace state.
func (s *Service) PurgeGatewayIdentityState(gatewayID string) []string {
	if s == nil || s.root == nil {
		return nil
	}
	gatewayID = strings.TrimSpace(gatewayID)
	if gatewayID == "" {
		return nil
	}

	surfaceIDs := make([]string, 0)
	for surfaceID, surface := range s.root.Surfaces {
		if surface != nil && strings.TrimSpace(surface.GatewayID) == gatewayID {
			surfaceIDs = append(surfaceIDs, surfaceID)
		}
	}
	sort.Strings(surfaceIDs)
	for _, surfaceID := range surfaceIDs {
		s.purgeGatewayIdentitySurface(surfaceID)
	}

	delete(s.root.BotCapabilitySettings, state.BotCapabilitySettingsKey(gatewayID))
	for roomID, room := range s.root.FeishuRoomContexts {
		if room == nil {
			continue
		}
		delete(room.GatewayIDs, gatewayID)
		if strings.TrimSpace(room.PrimaryGatewayID) == gatewayID {
			room.PrimaryGatewayID = ""
			room.PrimaryUpdatedBy = ""
			room.PrimaryUpdatedAt = time.Time{}
		}
		if room.WorkspaceKey == "" && room.WorkspaceResetGeneration == 0 && room.PrimaryGatewayID == "" && len(room.SurfaceSessionIDs) == 0 {
			delete(s.root.FeishuRoomContexts, roomID)
		}
	}
	return surfaceIDs
}

func (s *Service) purgeGatewayIdentitySurface(surfaceID string) {
	surfaceID = strings.TrimSpace(surfaceID)
	surface := s.root.Surfaces[surfaceID]
	if surface == nil {
		return
	}
	ownedRuntimeInstances := s.turns.runtimeInstancesForSurface(surfaceID)
	_ = s.finalizeDetachedSurface(surface)
	s.turns.purgeSurface(surfaceID)
	s.progress.purgeSurface(surfaceID, ownedRuntimeInstances)
	for instanceID := range ownedRuntimeInstances {
		s.clearItemBuffersForInstance(instanceID)
	}
	delete(s.surfaceUIRuntime, surfaceID)
	s.clearActiveNoticesForSurface(surfaceID)
	delete(s.handoffUntil, surfaceID)
	delete(s.pausedUntil, surfaceID)
	delete(s.abandoningUntil, surfaceID)
	delete(s.root.Surfaces, surfaceID)

	for _, room := range s.root.FeishuRoomContexts {
		if room == nil {
			continue
		}
		delete(room.SurfaceSessionIDs, surfaceID)
		for reservationID, reservation := range room.ActiveReservations {
			if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == surfaceID {
				delete(room.ActiveReservations, reservationID)
			}
		}
	}
}
