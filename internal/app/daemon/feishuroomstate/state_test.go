package feishuroomstate

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := LoadStore(StatePath(stateDir))
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}
	updatedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	if err := store.Put(state.FeishuRoomStateRecord{
		RoomID:                   " feishu:chat:oc_room ",
		ChatID:                   " oc_room ",
		WorkspaceKey:             " /data/projects/alpha/../workspace ",
		WorkspaceUpdatedBy:       " ou_workspace ",
		WorkspaceUpdatedAt:       updatedAt,
		WorkspaceResetGeneration: 2,
		PrimaryGatewayID:         " app-1 ",
		PrimaryUpdatedBy:         " ou_user ",
		PrimaryUpdatedAt:         updatedAt,
		ConcurrencyLimit:         intPointer(0),
	}); err != nil {
		t.Fatalf("put record: %v", err)
	}

	reloaded, err := LoadStore(StatePath(stateDir))
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	got, ok := reloaded.Get(state.FeishuRoomKey("oc_room"))
	if !ok {
		t.Fatal("expected record after reload")
	}
	if got.RoomID != "feishu:chat:oc_room" || got.ChatID != "oc_room" || got.PrimaryGatewayID != "app-1" {
		t.Fatalf("record identity = %#v, want normalized room/chat/gateway", got)
	}
	if got.PrimaryUpdatedBy != "ou_user" || !got.PrimaryUpdatedAt.Equal(updatedAt.UTC()) {
		t.Fatalf("metadata = %#v, want UTC-normalized update metadata", got)
	}
	if got.WorkspaceKey != "/data/projects/workspace" || got.WorkspaceUpdatedBy != "ou_workspace" || !got.WorkspaceUpdatedAt.Equal(updatedAt.UTC()) || got.WorkspaceResetGeneration != 2 {
		t.Fatalf("workspace state = %#v, want normalized durable workspace metadata", got)
	}
	if got.ConcurrencyLimit == nil || *got.ConcurrencyLimit != 0 {
		t.Fatalf("concurrency limit = %#v, want explicit unlimited", got.ConcurrencyLimit)
	}
}

func TestLoadStoreMigratesPrimaryOnlyVersionOneInPlace(t *testing.T) {
	t.Parallel()

	path := StatePath(t.TempDir())
	legacy := []byte(`{"version":1,"entries":{"feishu:chat:oc_room":{"RoomID":"feishu:chat:oc_room","ChatID":"oc_room","PrimaryGatewayID":"app-1"}}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("load legacy state: %v", err)
	}
	if !store.Dirty() {
		t.Fatal("legacy primary-only schema must be rewritten exactly once")
	}
	if record, ok := store.Get("oc_room"); !ok || record.PrimaryGatewayID != "app-1" || record.WorkspaceKey != "" {
		t.Fatalf("migrated legacy record = %#v, present=%v", record, ok)
	}
	if record, _ := store.Get("oc_room"); record.ConcurrencyLimit != nil || state.FeishuRoomConcurrencyLimit(record.ConcurrencyLimit) != 1 {
		t.Fatalf("legacy concurrency limit = %#v, want unset with runtime default 1", record.ConcurrencyLimit)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save migrated state: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migrated state: %v", err)
	}
	var persisted StateFile
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatalf("decode migrated state: %v", err)
	}
	if persisted.Version != 2 {
		t.Fatalf("migrated version = %d, want 2", persisted.Version)
	}
}

func intPointer(value int) *int { return &value }

func TestLoadStoreDropsInvalidRecords(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := LoadStore(StatePath(stateDir))
	if err != nil {
		t.Fatalf("load empty store: %v", err)
	}
	if err := store.Put(state.FeishuRoomStateRecord{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		PrimaryGatewayID: "app-1",
	}); err != nil {
		t.Fatalf("put valid record: %v", err)
	}
	if err := store.Put(state.FeishuRoomStateRecord{}); err == nil {
		t.Fatal("expected empty room state record to be rejected")
	}
}
