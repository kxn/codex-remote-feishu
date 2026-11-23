package workstore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func makeLegacyShellInbox(t *testing.T, s *Store, orphan bool) {
	t.Helper()
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, "PRAGMA foreign_keys=ON")
	var schema string
	if err = conn.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name='inbox'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	schema = strings.Replace(schema, privateShellInboxAttemptCheck, legacyInboxAttemptCheck, 1)
	schema = strings.Replace(schema, "CREATE TABLE inbox (", "CREATE TABLE inbox_legacy_fixture (", 1)
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, statement := range []string{schema, "INSERT INTO inbox_legacy_fixture(id,revision,payload) SELECT id,revision,payload FROM inbox", "DROP TABLE inbox", "ALTER TABLE inbox_legacy_fixture RENAME TO inbox", "CREATE UNIQUE INDEX inbox_event_target ON inbox(scope_id,role_id,coalesce(context_id,''),event_id)", "CREATE INDEX inbox_ready ON inbox(role_id,context_id,state)", "CREATE TABLE inbox_cascade_fixture(input_id TEXT REFERENCES inbox(id) ON DELETE CASCADE)", "INSERT INTO inbox_cascade_fixture VALUES('input')"} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if orphan {
		if _, err = tx.ExecContext(ctx, "INSERT INTO inbox_cascade_fixture VALUES('missing-input')"); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateShellInboxMigrationPreservesAttemptForeignKeysAndCascadeRows(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error {
		if e := tx.PutInbox(testInput("input", "r", "c-r"), 0); e != nil {
			return e
		}
		return tx.ClaimAttempt(testClaim())
	})
	makeLegacyShellInbox(t, s, false)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	requireTx(t, reopened, func(tx *Tx) error {
		for _, table := range []string{"inbox", "attempt_inputs", "inbox_cascade_fixture"} {
			var n int
			if e := tx.tx.QueryRowContext(tx.ctx, "SELECT count(*) FROM "+table).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				t.Fatalf("migration lost %s rows: %d", table, n)
			}
		}
		input, e := tx.GetInbox("input")
		if e != nil {
			return e
		}
		if input.ClaimedAttemptID != "attempt" {
			t.Fatal("original claimed ownership changed")
		}
		var count int
		if e := tx.tx.QueryRowContext(tx.ctx, "SELECT count(*) FROM sqlite_master WHERE type='index' AND tbl_name='inbox' AND name IN ('inbox_event_target','inbox_ready')").Scan(&count); e != nil {
			return e
		}
		if count != 2 {
			t.Fatal("indexes not preserved")
		}
		return nil
	})
	if err := reopened.migratePrivateShellInbox(t.Context()); err != nil {
		t.Fatal("migration was not idempotent", err)
	}
}

func TestPrivateShellInboxMigrationFailureRollsBackOriginalSchemaAndRows(t *testing.T) {
	s, path := openTestStore(t)
	seedPersonal(t, s)
	requireTx(t, s, func(tx *Tx) error { return tx.PutInbox(testInput("input", "r", "c-r"), 0) })
	makeLegacyShellInbox(t, s, true)
	s.Close()
	if bad, err := Open(t.Context(), path); err == nil {
		bad.Close()
		t.Fatal("migration accepted an orphan foreign key")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var schema string
	if err = db.QueryRow("SELECT sql FROM sqlite_master WHERE name='inbox'").Scan(&schema); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(schema, legacyInboxAttemptCheck) {
		t.Fatal("failed migration changed original constraint")
	}
	var n int
	if err = db.QueryRow("SELECT count(*) FROM inbox_cascade_fixture").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatal("failed migration cascaded or lost child records")
	}
	if _, err = db.Exec("DELETE FROM inbox_cascade_fixture WHERE input_id='missing-input'"); err != nil {
		t.Fatal(err)
	}
	repaired, err := Open(t.Context(), path)
	if err != nil {
		t.Fatal("original database could not reopen after correcting fixture", err)
	}
	repaired.Close()
}
