package orchestrator

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func feishuRoomContextID(chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	return "feishu:chat:" + chatID
}

func surfaceFeishuRoomID(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || !surfaceIsFeishuChat(surface) {
		return ""
	}
	return feishuRoomContextID(surface.ChatID)
}

func (s *Service) ensureFeishuRoomContextForSurface(surface *state.SurfaceConsoleRecord) *state.FeishuRoomContextRecord {
	if s == nil || s.root == nil {
		return nil
	}
	roomID := surfaceFeishuRoomID(surface)
	if roomID == "" {
		return nil
	}
	if s.root.FeishuRoomContexts == nil {
		s.root.FeishuRoomContexts = map[string]*state.FeishuRoomContextRecord{}
	}
	room := s.root.FeishuRoomContexts[roomID]
	if room == nil {
		room = &state.FeishuRoomContextRecord{
			RoomID:            roomID,
			ChatID:            strings.TrimSpace(surface.ChatID),
			GatewayIDs:        map[string]bool{},
			SurfaceSessionIDs: map[string]bool{},
		}
		s.root.FeishuRoomContexts[roomID] = room
	}
	if room.GatewayIDs == nil {
		room.GatewayIDs = map[string]bool{}
	}
	if room.SurfaceSessionIDs == nil {
		room.SurfaceSessionIDs = map[string]bool{}
	}
	if gatewayID := strings.TrimSpace(surface.GatewayID); gatewayID != "" {
		room.GatewayIDs[gatewayID] = true
	}
	if surfaceID := strings.TrimSpace(surface.SurfaceSessionID); surfaceID != "" {
		room.SurfaceSessionIDs[surfaceID] = true
	}
	return room
}

func (s *Service) feishuRoomSurfaces(roomID string) []*state.SurfaceConsoleRecord {
	if s == nil || s.root == nil {
		return nil
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return nil
	}
	room := s.root.FeishuRoomContexts[roomID]
	if room == nil {
		return nil
	}
	surfaces := make([]*state.SurfaceConsoleRecord, 0, len(room.SurfaceSessionIDs))
	for surfaceID := range room.SurfaceSessionIDs {
		surface := s.root.Surfaces[surfaceID]
		if surface != nil && surfaceFeishuRoomID(surface) == roomID {
			surfaces = append(surfaces, surface)
		}
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].SurfaceSessionID < surfaces[j].SurfaceSessionID
	})
	return surfaces
}

func surfaceIsFeishuChat(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || strings.TrimSpace(surface.ChatID) == "" {
		return false
	}
	if surface.Platform != "" && surface.Platform != "feishu" {
		return false
	}
	parts := strings.Split(strings.TrimSpace(surface.SurfaceSessionID), ":")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "chat" && strings.TrimSpace(parts[i+1]) != "" {
			return true
		}
	}
	return false
}
