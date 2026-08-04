package orchestrator

import (
	"sort"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) MaterializeFeishuRoomState(records []state.FeishuRoomStateRecord) {
	if s == nil || s.root == nil {
		return
	}
	if s.root.FeishuRoomContexts == nil {
		s.root.FeishuRoomContexts = map[string]*state.FeishuRoomContextRecord{}
	}
	for _, record := range records {
		normalized, ok := state.NormalizeFeishuRoomStateRecord(record)
		if !ok {
			continue
		}
		room := s.root.FeishuRoomContexts[normalized.RoomID]
		if room == nil {
			room = &state.FeishuRoomContextRecord{
				RoomID:             normalized.RoomID,
				ChatID:             normalized.ChatID,
				GatewayIDs:         map[string]bool{},
				SurfaceSessionIDs:  map[string]bool{},
				ActiveReservations: map[string]*state.FeishuRoomActiveReservationRecord{},
			}
			s.root.FeishuRoomContexts[normalized.RoomID] = room
		}
		room.RoomID = normalized.RoomID
		room.ChatID = normalized.ChatID
		room.PrimaryGatewayID = normalized.PrimaryGatewayID
		room.PrimaryUpdatedBy = normalized.PrimaryUpdatedBy
		room.PrimaryUpdatedAt = normalized.PrimaryUpdatedAt
		room.WorkspaceKey = normalized.WorkspaceKey
		room.WorkspaceUpdatedBy = normalized.WorkspaceUpdatedBy
		room.WorkspaceUpdatedAt = normalized.WorkspaceUpdatedAt
		room.WorkspaceResetGeneration = normalized.WorkspaceResetGeneration
		room.ConcurrencyLimit = state.CloneFeishuRoomConcurrencyLimit(normalized.ConcurrencyLimit)
		if room.ActiveReservations == nil {
			room.ActiveReservations = map[string]*state.FeishuRoomActiveReservationRecord{}
		}
	}
}

func (s *Service) FeishuRoomState() []state.FeishuRoomStateRecord {
	if s == nil || s.root == nil || len(s.root.FeishuRoomContexts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(s.root.FeishuRoomContexts))
	for key := range s.root.FeishuRoomContexts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	records := make([]state.FeishuRoomStateRecord, 0, len(keys))
	for _, key := range keys {
		room := s.root.FeishuRoomContexts[key]
		if room == nil || room.PrimaryGatewayID == "" && room.WorkspaceKey == "" && room.WorkspaceResetGeneration == 0 && room.ConcurrencyLimit == nil {
			continue
		}
		record, ok := state.FeishuRoomStateRecordFromContext(room)
		if !ok {
			continue
		}
		records = append(records, record)
	}
	return records
}
