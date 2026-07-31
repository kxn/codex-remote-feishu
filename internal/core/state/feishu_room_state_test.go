package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNormalizeFeishuRoomStateRecord(t *testing.T) {
	updatedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	record, ok := NormalizeFeishuRoomStateRecord(FeishuRoomStateRecord{
		RoomID:           " feishu:chat:oc_room ",
		ChatID:           " oc_room ",
		PrimaryGatewayID: " app-1 ",
		PrimaryUpdatedBy: " ou_user ",
		PrimaryUpdatedAt: updatedAt,
	})
	if !ok {
		t.Fatal("expected valid room primary record")
	}
	if record.RoomID != "feishu:chat:oc_room" || record.ChatID != "oc_room" || record.PrimaryGatewayID != "app-1" {
		t.Fatalf("record identity = %#v, want normalized room/chat/gateway", record)
	}
	if record.PrimaryUpdatedBy != "ou_user" || !record.PrimaryUpdatedAt.Equal(updatedAt.UTC()) {
		t.Fatalf("metadata = %#v, want UTC-normalized update metadata", record)
	}
}

func TestNormalizeFeishuRoomStateRecordResolvesWorkspaceClaimKey(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	record, ok := NormalizeFeishuRoomStateRecord(FeishuRoomStateRecord{
		RoomID:       "feishu:chat:oc_room",
		ChatID:       "oc_room",
		WorkspaceKey: filepath.Join(link, "."),
	})
	if !ok {
		t.Fatal("expected valid room workspace record")
	}
	if want := ResolveWorkspaceClaimKey(link); record.WorkspaceKey != want {
		t.Fatalf("workspace key = %q, want claim key %q", record.WorkspaceKey, want)
	}
}

func TestFeishuRoomStateRecordRequiresRoomIdentity(t *testing.T) {
	if key := FeishuRoomKey(" oc_room "); key != "feishu:chat:oc_room" {
		t.Fatalf("FeishuRoomKey = %q, want feishu:chat:oc_room", key)
	}
	if _, ok := NormalizeFeishuRoomStateRecord(FeishuRoomStateRecord{}); ok {
		t.Fatal("expected empty room state record to be rejected")
	}
}

func TestFeishuRoomStateRecordFromContextExcludesRuntimeEvidence(t *testing.T) {
	updatedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	room := &FeishuRoomContextRecord{
		RoomID:             "feishu:chat:oc_room",
		ChatID:             "oc_room",
		PrimaryGatewayID:   "app-1",
		PrimaryUpdatedBy:   "ou_user",
		PrimaryUpdatedAt:   updatedAt,
		WorkspaceKey:       "/data/dl/repo",
		ActiveLock:         &FeishuRoomActiveLockRecord{SurfaceSessionID: "surface-1"},
		GatewayIDs:         map[string]bool{"app-1": true, "app-2": true},
		SurfaceSessionIDs:  map[string]bool{"surface-1": true},
		WorkspaceUpdatedBy: "ou_workspace",
	}
	record, ok := FeishuRoomStateRecordFromContext(room)
	if !ok {
		t.Fatal("expected room primary record from context")
	}
	if record.RoomID != "feishu:chat:oc_room" || record.ChatID != "oc_room" || record.PrimaryGatewayID != "app-1" {
		t.Fatalf("durable primary record = %#v", record)
	}
}
