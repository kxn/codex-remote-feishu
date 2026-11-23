// Package workstore persists collaboration facts. Transactions are deliberately
// short: callers must not invoke runtimes, HTTP, or filesystem work inside them.
package workstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

const SchemaVersion = 1

var (
	ErrConflict            = errors.New("work revision conflict")
	ErrNotFound            = errors.New("work record not found")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different input")
)

// Store owns one SQLite connection. Serial transactions avoid read-to-write
// upgrade races; SQLite additionally fences independent processes opening it.
type Store struct{ db *sql.DB }

// Tx is valid only during Within. It must not be retained or used concurrently.
type Tx struct {
	tx  *sql.Tx
	ctx context.Context
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("work store path is required")
	}
	dsn := ":memory:"
	if path != ":memory:" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(filepath.Dir(absolute), 0700); err != nil {
			return nil, fmt.Errorf("create work data directory: %w", err)
		}
		// Establish permissions before SQLite writes any private project content.
		file, err := os.OpenFile(absolute, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return nil, err
		}
		if err = file.Close(); err != nil {
			return nil, err
		}
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}
		dsn = u.String()
	}
	separator := "?"
	if strings.Contains(dsn, "?") {
		separator = "&"
	}
	dsn += separator + "_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	fail := func(err error) (*Store, error) { _ = db.Close(); return nil, err }
	if err = db.PingContext(ctx); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
		return fail(err)
	}
	if _, err = db.ExecContext(ctx, "PRAGMA synchronous=FULL"); err != nil {
		return fail(err)
	}
	store := &Store{db: db}
	if err = store.initialize(ctx); err != nil {
		return fail(err)
	}
	if err = store.migratePrivateShellInbox(ctx); err != nil {
		return fail(err)
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Within(ctx context.Context, fn func(*Tx) error) error {
	if fn == nil {
		return fmt.Errorf("work transaction callback is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = fn(&Tx{tx: tx, ctx: ctx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) initialize(ctx context.Context) error {
	return s.Within(ctx, func(t *Tx) error {
		var version int
		if err := t.tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
			return err
		}
		if version > SchemaVersion {
			return fmt.Errorf("work schema %d is newer than supported %d", version, SchemaVersion)
		}
		if version == SchemaVersion {
			for _, statement := range []string{SchemaPrivateShellOperationsV1, SchemaPrivateReactionActionsV1, SchemaPrivateReactionsV1, SchemaProfileModelCatalogV1, SchemaPrivateExecutionDisplayV1, SchemaPublicationBudgetV1, SchemaPublicationOutboxIndexV1} {
				if _, err := t.tx.ExecContext(ctx, statement); err != nil {
					return err
				}
			}
			if err := t.repairLegacyTaskInboxBindings(); err != nil {
				return err
			}
			return t.initPublicationGrants()
		}
		if version != 0 {
			return fmt.Errorf("unsupported work schema migration from %d", version)
		}
		for _, statement := range allSchemaV1() {
			if _, err := t.tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("initialize work schema: %w", err)
			}
		}
		if err := t.initPublicationGrants(); err != nil {
			return err
		}
		_, err := t.tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version=%d", SchemaVersion))
		return err
	})
}
