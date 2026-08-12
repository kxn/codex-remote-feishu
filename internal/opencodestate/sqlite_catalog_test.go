package opencodestate

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestSQLiteThreadCatalogReadsRecentOpenCodeThreadsAndWorkspaces(t *testing.T) {
	fixture := createOpenCodeCatalogTestDB(t)
	catalog := NewSQLiteThreadCatalog(fixture.dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})

	threads, err := catalog.RecentThreads(10)
	if err != nil {
		t.Fatalf("RecentThreads: %v", err)
	}
	if len(threads) != 3 {
		t.Fatalf("expected three root non-archived threads, got %#v", threads)
	}
	if threads[0].ThreadID != "ses_empty" || threads[0].WorkspaceKey != fixture.repoB {
		t.Fatalf("unexpected first thread: %#v", threads[0])
	}
	if threads[1].ThreadID != "ses_new" || threads[1].Name != "New OpenCode session" {
		t.Fatalf("unexpected second thread: %#v", threads[1])
	}
	if threads[1].CWD != fixture.repoB || threads[1].WorkspaceKey != fixture.repoB {
		t.Fatalf("unexpected workspace fields: %#v", threads[1])
	}
	if !threads[1].LastUsedAt.Equal(time.UnixMilli(1_700_000_300_000).UTC()) {
		t.Fatalf("unexpected last used time: %s", threads[1].LastUsedAt)
	}
	if threads[1].Loaded || threads[1].RuntimeStatus == nil || threads[1].RuntimeStatus.Type != agentproto.ThreadRuntimeStatusTypeNotLoaded {
		t.Fatalf("persisted opencode thread must be not-loaded, got %#v", threads[1])
	}
	if threads[1].ExplicitModel != "mimo/kimi-k2" {
		t.Fatalf("unexpected explicit model: %q", threads[1].ExplicitModel)
	}

	workspaces, err := catalog.RecentWorkspaces(10)
	if err != nil {
		t.Fatalf("RecentWorkspaces: %v", err)
	}
	if len(workspaces) != 2 {
		t.Fatalf("expected two workspaces, got %#v", workspaces)
	}
	if !workspaces[fixture.repoB].Equal(time.UnixMilli(1_700_000_600_000).UTC()) {
		t.Fatalf("unexpected repo-b recency: %#v", workspaces)
	}

	thread, err := catalog.ThreadByID("ses_old")
	if err != nil {
		t.Fatalf("ThreadByID: %v", err)
	}
	if thread == nil || thread.ThreadID != "ses_old" || thread.WorkspaceKey != fixture.repoA {
		t.Fatalf("unexpected thread by id: %#v", thread)
	}

	archived, err := catalog.ThreadByID("ses_archived")
	if err != nil {
		t.Fatalf("ThreadByID archived: %v", err)
	}
	if archived != nil {
		t.Fatalf("archived thread must be hidden, got %#v", archived)
	}

	zeroArchived, err := catalog.ThreadByID("ses_archived_zero")
	if err != nil {
		t.Fatalf("ThreadByID zero archived: %v", err)
	}
	if zeroArchived != nil {
		t.Fatalf("zero archived thread must be hidden, got %#v", zeroArchived)
	}

	relative, err := catalog.ThreadByID("ses_relative")
	if err != nil {
		t.Fatalf("ThreadByID relative: %v", err)
	}
	if relative != nil {
		t.Fatalf("relative workspace thread must be hidden, got %#v", relative)
	}
}

func TestNewDefaultSQLiteThreadCatalogUsesOpenCodeDBEnv(t *testing.T) {
	fixture := createOpenCodeCatalogTestDB(t)
	t.Setenv(OpenCodeDBEnv, fixture.dbPath)

	catalog, err := NewDefaultSQLiteThreadCatalog(SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("NewDefaultSQLiteThreadCatalog: %v", err)
	}
	if catalog == nil {
		t.Fatal("expected catalog from OPENCODE_DB")
	}
	threads, err := catalog.RecentThreads(1)
	if err != nil {
		t.Fatalf("RecentThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].ThreadID != "ses_empty" {
		t.Fatalf("unexpected threads from env catalog: %#v", threads)
	}
}

func TestDefaultSQLiteStatePathUsesRelativeOpenCodeDBUnderDataDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	t.Setenv(OpenCodeDBEnv, "custom.db")

	path, ok, err := DefaultSQLiteStatePath()
	if err != nil {
		t.Fatalf("DefaultSQLiteStatePath: %v", err)
	}
	if !ok {
		t.Fatal("expected relative OPENCODE_DB path to be usable")
	}
	want := filepath.Join(home, "xdg-data", "opencode", "custom.db")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestNewDefaultSQLiteThreadCatalogMissingOrMemoryDBIsEmpty(t *testing.T) {
	t.Setenv(OpenCodeDBEnv, filepath.Join(t.TempDir(), "missing.db"))
	missing, err := NewDefaultSQLiteThreadCatalog(SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("missing DB should not error: %v", err)
	}
	if missing != nil {
		t.Fatalf("missing DB should not create catalog, got %#v", missing)
	}

	t.Setenv(OpenCodeDBEnv, ":memory:")
	memory, err := NewDefaultSQLiteThreadCatalog(SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf(":memory: DB should not error: %v", err)
	}
	if memory != nil {
		t.Fatalf(":memory: should not create persisted catalog, got %#v", memory)
	}
}

func TestDefaultSQLiteStatePathUsesXDGDataHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "xdg-data"))
	t.Setenv(OpenCodeDBEnv, "")

	path, ok, err := DefaultSQLiteStatePath()
	if err != nil {
		t.Fatalf("DefaultSQLiteStatePath: %v", err)
	}
	if !ok {
		t.Fatal("expected default path to be usable")
	}
	want := filepath.Join(home, "xdg-data", "opencode", "opencode.db")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

type openCodeCatalogTestFixture struct {
	dbPath string
	repoA  string
	repoB  string
}

func createOpenCodeCatalogTestDB(t *testing.T) openCodeCatalogTestFixture {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	repoA := state.ResolveWorkspaceKey(t.TempDir())
	repoB := state.ResolveWorkspaceKey(t.TempDir())
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open test sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE project (
	id TEXT PRIMARY KEY,
	worktree TEXT NOT NULL,
	name TEXT,
	time_created INTEGER NOT NULL,
	time_updated INTEGER NOT NULL,
	sandboxes TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE session (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL,
	workspace_id TEXT,
	parent_id TEXT,
	slug TEXT NOT NULL,
	directory TEXT NOT NULL,
	path TEXT,
	title TEXT NOT NULL,
	version TEXT NOT NULL,
	metadata TEXT,
	model TEXT,
	time_created INTEGER NOT NULL,
	time_updated INTEGER NOT NULL,
	time_archived INTEGER
);
`); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	mustExecOpenCodeCatalogTest(t, db, `INSERT INTO project (id, worktree, name, time_created, time_updated) VALUES
('proj_a', ?, 'Repo A', 1700000000000, 1700000200000),
('proj_b', ?, 'Repo B', 1700000000000, 1700000300000)`, repoA, repoB)
	mustExecOpenCodeCatalogTest(t, db, `INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, model, time_created, time_updated, time_archived) VALUES
('ses_old', 'proj_a', NULL, 'old', ?, 'Old OpenCode session', '1.0.0', '{"providerID":"anthropic","id":"claude-sonnet-4"}', 1700000000000, 1700000200000, NULL),
('ses_new', 'proj_b', NULL, 'new', ?, 'New OpenCode session', '1.0.0', '{"providerID":"mimo","id":"kimi-k2"}', 1700000000000, 1700000300000, NULL),
('ses_child', 'proj_b', 'ses_new', 'child', ?, 'Child OpenCode session', '1.0.0', NULL, 1700000000000, 1700000400000, NULL),
('ses_archived', 'proj_b', NULL, 'archived', ?, 'Archived OpenCode session', '1.0.0', NULL, 1700000000000, 1700000500000, 1700000600000),
('ses_empty', 'proj_b', NULL, 'empty', '', 'Empty directory', '1.0.0', NULL, 1700000000000, 1700000600000, NULL),
('ses_archived_zero', 'proj_b', NULL, 'archived-zero', ?, 'Zero archived OpenCode session', '1.0.0', NULL, 1700000000000, 1700000700000, 0),
('ses_relative', 'proj_b', NULL, 'relative', 'relative/repo', 'Relative directory', '1.0.0', NULL, 1700000000000, 1700000800000, NULL)`, repoA, repoB, repoB, repoB, repoB)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("stat test DB: %v", err)
	}
	return openCodeCatalogTestFixture{dbPath: dbPath, repoA: repoA, repoB: repoB}
}

func mustExecOpenCodeCatalogTest(t *testing.T, db *sql.DB, stmt string, args ...any) {
	t.Helper()
	if _, err := db.Exec(stmt, args...); err != nil {
		t.Fatalf("exec fixture SQL: %v", err)
	}
}
