package orchestrator

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestPersistedWorkspaceSQLiteUnrelatedParentDoesNotHideSavedChild(t *testing.T) {
	for _, tc := range []struct {
		name       string
		attached   bool
		globalCopy bool
	}{
		{name: "parent-runtime"},
		{name: "parent-runtime/global-copy", globalCopy: true},
		{name: "parent-bot", attached: true},
		{name: "parent-bot/global-copy", attached: true, globalCopy: true},
	} {
		for _, prewarmed := range []bool{false, true} {
			name := tc.name
			if prewarmed {
				name += "/prewarmed"
			}
			t.Run(name, func(t *testing.T) {
				now := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
				svc := newServiceForTest(&now)
				parent, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				child := filepath.Join(parent, "child")
				if err := os.MkdirAll(child, 0o755); err != nil {
					t.Fatal(err)
				}
				svc.SetPersistedThreadCatalog(sqliteWorkspaceCatalogForTest(t, child, now))
				parentRuntime := &state.InstanceRecord{
					InstanceID: "parent-runtime", WorkspaceRoot: parent, WorkspaceKey: parent,
					Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
					Threads: map[string]*state.ThreadRecord{"parent-thread": {
						ThreadID: "parent-thread", CWD: parent, WorkspaceKey: parent, Loaded: true,
					}},
				}
				if tc.globalCopy {
					parentRuntime.Threads["thread"] = &state.ThreadRecord{
						ThreadID: "thread", CWD: child, WorkspaceKey: parent,
					}
				}
				svc.UpsertInstance(parentRuntime)
				if prewarmed {
					pool := t.TempDir()
					svc.UpsertInstance(&state.InstanceRecord{
						InstanceID: "pool", WorkspaceRoot: pool, WorkspaceKey: pool,
						Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
						Threads: map[string]*state.ThreadRecord{"thread": {
							ThreadID: "thread", CWD: child, WorkspaceKey: child, Loaded: true,
						}},
					})
				}
				if tc.attached {
					svc.MaterializeSurfaceResume("parent-bot", "parent-app", "parent-chat", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
					owner := svc.root.Surfaces["parent-bot"]
					owner.AttachedInstanceID = "parent-runtime"
					owner.ClaimedWorkspaceKey = parent
					owner.SelectedThreadID = "parent-thread"
					svc.bindInstanceClaim(owner, owner.AttachedInstanceID)
				}
				svc.MaterializeSurfaceResume("child-bot", "child-app", "child-chat", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
				surface := svc.root.Surfaces["child-bot"]
				views := svc.mergedThreadViews(surface)
				var listed *mergedThreadView
				for _, view := range views {
					if view.ThreadID == "thread" {
						listed = view
					}
				}
				for _, view := range []*mergedThreadView{listed, svc.mergedThreadView(surface, "thread"), svc.persistedThreadView(surface, "thread")} {
					if view == nil || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), child) {
						t.Fatalf("an unrelated parent must not absorb the saved child session: got %q, want %q", mergedThreadWorkspaceClaimKey(view), child)
					}
				}
				options := svc.targetPickerSessionOptions(surface, workspaceSelectionEntry{workspaceKey: child}, control.TargetPickerRequestSourceList, true)
				if !targetPickerHasSessionOption(options, targetPickerThreadValue("thread")) {
					t.Fatalf("the child workspace must offer its saved session: %#v", options)
				}
				target := svc.resolveThreadTargetFromView(surface, listed)
				if target.Mode == threadAttachUnavailable || (tc.attached && target.Instance != nil && target.Instance.InstanceID == "parent-runtime") {
					t.Fatalf("the saved child must be recoverable without acquiring an occupied parent runtime: %#v", target)
				}
				if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.ClaimedWorkspaceKey != "" {
					t.Fatal("listing must not attach the detached child bot")
				}
				if tc.attached {
					owner := svc.root.Surfaces["parent-bot"]
					if owner.AttachedInstanceID != "parent-runtime" || owner.SelectedThreadID != "parent-thread" || !testutil.SamePath(owner.ClaimedWorkspaceKey, parent) {
						t.Fatal("listing must not retarget the parent bot")
					}
				}
			})
		}
	}
}

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
				foundCWD := false
				for path, usedAt := range workspaces {
					if testutil.SamePath(path, cwd) && !usedAt.IsZero() {
						foundCWD = true
						break
					}
				}
				if err != nil || !foundCWD {
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
