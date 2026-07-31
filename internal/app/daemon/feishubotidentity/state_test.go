package feishubotidentity

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), StateFileName)
	store := NewStore(path)
	updatedAt := time.Date(2026, time.July, 31, 10, 20, 30, 0, time.FixedZone("test", 8*60*60))
	if err := store.Put(Record{
		GatewayID:  " main ",
		AppID:      " cli_xxx ",
		Generation: 2,
		UpdatedAt:  updatedAt,
		Pending: &PendingTransition{
			DesiredAppID: " cli_next ",
			StartedAt:    updatedAt.Add(time.Minute),
		},
	}); err != nil {
		t.Fatalf("put identity: %v", err)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("reload identity store: %v", err)
	}
	record, ok := reloaded.Get("main")
	if !ok || record.AppID != "cli_xxx" || record.Generation != 2 || !record.UpdatedAt.Equal(updatedAt.UTC()) {
		t.Fatalf("round-trip identity = %#v, present=%v", record, ok)
	}
	if record.Pending == nil || record.Pending.DesiredAppID != "cli_next" || !record.Pending.StartedAt.Equal(updatedAt.Add(time.Minute).UTC()) {
		t.Fatalf("round-trip pending transition = %#v", record.Pending)
	}
	record.Pending.DesiredAppID = "mutated"
	entries := reloaded.Entries()
	entries["main"].Pending.DesiredAppID = "mutated-again"
	record, ok = reloaded.Get("main")
	if !ok || record.Pending == nil || record.Pending.DesiredAppID != "cli_next" {
		t.Fatalf("store returned aliased pending transition: %#v, present=%v", record, ok)
	}
	if err := reloaded.Delete("main"); err != nil {
		t.Fatalf("delete identity: %v", err)
	}
	reloaded, err = LoadStore(path)
	if err != nil {
		t.Fatalf("reload deleted identity store: %v", err)
	}
	if _, ok := reloaded.Get("main"); ok {
		t.Fatal("deleted identity survived reload")
	}
}

func TestStoreFailedWriteRollsBackMemory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, StateFileName)
	store := NewStore(path)
	if err := store.Put(Record{GatewayID: "main", AppID: "cli_old", Generation: 1}); err != nil {
		t.Fatalf("put initial identity: %v", err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("remove store root: %v", err)
	}
	if err := os.WriteFile(root, []byte("blocks writes"), 0o600); err != nil {
		t.Fatalf("block store root: %v", err)
	}

	err := store.Put(Record{GatewayID: "main", AppID: "cli_new", Generation: 2})
	if err == nil {
		t.Fatal("expected blocked identity write to fail")
	}
	record, ok := store.Get("main")
	if !ok || record.AppID != "cli_old" || record.Generation != 1 {
		t.Fatalf("failed write changed in-memory identity: %#v, present=%v", record, ok)
	}
}
