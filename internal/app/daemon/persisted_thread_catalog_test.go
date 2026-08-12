package daemon

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/opencodestate"
)

func TestDaemonPersistedThreadCatalogDoesNotExposeCodexRowsToOpenCode(t *testing.T) {
	dbPath := createDaemonCodexThreadCatalogTestDB(t)
	catalog := &daemonPersistedThreadCatalog{
		codex: codexstate.NewSQLiteThreadCatalog(dbPath, codexstate.SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}}),
	}

	codexThreads, err := catalog.RecentThreadsForBackend(agentproto.BackendCodex, 10)
	if err != nil {
		t.Fatalf("codex recent threads: %v", err)
	}
	if len(codexThreads) != 1 || codexThreads[0].ThreadID != "codex-thread-1" {
		t.Fatalf("expected codex catalog to contain fixture thread, got %#v", codexThreads)
	}

	opencodeThreads, err := catalog.RecentThreadsForBackend(agentproto.BackendOpenCode, 10)
	if err != nil {
		t.Fatalf("opencode recent threads: %v", err)
	}
	if len(opencodeThreads) != 0 {
		t.Fatalf("opencode must not read codex persisted threads, got %#v", opencodeThreads)
	}
	opencodeWorkspaces, err := catalog.RecentWorkspacesForBackend(agentproto.BackendOpenCode, 10)
	if err != nil {
		t.Fatalf("opencode recent workspaces: %v", err)
	}
	if len(opencodeWorkspaces) != 0 {
		t.Fatalf("opencode must not read codex persisted workspaces, got %#v", opencodeWorkspaces)
	}
	thread, err := catalog.ThreadByIDForBackend(agentproto.BackendOpenCode, "codex-thread-1")
	if err != nil {
		t.Fatalf("opencode thread by id: %v", err)
	}
	if thread != nil {
		t.Fatalf("opencode must not resolve codex persisted thread by id, got %#v", thread)
	}
}

func TestDaemonPersistedThreadCatalogExposesOpenCodeRowsOnlyToOpenCode(t *testing.T) {
	dbPath := createDaemonOpenCodeThreadCatalogTestDB(t)
	catalog := &daemonPersistedThreadCatalog{
		opencode: opencodestate.NewSQLiteThreadCatalog(dbPath, opencodestate.SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}}),
	}

	opencodeThreads, err := catalog.RecentThreadsForBackend(agentproto.BackendOpenCode, 10)
	if err != nil {
		t.Fatalf("opencode recent threads: %v", err)
	}
	if len(opencodeThreads) != 1 || opencodeThreads[0].ThreadID != "opencode-session-1" {
		t.Fatalf("expected opencode catalog to contain fixture session, got %#v", opencodeThreads)
	}

	codexThreads, err := catalog.RecentThreadsForBackend(agentproto.BackendCodex, 10)
	if err != nil {
		t.Fatalf("codex recent threads: %v", err)
	}
	if len(codexThreads) != 0 {
		t.Fatalf("codex must not read opencode persisted threads, got %#v", codexThreads)
	}

	opencodeWorkspaces, err := catalog.RecentWorkspacesForBackend(agentproto.BackendOpenCode, 10)
	if err != nil {
		t.Fatalf("opencode recent workspaces: %v", err)
	}
	if len(opencodeWorkspaces) != 1 {
		t.Fatalf("expected opencode workspaces, got %#v", opencodeWorkspaces)
	}
	thread, err := catalog.ThreadByIDForBackend(agentproto.BackendOpenCode, "opencode-session-1")
	if err != nil {
		t.Fatalf("opencode thread by id: %v", err)
	}
	if thread == nil || thread.ThreadID != "opencode-session-1" {
		t.Fatalf("expected opencode thread by id, got %#v", thread)
	}
}

func createDaemonCodexThreadCatalogTestDB(t *testing.T) string {
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
	workspace := filepath.Join(t.TempDir(), "repo")
	if _, err := db.Exec(`
INSERT INTO threads (
	id, rollout_path, created_at, updated_at, source, model_provider, cwd, title, sandbox_policy, approval_mode,
	tokens_used, has_user_event, archived, cli_version, first_user_message, memory_mode, model, reasoning_effort, agent_role
) VALUES (?, ?, 0, 1775710100, 'cli', 'openai', ?, 'Codex fixture', 'workspace-write', 'never', 0, 0, 0, '', 'fixture preview', 'enabled', 'gpt-5.5', 'high', '')
`, "codex-thread-1", filepath.Join(workspace, "codex-thread-1.jsonl"), workspace); err != nil {
		t.Fatalf("insert thread: %v", err)
	}
	return dbPath
}

func createDaemonOpenCodeThreadCatalogTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
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
INSERT INTO project (id, worktree, name, time_created, time_updated)
VALUES ('proj-opencode', '/data/dl/opencode', 'OpenCode', 1700000000000, 1700000000000);
INSERT INTO session (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated, time_archived)
VALUES ('opencode-session-1', 'proj-opencode', NULL, 'one', '/data/dl/opencode', 'OpenCode session', '1.0.0', 1700000000000, 1700000000000, NULL);
`); err != nil {
		t.Fatalf("create opencode schema: %v", err)
	}
	return dbPath
}
