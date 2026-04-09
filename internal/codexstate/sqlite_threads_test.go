package codexstate

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteThreadCatalogRecentThreadsReturnsSortedNonArchivedRows(t *testing.T) {
	dbPath := createThreadCatalogTestDB(t)
	catalog := NewSQLiteThreadCatalog(dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})

	threads, err := catalog.RecentThreads(2)
	if err != nil {
		t.Fatalf("recent threads: %v", err)
	}
	if len(threads) != 2 {
		t.Fatalf("expected 2 threads, got %#v", threads)
	}
	if threads[0].ThreadID != "thread-3" || threads[1].ThreadID != "thread-1" {
		t.Fatalf("unexpected recent thread order: %#v", threads)
	}
	if threads[0].Preview != "第三条消息" || threads[0].ExplicitModel != "gpt-5.4" || threads[0].ExplicitReasoningEffort != "high" {
		t.Fatalf("unexpected mapped metadata: %#v", threads[0])
	}
	if !threads[0].Loaded {
		t.Fatalf("expected persisted thread to be marked loaded, got %#v", threads[0])
	}
	if want := time.Unix(1775710200, 0).UTC(); !threads[0].LastUsedAt.Equal(want) {
		t.Fatalf("unexpected updated_at mapping: got %v want %v", threads[0].LastUsedAt, want)
	}
}

func TestSQLiteThreadCatalogThreadByIDReturnsSingleMappedThread(t *testing.T) {
	dbPath := createThreadCatalogTestDB(t)
	catalog := NewSQLiteThreadCatalog(dbPath, SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})

	thread, err := catalog.ThreadByID("thread-1")
	if err != nil {
		t.Fatalf("thread by id: %v", err)
	}
	if thread == nil {
		t.Fatal("expected thread")
	}
	if thread.ThreadID != "thread-1" || thread.Name != "修复登录流程" || thread.Preview != "第一条消息" || thread.CWD != "/data/dl/droid" {
		t.Fatalf("unexpected thread mapping: %#v", thread)
	}
}

func TestNewDefaultSQLiteThreadCatalogReturnsNilWhenStateFileMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	catalog, err := NewDefaultSQLiteThreadCatalog(SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
	if err != nil {
		t.Fatalf("new default catalog: %v", err)
	}
	if catalog != nil {
		t.Fatalf("expected nil catalog when state file is missing, got %#v", catalog)
	}
}

func createThreadCatalogTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state_5.sqlite")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open test sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE threads (
	id TEXT PRIMARY KEY,
	rollout_path TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	source TEXT NOT NULL,
	model_provider TEXT NOT NULL,
	cwd TEXT NOT NULL,
	title TEXT NOT NULL,
	sandbox_policy TEXT NOT NULL,
	approval_mode TEXT NOT NULL,
	tokens_used INTEGER NOT NULL DEFAULT 0,
	has_user_event INTEGER NOT NULL DEFAULT 0,
	archived INTEGER NOT NULL DEFAULT 0,
	archived_at INTEGER,
	git_sha TEXT,
	git_branch TEXT,
	git_origin_url TEXT,
	cli_version TEXT NOT NULL DEFAULT '',
	first_user_message TEXT NOT NULL DEFAULT '',
	agent_nickname TEXT,
	agent_role TEXT,
	memory_mode TEXT NOT NULL DEFAULT 'enabled',
	model TEXT,
	reasoning_effort TEXT,
	agent_path TEXT
)`); err != nil {
		t.Fatalf("create threads table: %v", err)
	}
	insert := `
INSERT INTO threads (
	id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode,
	tokens_used, has_user_event, archived, cli_version, first_user_message, memory_mode, model, reasoning_effort
) VALUES (?, '', 0, ?, 'cli', 'openai', ?, ?, 'workspace-write', 'never', 0, 0, ?, '', ?, 'enabled', ?, ?)
`
	rows := []struct {
		id        string
		updatedAt int64
		cwd       string
		title     string
		archived  int
		preview   string
		model     string
		reasoning string
	}{
		{id: "thread-1", updatedAt: 1775710100, cwd: "/data/dl/droid", title: "修复登录流程", archived: 0, preview: "第一条消息", model: "gpt-5.4", reasoning: "xhigh"},
		{id: "thread-2", updatedAt: 1775710150, cwd: "/data/dl/archived", title: "旧会话", archived: 1, preview: "已归档", model: "gpt-5.4", reasoning: "medium"},
		{id: "thread-3", updatedAt: 1775710200, cwd: "/data/dl/web", title: "整理样式", archived: 0, preview: "第三条消息", model: "gpt-5.4", reasoning: "high"},
	}
	for _, row := range rows {
		if _, err := db.Exec(insert, row.id, row.updatedAt, row.cwd, row.title, row.archived, row.preview, row.model, row.reasoning); err != nil {
			t.Fatalf("insert thread %s: %v", row.id, err)
		}
	}
	return dbPath
}
