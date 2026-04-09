package codexstate

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	defaultCodexStateDir       = ".codex"
	defaultCodexSQLiteFilename = "state_5.sqlite"
)

type SQLiteThreadCatalogOptions struct {
	Logf func(string, ...any)
}

type SQLiteThreadCatalog struct {
	path string
	logf func(string, ...any)
}

func NewDefaultSQLiteThreadCatalog(opts SQLiteThreadCatalogOptions) (*SQLiteThreadCatalog, error) {
	path, err := DefaultSQLiteStatePath()
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	switch {
	case err == nil && !info.IsDir():
		return NewSQLiteThreadCatalog(path, opts), nil
	case errors.Is(err, os.ErrNotExist):
		return nil, nil
	case err != nil:
		return nil, err
	default:
		return nil, fmt.Errorf("codex sqlite path is a directory: %s", path)
	}
}

func DefaultSQLiteStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultCodexStateDir, defaultCodexSQLiteFilename), nil
}

func NewSQLiteThreadCatalog(path string, opts SQLiteThreadCatalogOptions) *SQLiteThreadCatalog {
	logf := opts.Logf
	if logf == nil {
		logf = log.Printf
	}
	return &SQLiteThreadCatalog{
		path: strings.TrimSpace(path),
		logf: logf,
	}
}

func (c *SQLiteThreadCatalog) RecentThreads(limit int) ([]state.ThreadRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	db, err := c.openReadOnly()
	if err != nil {
		c.logError("open recent threads", err)
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query(`
SELECT id, title, cwd, updated_at, archived, model, reasoning_effort, first_user_message
FROM threads
WHERE archived = 0
ORDER BY updated_at DESC, id DESC
LIMIT ?
`, limit)
	if err != nil {
		c.logError("query recent threads", err)
		return nil, err
	}
	defer rows.Close()

	threads := make([]state.ThreadRecord, 0, limit)
	for rows.Next() {
		thread, err := scanPersistedThread(rows)
		if err != nil {
			c.logError("scan recent thread", err)
			return nil, err
		}
		if thread == nil {
			continue
		}
		threads = append(threads, *thread)
	}
	if err := rows.Err(); err != nil {
		c.logError("iterate recent threads", err)
		return nil, err
	}
	return threads, nil
}

func (c *SQLiteThreadCatalog) ThreadByID(threadID string) (*state.ThreadRecord, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, nil
	}
	db, err := c.openReadOnly()
	if err != nil {
		c.logError("open thread by id", err)
		return nil, err
	}
	defer db.Close()

	row := db.QueryRow(`
SELECT id, title, cwd, updated_at, archived, model, reasoning_effort, first_user_message
FROM threads
WHERE id = ? AND archived = 0
LIMIT 1
`, threadID)
	thread, err := scanPersistedThread(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		c.logError("query thread by id", err)
		return nil, err
	default:
		return thread, nil
	}
}

func (c *SQLiteThreadCatalog) openReadOnly() (*sql.DB, error) {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil, fmt.Errorf("missing codex sqlite path")
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     c.path,
		RawQuery: url.Values{"mode": {"ro"}}.Encode(),
	}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}

func (c *SQLiteThreadCatalog) logError(scope string, err error) {
	if c == nil || c.logf == nil || err == nil {
		return
	}
	c.logf("codex sqlite thread catalog %s failed: %v", strings.TrimSpace(scope), err)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPersistedThread(scanner rowScanner) (*state.ThreadRecord, error) {
	var (
		threadID         string
		title            string
		cwd              string
		updatedAt        int64
		archived         int64
		model            sql.NullString
		reasoningEffort  sql.NullString
		firstUserMessage sql.NullString
	)
	if err := scanner.Scan(
		&threadID,
		&title,
		&cwd,
		&updatedAt,
		&archived,
		&model,
		&reasoningEffort,
		&firstUserMessage,
	); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || archived != 0 {
		return nil, nil
	}
	preview := strings.TrimSpace(firstUserMessage.String)
	title = strings.TrimSpace(title)
	if preview == "" {
		preview = title
	}
	if title == "" {
		title = preview
	}
	return &state.ThreadRecord{
		ThreadID:                threadID,
		Name:                    title,
		Preview:                 preview,
		CWD:                     strings.TrimSpace(cwd),
		ExplicitModel:           strings.TrimSpace(model.String),
		ExplicitReasoningEffort: strings.TrimSpace(reasoningEffort.String),
		Loaded:                  true,
		LastUsedAt:              unixTimestamp(updatedAt),
	}, nil
}

func unixTimestamp(value int64) time.Time {
	switch {
	case value <= 0:
		return time.Time{}
	case value >= 1_000_000_000_000:
		return time.UnixMilli(value).UTC()
	default:
		return time.Unix(value, 0).UTC()
	}
}
