package orchestrator

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestPersistedWorkspaceSQLiteCWDDoesNotReplaceExplicitRoot(t *testing.T) {
	for _, source := range []string{"runtime", "restored-room", "cwd-only"} {
		for _, suffix := range []string{"src", "src/deeper"} {
			t.Run(source+"/"+suffix, func(t *testing.T) {
				now := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
				svc := newServiceForTest(&now)
				root, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				cwd := filepath.Join(root, filepath.FromSlash(suffix))
				if err := os.MkdirAll(cwd, 0o755); err != nil {
					t.Fatal(err)
				}
				catalog := sqliteWorkspaceCatalogForTest(t, cwd, now)
				workspaces, err := catalog.RecentWorkspaces(10)
				if err != nil || workspaces[cwd].IsZero() {
					t.Fatalf("real catalog must expose the current cwd as a recent workspace: %v (%v)", workspaces, err)
				}
				svc.SetPersistedThreadCatalog(catalog)
				surfaceID, chatID := "surface", "chat"
				if source == "runtime" {
					svc.UpsertInstance(&state.InstanceRecord{
						InstanceID: "inst-root", WorkspaceRoot: root, WorkspaceKey: root,
						Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
						Threads: map[string]*state.ThreadRecord{"thread": {
							ThreadID: "thread", Name: "Session", CWD: root, WorkspaceKey: root, Loaded: true,
						}},
					})
				} else if source == "restored-room" {
					chatID, surfaceID = "oc_saved", "feishu:app:chat:oc_saved"
					svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
						RoomID: "feishu:chat:oc_saved", ChatID: chatID, WorkspaceKey: root,
					}})
				}
				svc.MaterializeSurfaceResume(surfaceID, "app", chatID, "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
				surface := svc.root.Surfaces[surfaceID]
				views := svc.mergedThreadViews(surface)
				if len(views) != 1 {
					t.Fatalf("expected one saved thread, got %#v", views)
				}
				wantRoot := root
				if source == "cwd-only" {
					wantRoot = cwd
				}
				fallback := svc.persistedThreadView(surface, "thread")
				if fallback == nil || !testutil.SamePath(mergedThreadWorkspaceClaimKey(fallback), wantRoot) || !testutil.SamePath(threadCWD(fallback), cwd) {
					t.Fatalf("global exact fallback must use root %q and saved cwd %q: %#v", wantRoot, cwd, fallback)
				}
				for _, view := range []*mergedThreadView{views[0], svc.mergedThreadView(surface, "thread")} {
					if view == nil || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), wantRoot) || !testutil.SamePath(threadCWD(view), cwd) {
						t.Fatalf("list and exact lookup must use root %q and saved cwd %q: %#v", wantRoot, cwd, view)
					}
					if source == "runtime" {
						if view.FreeVisibleInst == nil || view.FreeVisibleInst.InstanceID != "inst-root" {
							t.Fatalf("an ordinary cwd must keep the original root instance eligible: %#v", view)
						}
					} else if view.AnyVisibleInst != nil || view.FreeVisibleInst != nil || view.CurrentVisible {
						t.Fatalf("a restored room binding must not invent a visible runtime: %#v", view)
					}
				}
			})
		}
	}
}

func sqliteWorkspaceCatalogForTest(t *testing.T, cwd string, now time.Time) *codexstate.SQLiteThreadCatalog {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state.sqlite")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY, title TEXT, cwd TEXT, updated_at INTEGER,
		archived INTEGER, model TEXT, reasoning_effort TEXT, first_user_message TEXT,
		source TEXT, agent_role TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES
		('thread', 'Session', ?, ?, 0, '', '', 'First message', 'cli', '')`, cwd, now.Add(-time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	return codexstate.NewSQLiteThreadCatalog(dbPath, codexstate.SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}})
}
