package statestore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type testRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func testEqual(left, right testRecord) bool {
	return left == right
}

func testKey(record testRecord) string {
	return record.ID
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store := New[testRecord](path, Options[testRecord]{
		Version: 2,
		Name:    "test",
		Equal:   testEqual,
		LoadKey: testKey,
	})
	if store.Dirty() {
		t.Fatal("new store must be clean")
	}
	if err := store.Replace(map[string]testRecord{
		"a": {ID: "a", Name: "alpha"},
		"b": {ID: "b", Name: "beta"},
	}); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if store.Dirty() {
		t.Fatal("store must be clean after save")
	}

	reloaded, err := Load[testRecord](path, Options[testRecord]{
		Version: 2,
		Name:    "test",
		Equal:   testEqual,
		LoadKey: testKey,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if reloaded.Dirty() {
		t.Fatal("round-trip store must be clean")
	}
	got, ok := reloaded.Get("a")
	if !ok || got.Name != "alpha" {
		t.Fatalf("get a = %#v, %v", got, ok)
	}
	entries := reloaded.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %#v", entries)
	}
}

func TestReplaceSkipsUnchangedWrite(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	store := New[testRecord](path, Options[testRecord]{
		Version: 1,
		Name:    "test",
		Equal:   testEqual,
		LoadKey: testKey,
	})
	initial := map[string]testRecord{"a": {ID: "a", Name: "alpha"}}
	if err := store.Replace(initial); err != nil {
		t.Fatalf("initial replace: %v", err)
	}
	statBefore, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if err := store.Replace(initial); err != nil {
		t.Fatalf("no-op replace: %v", err)
	}
	statAfter, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !statBefore.ModTime().Equal(statAfter.ModTime()) {
		t.Fatal("unchanged replace must not rewrite the file")
	}
}

func TestLoadMissingFileIsCleanEmptyStore(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "missing.json")
	store, err := Load[testRecord](path, Options[testRecord]{
		Version: 1,
		Name:    "test",
		Equal:   testEqual,
		LoadKey: testKey,
	})
	if err != nil {
		t.Fatalf("load missing: %v", err)
	}
	if store.Dirty() {
		t.Fatal("missing file must yield clean store")
	}
	if len(store.Entries()) != 0 {
		t.Fatalf("entries = %#v", store.Entries())
	}
}

func TestLoadRejectsUnsupportedVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte(`{"version":9,"entries":{}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Load[testRecord](path, Options[testRecord]{
		Version: 1,
		Name:    "test",
		Equal:   testEqual,
		LoadKey: testKey,
	}); err == nil {
		t.Fatal("expected unsupported version error")
	}
}

func TestLoadMigratesLegacyVersion(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte(`{"version":1,"entries":{"a":{"id":"a","name":"alpha"}}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store, err := Load[testRecord](path, Options[testRecord]{
		Version:        2,
		Name:           "test",
		Equal:          testEqual,
		LoadKey:        testKey,
		LegacyVersions: []int{1},
	})
	if err != nil {
		t.Fatalf("load legacy: %v", err)
	}
	if !store.Dirty() {
		t.Fatal("legacy version must mark store dirty for rewrite")
	}
	if got, ok := store.Get("a"); !ok || got.Name != "alpha" {
		t.Fatalf("get a = %#v, %v", got, ok)
	}
	if err := store.Save(); err != nil {
		t.Fatalf("save migrated: %v", err)
	}
	rawAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after save: %v", err)
	}
	var persisted StateFile[testRecord]
	if err := json.Unmarshal(rawAfter, &persisted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if persisted.Version != 2 {
		t.Fatalf("version after migration = %d, want 2", persisted.Version)
	}
}

func TestLoadNormalizeDropsInvalidAndMarksDirty(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "state.json")
	raw := []byte(`{"version":1,"entries":{"a":{"id":"a","name":"alpha"},"bad":{"id":"","name":"invalid"}}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store, err := Load[testRecord](path, Options[testRecord]{
		Version:       1,
		Name:          "test",
		Equal:         testEqual,
		LoadNormalize: func(record testRecord) (testRecord, bool) { return record, record.ID != "" },
		LoadKey:       testKey,
	})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !store.Dirty() {
		t.Fatal("dropped record must mark store dirty")
	}
	if len(store.Entries()) != 1 {
		t.Fatalf("entries = %#v, want only valid record", store.Entries())
	}
}

func TestStatePath(t *testing.T) {
	t.Parallel()

	if got := StatePath("", "x.json"); got != "" {
		t.Fatalf("StatePath('') = %q, want empty", got)
	}
	if got := StatePath("  /tmp/dir  ", "x.json"); got != filepath.Join("/tmp/dir", "x.json") {
		t.Fatalf("StatePath = %q", got)
	}
}
