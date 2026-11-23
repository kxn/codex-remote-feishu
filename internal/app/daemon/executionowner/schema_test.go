package executionowner

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestUnversionedForeignDatabaseIsNotAdopted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE existing_data(value TEXT); INSERT INTO existing_data VALUES('preserve')`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	registry, err := Open(path, verifier{true})
	if err == nil {
		registry.Close()
		t.Fatal("adopted an unrelated database")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version, tables int
	var value string
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 0 {
		t.Fatalf("changed foreign version: %d %v", version, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='carriers'`).Scan(&tables); err != nil || tables != 0 {
		t.Fatalf("created owner tables: %d %v", tables, err)
	}
	if err = db.QueryRow(`SELECT value FROM existing_data`).Scan(&value); err != nil || value != "preserve" {
		t.Fatalf("lost foreign data: %q %v", value, err)
	}
}
