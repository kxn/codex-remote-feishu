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
	internalProbeThreadPrefix  = "_tmp-codex-thread-latency-"
	internalProbeAppPrefix     = "_tmp-codex-appserver-"
	cronRepoRunPathPattern     = "%/cron-repos/runs/%"
	sqliteReadRetryCount       = 3
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
	var threads []state.ThreadRecord
	err := c.readWithRetry("query recent threads", func(db *sql.DB) error {
		rows, err := db.Query(`
SELECT id, title, cwd, updated_at, archived, model, reasoning_effort, first_user_message
FROM threads
WHERE archived = 0
  AND source IN ('cli', 'vscode')
  AND COALESCE(agent_role, '') = ''
  AND cwd NOT LIKE '%/_tmp-codex-thread-latency-%'
  AND cwd NOT LIKE '%/_tmp-codex-appserver-%'
  AND cwd NOT LIKE ?
ORDER BY updated_at DESC, id DESC
LIMIT ?
`, cronRepoRunPathPattern, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		local := make([]state.ThreadRecord, 0, limit)
		for rows.Next() {
			thread, err := scanPersistedThread(rows)
			if err != nil {
				return err
			}
			if thread == nil {
				continue
			}
			local = append(local, *thread)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		threads = local
		return nil
	})
	if err != nil {
		c.logError("query recent threads", err)
		return nil, err
	}
	return threads, nil
}

func (c *SQLiteThreadCatalog) RecentWorkspaces(limit int) (map[string]time.Time, error) {
	if limit <= 0 {
		limit = 200
	}
	var workspaces map[string]time.Time
	err := c.readWithRetry("query recent workspaces", func(db *sql.DB) error {
		rows, err := db.Query(`
SELECT cwd, MAX(updated_at) AS updated_at
FROM threads
WHERE archived = 0
  AND source IN ('cli', 'vscode')
  AND COALESCE(agent_role, '') = ''
  AND cwd NOT LIKE '%/_tmp-codex-thread-latency-%'
  AND cwd NOT LIKE '%/_tmp-codex-appserver-%'
  AND cwd NOT LIKE ?
GROUP BY cwd
ORDER BY updated_at DESC, cwd ASC
LIMIT ?
`, cronRepoRunPathPattern, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		local := map[string]time.Time{}
		for rows.Next() {
			var cwd string
			var updatedAt int64
			if err := rows.Scan(&cwd, &updatedAt); err != nil {
				return err
			}
			cwd = state.ResolveWorkspaceKey(cwd)
			if cwd == "" || internalProbeWorkspace(cwd) {
				continue
			}
			lastUsedAt := unixTimestamp(updatedAt)
			if current, ok := local[cwd]; !ok || lastUsedAt.After(current) {
				local[cwd] = lastUsedAt
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		workspaces = local
		return nil
	})
	if err != nil {
		c.logError("query recent workspaces", err)
		return nil, err
	}
	return workspaces, nil
}

func (c *SQLiteThreadCatalog) ThreadByID(threadID string) (*state.ThreadRecord, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil, nil
	}
	var thread *state.ThreadRecord
	err := c.readWithRetry("query thread by id", func(db *sql.DB) error {
		row := db.QueryRow(`
SELECT id, title, cwd, updated_at, archived, model, reasoning_effort, first_user_message
FROM threads
WHERE id = ?
  AND archived = 0
  AND source IN ('cli', 'vscode')
  AND COALESCE(agent_role, '') = ''
  AND cwd NOT LIKE '%/_tmp-codex-thread-latency-%'
  AND cwd NOT LIKE '%/_tmp-codex-appserver-%'
  AND cwd NOT LIKE ?
LIMIT 1
`, threadID, cronRepoRunPathPattern)
		record, err := scanPersistedThread(row)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			thread = nil
			return nil
		case err != nil:
			return err
		default:
			thread = record
			return nil
		}
	})
	if err != nil {
		c.logError("query thread by id", err)
		return nil, err
	}
	return thread, nil
}

func (c *SQLiteThreadCatalog) openReadOnly() (*sql.DB, error) {
	if c == nil || strings.TrimSpace(c.path) == "" {
		return nil, fmt.Errorf("missing codex sqlite path")
	}
	path := filepath.Clean(strings.TrimSpace(c.path))
	path = filepath.ToSlash(path)
	if vol := filepath.VolumeName(filepath.Clean(strings.TrimSpace(c.path))); vol != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     path,
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

func (c *SQLiteThreadCatalog) readWithRetry(scope string, fn func(*sql.DB) error) error {
	if c == nil {
		return fmt.Errorf("missing codex sqlite thread catalog")
	}
	scope = strings.TrimSpace(scope)
	var lastErr error
	for attempt := 0; attempt < sqliteReadRetryCount; attempt++ {
		db, err := c.openReadOnly()
		if err != nil {
			lastErr = err
		} else {
			runErr := fn(db)
			closeErr := db.Close()
			if runErr != nil {
				lastErr = runErr
			} else if closeErr != nil {
				lastErr = closeErr
			} else {
				if attempt > 0 && c.logf != nil {
					c.logf("codex sqlite thread catalog %s recovered after busy retry (%d)", scope, attempt)
				}
				return nil
			}
		}
		if !isSQLiteBusyError(lastErr) || attempt+1 >= sqliteReadRetryCount {
			return lastErr
		}
		if c.logf != nil {
			c.logf("codex sqlite thread catalog %s busy, retrying (%d/%d): %v", scope, attempt+1, sqliteReadRetryCount-1, lastErr)
		}
		time.Sleep(sqliteReadRetryBackoff(attempt))
	}
	return lastErr
}

func sqliteReadRetryBackoff(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 15 * time.Millisecond
	case 1:
		return 35 * time.Millisecond
	default:
		return 60 * time.Millisecond
	}
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked") ||
		strings.Contains(msg, "sqlite_busy")
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
	cwd = strings.TrimSpace(cwd)
	if internalProbeWorkspace(cwd) {
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
		CWD:                     cwd,
		ExplicitModel:           strings.TrimSpace(model.String),
		ExplicitReasoningEffort: strings.TrimSpace(reasoningEffort.String),
		Loaded:                  true,
		LastUsedAt:              unixTimestamp(updatedAt),
	}, nil
}

func internalProbeWorkspace(cwd string) bool {
	base := filepath.Base(filepath.Clean(strings.TrimSpace(cwd)))
	switch {
	case strings.HasPrefix(base, internalProbeThreadPrefix):
		return true
	case strings.HasPrefix(base, internalProbeAppPrefix):
		return true
	default:
		return false
	}
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
