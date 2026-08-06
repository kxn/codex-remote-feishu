package feishufacts

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStorePutGetAndReloadPersistsRecord(t *testing.T) {
	stateDir := t.TempDir()
	path := StatePath(stateDir)
	store := NewStore(path)

	now := time.Date(2026, 8, 6, 4, 0, 0, 0, time.UTC)
	record := Record{
		GatewayID: "main",
		AppID:     "cli_test",
		AppName:   "Codex Bot",
		BotOpenID: "ou_bot",
		Scopes: []ScopeStatus{
			{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1},
		},
		FetchedAt: now,
	}
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}

	reloaded, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	got, ok := reloaded.Get("main")
	if !ok {
		t.Fatal("expected record to survive reload")
	}
	if got.AppID != "cli_test" || got.AppName != "Codex Bot" || got.BotOpenID != "ou_bot" {
		t.Fatalf("reloaded identity fields = %#v", got)
	}
	if len(got.Scopes) != 1 || got.Scopes[0].ScopeName != "im:message.group_msg" {
		t.Fatalf("reloaded scopes = %#v", got.Scopes)
	}
	if !got.FetchedAt.Equal(now) {
		t.Fatalf("reloaded fetchedAt = %s, want %s", got.FetchedAt, now)
	}
}

func TestLoadStoreDropsInvalidRecordsAndKeepsValidOnes(t *testing.T) {
	stateDir := t.TempDir()
	path := StatePath(stateDir)
	raw := `{
		"version": 1,
		"entries": {
			"invalid": {"gatewayID": "invalid", "appID": ""},
			"valid": {"gatewayID": "valid", "appID": "cli_valid", "appName": "Valid Bot"}
		}
	}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	store, err := LoadStore(path)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	if _, ok := store.Get("invalid"); ok {
		t.Fatal("invalid record should be dropped")
	}
	got, ok := store.Get("valid")
	if !ok || got.AppName != "Valid Bot" {
		t.Fatalf("valid record = %#v, ok=%v", got, ok)
	}
}

func TestStoreDeleteRemovesRecord(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "facts.json"))
	if err := store.Put(Record{GatewayID: "main", AppID: "cli_test"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Delete("main"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.Get("main"); ok {
		t.Fatal("record should be gone after Delete")
	}
}

func TestPutRejectsMissingGatewayOrAppID(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "facts.json"))
	if err := store.Put(Record{AppID: "cli_test"}); err == nil {
		t.Fatal("expected missing gateway id to fail")
	}
	if err := store.Put(Record{GatewayID: "main"}); err == nil {
		t.Fatal("expected missing app id to fail")
	}
}

func TestGetReturnsDeepCopyOfScopes(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "facts.json"))
	record := Record{
		GatewayID: "main",
		AppID:     "cli_test",
		Scopes:    []ScopeStatus{{ScopeName: "im:message.group_msg", ScopeType: "tenant", GrantStatus: 1}},
	}
	if err := store.Put(record); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, ok := store.Get("main")
	if !ok {
		t.Fatal("expected record")
	}
	got.Scopes[0].GrantStatus = 0
	again, _ := store.Get("main")
	if again.Scopes[0].GrantStatus != 1 {
		t.Fatal("Get returned a mutable scope slice")
	}
}
