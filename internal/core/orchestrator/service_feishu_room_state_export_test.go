package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func intPointer(value int) *int { return &value }

func TestMaterializeFeishuRoomState(t *testing.T) {
	svc := NewService(nil, Config{}, nil)
	updatedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:                   "feishu:chat:oc_room",
		ChatID:                   "oc_room",
		WorkspaceKey:             "/data/dl/workspace",
		WorkspaceUpdatedBy:       "ou_workspace",
		WorkspaceUpdatedAt:       updatedAt,
		WorkspaceResetGeneration: 3,
		PrimaryGatewayID:         "app-1",
		PrimaryUpdatedBy:         "ou_user",
		PrimaryUpdatedAt:         updatedAt,
		ConcurrencyLimit:         intPointer(0),
	}})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room == nil {
		t.Fatal("expected room context to be materialized")
	}
	if room.PrimaryGatewayID != "app-1" || room.PrimaryUpdatedBy != "ou_user" || !room.PrimaryUpdatedAt.Equal(updatedAt) {
		t.Fatalf("primary state = %#v, want app-1/ou_user/%v", room, updatedAt)
	}
	if room.WorkspaceKey != "/data/dl/workspace" || room.WorkspaceUpdatedBy != "ou_workspace" || !room.WorkspaceUpdatedAt.Equal(updatedAt) || room.WorkspaceResetGeneration != 3 {
		t.Fatalf("workspace state = %#v, want durable workspace metadata", room)
	}
	if len(room.GatewayIDs) != 0 || len(room.SurfaceSessionIDs) != 0 || len(room.ActiveReservations) != 0 {
		t.Fatalf("materialized durable primary state should not invent runtime evidence: %#v", room)
	}
	if room.ConcurrencyLimit == nil || *room.ConcurrencyLimit != 0 {
		t.Fatalf("materialized concurrency limit = %#v, want explicit unlimited", room.ConcurrencyLimit)
	}
}

func TestFeishuRoomStateExportsOnlyRoomsWithDurableState(t *testing.T) {
	svc := NewService(nil, Config{}, nil)
	svc.root.FeishuRoomContexts["feishu:chat:oc_b"] = &state.FeishuRoomContextRecord{
		RoomID:           "feishu:chat:oc_b",
		ChatID:           "oc_b",
		PrimaryGatewayID: "app-b",
	}
	svc.root.FeishuRoomContexts["feishu:chat:oc_a"] = &state.FeishuRoomContextRecord{
		RoomID:           "feishu:chat:oc_a",
		ChatID:           "oc_a",
		PrimaryGatewayID: "app-a",
	}
	svc.root.FeishuRoomContexts["feishu:chat:oc_empty"] = &state.FeishuRoomContextRecord{
		RoomID: "feishu:chat:oc_empty",
		ChatID: "oc_empty",
	}

	records := svc.FeishuRoomState()
	if len(records) != 2 {
		t.Fatalf("exported records = %d, want 2: %#v", len(records), records)
	}
	if records[0].RoomID != "feishu:chat:oc_a" || records[1].RoomID != "feishu:chat:oc_b" {
		t.Fatalf("records not sorted by room id: %#v", records)
	}
}

func TestFeishuRoomStateExportsWorkspaceOnlyRooms(t *testing.T) {
	svc := NewService(nil, Config{}, nil)
	svc.root.FeishuRoomContexts["feishu:chat:oc_workspace"] = &state.FeishuRoomContextRecord{
		RoomID:             "feishu:chat:oc_workspace",
		ChatID:             "oc_workspace",
		WorkspaceKey:       "/data/dl/workspace",
		WorkspaceUpdatedBy: "ou_owner",
	}

	records := svc.FeishuRoomState()
	if len(records) != 1 {
		t.Fatalf("exported records = %d, want workspace-only room to remain durable: %#v", len(records), records)
	}
	if records[0].WorkspaceKey != "/data/dl/workspace" {
		t.Fatalf("durable room record does not carry workspace binding: %#v", records[0])
	}
}
