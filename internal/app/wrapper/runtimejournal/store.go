// Package runtimejournal records transport evidence. Native writes and SQLite
// commits are not atomic; interrupted writes require reconciliation, not replay.
package runtimejournal

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
	_ "modernc.org/sqlite"
)

var ErrConflict = errors.New("runtime journal identity or payload conflict")

const (
	StagePrepared         = "prepared"
	StageWriteStarted     = "child_write_started"
	StageWritten          = "written"
	StagePartiallyWritten = "partially_written"
	StageUnknown          = "unknown"
)

type Store struct {
	db                *sql.DB
	lock              *relayruntime.FileLock
	incarnation       string
	workspaceRoot     string
	displayMu         sync.Mutex
	display           map[displayTurnKey]*displayEvidence
	privateLive       bool
	liveNext, liveEnd uint64
}

// Open takes an exclusive carrier lock. Reopening conservatively marks every
// command with an unfinished or possibly effective write as unknown.
func Open(ctx context.Context, path, stateDir, cwd, incarnation string) (*Store, error) {
	if strings.TrimSpace(incarnation) == "" {
		return nil, fmt.Errorf("carrier incarnation is required")
	}
	path, err := ValidatePath(path, stateDir, cwd)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	path, err = ValidatePath(path, stateDir, cwd)
	if err != nil {
		return nil, err
	}
	lock, err := relayruntime.AcquireLock(ctx, path+".lock", false)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			_ = lock.Release()
		}
	}()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	info, statErr := file.Stat()
	_ = file.Close()
	if statErr != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
		return nil, fmt.Errorf("journal requires an owner-only regular file")
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, lock: lock, incarnation: incarnation, workspaceRoot: cwd}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	failed = false
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("runtime journal schema requires a newer wrapper")
	}
	for _, statement := range []string{
		`PRAGMA journal_mode=WAL`, `PRAGMA synchronous=FULL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS streams (incarnation TEXT PRIMARY KEY, sequence INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS private_live_streams (incarnation TEXT PRIMARY KEY, reserved_through INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE IF NOT EXISTS commands (id TEXT PRIMARY KEY, hash TEXT NOT NULL, incarnation TEXT NOT NULL, binding BLOB NOT NULL, payload BLOB NOT NULL, stage TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS frames (id INTEGER PRIMARY KEY AUTOINCREMENT, command_id TEXT NOT NULL REFERENCES commands(id), hash TEXT NOT NULL, native_id TEXT NOT NULL, native_method TEXT NOT NULL, stage TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY AUTOINCREMENT, incarnation TEXT NOT NULL, sequence INTEGER NOT NULL, hash TEXT NOT NULL, payload BLOB NOT NULL, acked INTEGER NOT NULL DEFAULT 0, UNIQUE(incarnation,sequence))`,
		`CREATE INDEX IF NOT EXISTS pending_events ON events(acked,id)`,
		`CREATE TABLE IF NOT EXISTS inventory_state (incarnation TEXT PRIMARY KEY,payload BLOB NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS launch_contracts (incarnation TEXT PRIMARY KEY,payload_hash TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS private_query_links (waiter_id TEXT PRIMARY KEY, incarnation TEXT NOT NULL, source_id TEXT NOT NULL, source_hash TEXT NOT NULL, request_id TEXT NOT NULL)`,
		`PRAGMA user_version=1`,
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO streams(incarnation) VALUES(?)`, s.incarnation); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE commands SET stage=? WHERE id IN (SELECT command_id FROM frames WHERE stage IN ('written','child_write_started','unknown'))`, StageUnknown); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE frames SET stage=? WHERE stage=?`, StageUnknown, StageWriteStarted); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	err := s.db.Close()
	lockErr := s.lock.Release()
	if err != nil {
		return err
	}
	return lockErr
}

func hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// ValidatePath resolves existing ancestors so symlinked parent directories
// cannot move the journal outside the host's controlled state directory.
func ValidatePath(path, stateDir, cwd string) (string, error) {
	if !filepath.IsAbs(path) || !filepath.IsAbs(stateDir) {
		return "", fmt.Errorf("journal and host state directory require absolute paths")
	}
	root, err := resolvePath(stateDir)
	if err != nil {
		return "", err
	}
	target, err := resolvePath(path)
	if err != nil {
		return "", err
	}
	workspace, err := resolvePath(cwd)
	if err != nil {
		return "", err
	}
	if !within(root, target) || target == root || within(workspace, target) {
		return "", fmt.Errorf("journal must be inside host state directory and outside execution cwd")
	}
	return target, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func resolvePath(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("absolute path required")
	}
	path = filepath.Clean(path)
	var suffix []string
	for {
		_, err := os.Lstat(path)
		if err == nil {
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", err
		}
		suffix = append(suffix, filepath.Base(path))
		path = parent
	}
}

func (s *Store) Binding(commandID string) (*agentproto.ExecutionBinding, error) {
	var raw []byte
	if err := s.db.QueryRow(`SELECT binding FROM commands WHERE id=?`, commandID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var binding agentproto.ExecutionBinding
	if err := json.Unmarshal(raw, &binding); err != nil {
		return nil, err
	}
	return &binding, nil
}
