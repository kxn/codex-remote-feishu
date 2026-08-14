package opencodestate

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const (
	OpenCodeDBEnv = "OPENCODE_DB"

	defaultOpenCodeDataDir        = "opencode"
	defaultOpenCodeSQLiteFilename = "opencode.db"
	sqliteReadRetryCount          = 3
)

type SQLiteThreadCatalogOptions struct {
	Logf func(string, ...any)
}

type SQLiteThreadCatalog struct {
	path string
	logf func(string, ...any)
}

func NewDefaultSQLiteThreadCatalog(opts SQLiteThreadCatalogOptions) (*SQLiteThreadCatalog, error) {
	path, ok, err := DefaultSQLiteStatePath()
	if err != nil || !ok {
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
		return nil, fmt.Errorf("opencode sqlite path is a directory: %s", path)
	}
}

func DefaultSQLiteStatePath() (string, bool, error) {
	if raw := strings.TrimSpace(os.Getenv(OpenCodeDBEnv)); raw != "" {
		if raw == ":memory:" {
			return "", false, nil
		}
		if filepath.IsAbs(raw) {
			return filepath.Clean(raw), true, nil
		}
		dataDir, err := defaultDataDir()
		if err != nil {
			return "", false, err
		}
		return filepath.Join(dataDir, raw), true, nil
	}
	dataDir, err := defaultDataDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(dataDir, defaultOpenCodeSQLiteFilename), true, nil
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
	queryLimit := sqliteCatalogQueryLimit(limit)
	var threads []state.ThreadRecord
	err := c.readWithRetry("query recent threads", func(db *sql.DB) error {
		rows, err := db.Query(`
SELECT s.id, s.title, s.directory, p.worktree, s.time_updated, s.model
FROM session s
LEFT JOIN project p ON p.id = s.project_id
WHERE s.time_archived IS NULL
  AND TRIM(COALESCE(s.parent_id, '')) = ''
  AND COALESCE(NULLIF(TRIM(s.directory), ''), NULLIF(TRIM(p.worktree), '')) IS NOT NULL
ORDER BY s.time_updated DESC, s.id DESC
LIMIT ?
`, queryLimit)
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
			if len(local) >= limit {
				break
			}
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
	queryLimit := sqliteCatalogQueryLimit(limit)
	var workspaces map[string]time.Time
	err := c.readWithRetry("query recent workspaces", func(db *sql.DB) error {
		rows, err := db.Query(`
SELECT s.directory, p.worktree, s.time_updated
FROM session s
LEFT JOIN project p ON p.id = s.project_id
WHERE s.time_archived IS NULL
  AND TRIM(COALESCE(s.parent_id, '')) = ''
  AND COALESCE(NULLIF(TRIM(s.directory), ''), NULLIF(TRIM(p.worktree), '')) IS NOT NULL
ORDER BY s.time_updated DESC, s.id DESC
LIMIT ?
`, queryLimit)
		if err != nil {
			return err
		}
		defer rows.Close()

		local := map[string]time.Time{}
		for rows.Next() {
			var directory string
			var worktree sql.NullString
			var updatedAt int64
			if err := rows.Scan(&directory, &worktree, &updatedAt); err != nil {
				return err
			}
			workspaceKey := resolveOpenCodeWorkspacePath(directory, worktree.String)
			if workspaceKey == "" {
				continue
			}
			lastUsedAt := unixTimestamp(updatedAt)
			if current, ok := local[workspaceKey]; !ok || lastUsedAt.After(current) {
				local[workspaceKey] = lastUsedAt
			}
			if len(local) >= limit {
				break
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
SELECT s.id, s.title, s.directory, p.worktree, s.time_updated, s.model
FROM session s
LEFT JOIN project p ON p.id = s.project_id
WHERE s.id = ?
  AND s.time_archived IS NULL
  AND TRIM(COALESCE(s.parent_id, '')) = ''
  AND COALESCE(NULLIF(TRIM(s.directory), ''), NULLIF(TRIM(p.worktree), '')) IS NOT NULL
LIMIT 1
`, threadID)
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
		return nil, fmt.Errorf("missing opencode sqlite path")
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
	c.logf("opencode sqlite thread catalog %s failed: %v", strings.TrimSpace(scope), err)
}

func (c *SQLiteThreadCatalog) readWithRetry(scope string, fn func(*sql.DB) error) error {
	if c == nil {
		return fmt.Errorf("missing opencode sqlite thread catalog")
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
					c.logf("opencode sqlite thread catalog %s recovered after busy retry (%d)", scope, attempt)
				}
				return nil
			}
		}
		if !isSQLiteBusyError(lastErr) || attempt+1 >= sqliteReadRetryCount {
			return lastErr
		}
		if c.logf != nil {
			c.logf("opencode sqlite thread catalog %s busy, retrying (%d/%d): %v", scope, attempt+1, sqliteReadRetryCount-1, lastErr)
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
		threadID  string
		title     string
		directory string
		worktree  sql.NullString
		updatedAt int64
		model     sql.NullString
	)
	if err := scanner.Scan(&threadID, &title, &directory, &worktree, &updatedAt, &model); err != nil {
		return nil, err
	}
	threadID = strings.TrimSpace(threadID)
	workspaceKey := resolveOpenCodeWorkspacePath(directory, worktree.String)
	if threadID == "" || workspaceKey == "" {
		return nil, nil
	}
	title = strings.TrimSpace(title)
	return &state.ThreadRecord{
		ThreadID:      threadID,
		Name:          title,
		Preview:       title,
		WorkspaceKey:  workspaceKey,
		CWD:           workspaceKey,
		State:         string(agentproto.ThreadRuntimeStatusTypeNotLoaded),
		RuntimeStatus: &agentproto.ThreadRuntimeStatus{Type: agentproto.ThreadRuntimeStatusTypeNotLoaded},
		ExplicitModel: parseOpenCodeModel(model.String),
		Loaded:        false,
		Archived:      false,
		LastUsedAt:    unixTimestamp(updatedAt),
	}, nil
}

func resolveOpenCodeWorkspacePath(directory, worktree string) string {
	if raw := strings.TrimSpace(directory); raw != "" {
		if !openCodeWorkspacePathAllowed(raw) {
			return ""
		}
		return state.ResolveWorkspaceKey(raw)
	}
	if raw := strings.TrimSpace(worktree); raw != "" {
		if !openCodeWorkspacePathAllowed(raw) {
			return ""
		}
		return state.ResolveWorkspaceKey(raw)
	}
	return ""
}

func openCodeWorkspacePathAllowed(value string) bool {
	value = strings.TrimSpace(value)
	switch {
	case value == "":
		return false
	case filepath.IsAbs(value):
		return true
	case state.IsWindowsVolumePath(value):
		return true
	case strings.HasPrefix(value, `\\`):
		return true
	case strings.HasPrefix(value, `//`):
		return true
	default:
		return false
	}
}

func sqliteCatalogQueryLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 1000 {
		return limit
	}
	return limit * 4
}

func parseOpenCodeModel(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var model struct {
		ProviderID string `json:"providerID"`
		ID         string `json:"id"`
		Variant    string `json:"variant"`
	}
	if err := json.Unmarshal([]byte(raw), &model); err != nil {
		return ""
	}
	providerID := strings.TrimSpace(model.ProviderID)
	modelID := strings.TrimSpace(model.ID)
	variant := strings.TrimSpace(model.Variant)
	if providerID == "" || modelID == "" {
		return ""
	}
	if variant != "" {
		return providerID + "/" + modelID + "/" + variant
	}
	return providerID + "/" + modelID
}

func defaultDataDir() (string, error) {
	if dataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); dataHome != "" {
		return filepath.Join(dataHome, defaultOpenCodeDataDir), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", defaultOpenCodeDataDir), nil
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
