package state

import "strings"

const DefaultFeishuRoomConcurrencyLimit = 1

func FeishuRoomConcurrencyLimit(value *int) int {
	if value == nil || *value < 0 {
		return DefaultFeishuRoomConcurrencyLimit
	}
	return *value
}

func NormalizeFeishuRoomConcurrencyLimit(value *int) *int {
	if value == nil {
		return nil
	}
	normalized := *value
	if normalized < 0 {
		normalized = DefaultFeishuRoomConcurrencyLimit
	}
	return &normalized
}

func CloneFeishuRoomConcurrencyLimit(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func FeishuRoomKey(roomOrChatID string) string {
	value := strings.TrimSpace(roomOrChatID)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "feishu:chat:") {
		return value
	}
	return "feishu:chat:" + value
}

func NormalizeFeishuRoomStateRecord(record FeishuRoomStateRecord) (FeishuRoomStateRecord, bool) {
	record.RoomID = FeishuRoomKey(record.RoomID)
	record.ChatID = strings.TrimSpace(record.ChatID)
	if record.RoomID == "" && record.ChatID != "" {
		record.RoomID = FeishuRoomKey(record.ChatID)
	}
	if record.ChatID == "" && strings.HasPrefix(record.RoomID, "feishu:chat:") {
		record.ChatID = strings.TrimPrefix(record.RoomID, "feishu:chat:")
	}
	if record.RoomID == "" || record.ChatID == "" {
		return FeishuRoomStateRecord{}, false
	}
	record.PrimaryGatewayID = strings.TrimSpace(record.PrimaryGatewayID)
	record.PrimaryUpdatedBy = strings.TrimSpace(record.PrimaryUpdatedBy)
	record.ConcurrencyLimit = NormalizeFeishuRoomConcurrencyLimit(record.ConcurrencyLimit)
	record.WorkspaceKey = ResolveWorkspaceClaimKey(record.WorkspaceKey)
	record.WorkspaceUpdatedBy = strings.TrimSpace(record.WorkspaceUpdatedBy)
	if record.WorkspaceResetGeneration < 0 {
		record.WorkspaceResetGeneration = 0
	}
	if !record.WorkspaceUpdatedAt.IsZero() {
		record.WorkspaceUpdatedAt = record.WorkspaceUpdatedAt.UTC()
	}
	if !record.PrimaryUpdatedAt.IsZero() {
		record.PrimaryUpdatedAt = record.PrimaryUpdatedAt.UTC()
	}
	return record, true
}

func FeishuRoomStateRecordFromContext(room *FeishuRoomContextRecord) (FeishuRoomStateRecord, bool) {
	if room == nil {
		return FeishuRoomStateRecord{}, false
	}
	return NormalizeFeishuRoomStateRecord(FeishuRoomStateRecord{
		RoomID:                   room.RoomID,
		ChatID:                   room.ChatID,
		WorkspaceKey:             room.WorkspaceKey,
		WorkspaceUpdatedBy:       room.WorkspaceUpdatedBy,
		WorkspaceUpdatedAt:       room.WorkspaceUpdatedAt,
		WorkspaceResetGeneration: room.WorkspaceResetGeneration,
		PrimaryGatewayID:         room.PrimaryGatewayID,
		PrimaryUpdatedBy:         room.PrimaryUpdatedBy,
		PrimaryUpdatedAt:         room.PrimaryUpdatedAt,
		ConcurrencyLimit:         CloneFeishuRoomConcurrencyLimit(room.ConcurrencyLimit),
	})
}
