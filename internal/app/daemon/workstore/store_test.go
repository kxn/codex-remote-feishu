package workstore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "work.sqlite")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestStoreCommitRollbackAndReopen(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	if err := s.Within(ctx, func(tx *Tx) error {
		_, err := tx.tx.ExecContext(ctx, `INSERT INTO principals(id,payload) VALUES('kept','{}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("interrupt transaction")
	if err := s.Within(ctx, func(tx *Tx) error {
		if _, err := tx.tx.ExecContext(ctx, `INSERT INTO principals(id,payload) VALUES('discarded','{}')`); err != nil {
			return err
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("rollback error: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.Within(ctx, func(tx *Tx) error {
		var count int
		if err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM principals`).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			t.Fatalf("rollback leaked: %d", count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsFutureSchema(t *testing.T) {
	s, path := openTestStore(t)
	if _, err := s.db.Exec("PRAGMA user_version=999"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if reopened, err := Open(context.Background(), path); err == nil {
		reopened.Close()
		t.Fatal("accepted unsupported schema")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 999 {
		t.Fatalf("overwrote schema: %d", version)
	}
}

func TestStoreEnforcesScopeOwnerAndForeignKeys(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	cases := []struct{ name, payload string }{
		{"missing owner", `{"kind":"personal","principal_id":"missing"}`},
		{"mixed ownership", `{"kind":"personal","principal_id":"person","project_id":"project"}`},
		{"unknown kind", `{"kind":"unknown","principal_id":"person"}`},
	}
	if err := s.Within(ctx, func(tx *Tx) error {
		_, err := tx.tx.ExecContext(ctx, `INSERT INTO principals(id,payload) VALUES('person','{}')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := s.Within(ctx, func(tx *Tx) error {
				_, err := tx.tx.ExecContext(ctx, `INSERT INTO scopes(id,revision,payload) VALUES('bad',1,?)`, tc.payload)
				return err
			})
			if err == nil {
				t.Fatal("accepted invalid scope")
			}
		})
	}
}

func TestStoreCancellationDoesNotCommit(t *testing.T) {
	s, _ := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Within(ctx, func(tx *Tx) error { t.Fatal("called transaction after cancellation"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
