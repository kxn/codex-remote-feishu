package orchestrator

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/codexstate"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func desktopNameFixture(t *testing.T) (*Service, *state.SurfaceConsoleRecord, *sql.DB) {
	t.Helper()
	now := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	root := filepath.Join(t.TempDir(), "dasheng_memory")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	var err error
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "state_5.sqlite")
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE threads (
		id TEXT PRIMARY KEY, title TEXT, name TEXT, cwd TEXT, updated_at INTEGER,
		archived INTEGER, model TEXT, reasoning_effort TEXT, first_user_message TEXT,
		source TEXT, agent_role TEXT
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES
		('thread', '最初输入', 'Memory', ?, ?, 0, 'model', 'high', '最初输入', 'cli', '')`, root, now.Add(-time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	svc.SetPersistedThreadCatalog(codexstate.NewSQLiteThreadCatalog(dbPath, codexstate.SQLiteThreadCatalogOptions{Logf: func(string, ...any) {}}))
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst", Backend: agentproto.BackendCodex, Source: "headless", Managed: true, Online: true,
		WorkspaceRoot: root, WorkspaceKey: root,
		Threads: map[string]*state.ThreadRecord{"thread": {
			ThreadID: "thread", Name: "旧运行名称", CWD: root, WorkspaceKey: root,
			LastUsedAt: now, Loaded: true, FirstUserMessage: "最初输入",
		}},
	})
	svc.MaterializeSurfaceResume("surface", "gateway", "chat", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
	surface := svc.root.Surfaces["surface"]
	surface.AttachedInstanceID, surface.SelectedThreadID = "inst", "thread"
	surface.ClaimedWorkspaceKey, surface.RouteMode = root, state.RouteModePinned
	svc.bindInstanceClaim(surface, "inst")
	return svc, surface, db
}

func TestDesktopRenameRefreshesListAndStatusWithoutRetargeting(t *testing.T) {
	svc, surface, db := desktopNameFixture(t)
	inst := svc.root.Instances["inst"]
	beforeThread := *inst.Threads["thread"]
	beforeSurface := *surface
	for _, name := range []string{"Memory", "Memory 部署"} {
		if _, err := db.Exec("UPDATE threads SET name = ? WHERE id = 'thread'", name); err != nil {
			t.Fatal(err)
		}
		view := svc.mergedThreadView(surface, "thread")
		if view == nil || displayThreadTitle(view.Inst, view.Thread) != "dasheng_memory · "+name {
			t.Errorf("active runtime must not hide saved desktop name %q: %#v", name, view)
		}
		current := svc.currentInstanceThreadViews(surface)
		if len(current) != 1 || displayThreadTitle(current[0].Inst, current[0].Thread) != "dasheng_memory · "+name {
			t.Errorf("current-instance dropdown must also use saved name %q", name)
		}
		snapshot := svc.buildSnapshot(surface)
		if snapshot.Attachment.SelectedThreadTitle != "dasheng_memory · "+name {
			t.Errorf("selected status must show saved name %q, got %q", name, snapshot.Attachment.SelectedThreadTitle)
		}
		if len(snapshot.Threads) != 1 || snapshot.Threads[0].Name != name || snapshot.Threads[0].DisplayTitle != "dasheng_memory · "+name {
			t.Errorf("snapshot list must agree with selected status: %#v", snapshot.Threads)
		}
	}
	after := inst.Threads["thread"]
	if after.Name != beforeThread.Name || after.CWD != beforeThread.CWD || after.WorkspaceKey != beforeThread.WorkspaceKey || !after.LastUsedAt.Equal(beforeThread.LastUsedAt) {
		t.Fatal("name projection mutated runtime identity, name or activity")
	}
	if surface.SelectedThreadID != beforeSurface.SelectedThreadID || surface.AttachedInstanceID != beforeSurface.AttachedInstanceID || surface.ClaimedWorkspaceKey != beforeSurface.ClaimedWorkspaceKey || surface.RouteMode != beforeSurface.RouteMode {
		t.Fatal("name projection retargeted the surface")
	}
}

func TestDesktopNameCacheDoesNotRollBackNewNativeName(t *testing.T) {
	svc, surface, _ := desktopNameFixture(t)
	root := surface.ClaimedWorkspaceKey
	row := state.ThreadRecord{ThreadID: "thread", CWD: root, Name: "旧桌面名称", PersistedName: "旧桌面名称", LastUsedAt: time.Unix(1, 0)}
	catalog := &fakePersistedThreadCatalog{recent: []state.ThreadRecord{row}, byID: map[string]state.ThreadRecord{"thread": row}}
	svc.SetPersistedThreadCatalog(catalog)
	_ = svc.mergedThreadViews(surface) // fill the successful-read cache
	svc.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: "thread", Name: "刚收到的新名称"})
	catalog.recentErr, catalog.byIDErr = errors.New("database busy"), errors.New("database busy")
	view := svc.mergedThreadView(surface, "thread")
	if view == nil || view.Thread.Name != "刚收到的新名称" {
		t.Fatal("failed exact lookup rolled back the native name")
	}
	for _, listed := range svc.mergedThreadViews(surface) {
		if listed.ThreadID == "thread" && listed.Thread.Name != "刚收到的新名称" {
			t.Errorf("cached name overrode fresh native notification: %q", listed.Thread.Name)
		}
	}
	if got := svc.buildSnapshot(surface).Attachment.SelectedThreadTitle; got != "dasheng_memory · 刚收到的新名称" {
		t.Errorf("failed catalog read must retain native name, got %q", got)
	}
	row.Name, row.PersistedName = "之后的桌面名称", "之后的桌面名称"
	catalog.recent, catalog.byID = []state.ThreadRecord{row}, map[string]state.ThreadRecord{"thread": row}
	catalog.recentErr, catalog.byIDErr = nil, nil
	if got := svc.buildSnapshot(surface).Attachment.SelectedThreadTitle; got != "dasheng_memory · 之后的桌面名称" {
		t.Errorf("recovered catalog must expose the next saved name, got %q", got)
	}
}

func TestDesktopNameSelectedOutsideRecentListStillRefreshes(t *testing.T) {
	svc, surface, _ := desktopNameFixture(t)
	row := state.ThreadRecord{ThreadID: "thread", CWD: surface.ClaimedWorkspaceKey, Name: "远期会话", PersistedName: "远期会话"}
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{byID: map[string]state.ThreadRecord{"thread": row}})
	if got := svc.buildSnapshot(surface).Attachment.SelectedThreadTitle; got != "dasheng_memory · 远期会话" {
		t.Fatalf("selected thread outside recent window must use exact lookup, got %q", got)
	}
	views := svc.mergedThreadViews(surface)
	if len(views) != 1 || views[0].Thread.Name != "远期会话" {
		t.Fatal("headless list must refresh an old selected thread outside the recent window")
	}
}

func TestDesktopNameUnselectedOutsideRecentListStillRefreshes(t *testing.T) {
	svc, surface, _ := desktopNameFixture(t)
	inst := svc.root.Instances["inst"]
	inst.Threads["old-thread"] = &state.ThreadRecord{ThreadID: "old-thread", Name: "旧运行名", CWD: surface.ClaimedWorkspaceKey, WorkspaceKey: surface.ClaimedWorkspaceKey, Loaded: true}
	row := state.ThreadRecord{ThreadID: "old-thread", CWD: surface.ClaimedWorkspaceKey, Name: "旧会话的新名字", PersistedName: "旧会话的新名字"}
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{byID: map[string]state.ThreadRecord{"old-thread": row}})
	for _, views := range [][]*mergedThreadView{svc.mergedThreadViews(surface), svc.currentInstanceThreadViews(surface)} {
		found := false
		for _, view := range views {
			if view.ThreadID == "old-thread" {
				found = true
				if view.Thread.Name != "旧会话的新名字" {
					t.Errorf("unselected old thread did not refresh: %q", view.Thread.Name)
				}
			}
		}
		if !found {
			t.Fatal("unselected old thread disappeared")
		}
	}
}

func TestDesktopNameRecentReadFailureStillUsesFreshExactName(t *testing.T) {
	svc, surface, _ := desktopNameFixture(t)
	row := state.ThreadRecord{ThreadID: "thread", CWD: surface.ClaimedWorkspaceKey, Name: "旧名称", PersistedName: "旧名称"}
	catalog := &fakePersistedThreadCatalog{recent: []state.ThreadRecord{row}, byID: map[string]state.ThreadRecord{"thread": row}}
	svc.SetPersistedThreadCatalog(catalog)
	_ = svc.buildSnapshot(surface)
	row.Name, row.PersistedName = "按ID读取的新名称", "按ID读取的新名称"
	catalog.byID["thread"] = row
	catalog.recentErr = errors.New("recent query failed")
	if got := svc.buildSnapshot(surface).Attachment.SelectedThreadTitle; got != "dasheng_memory · 按ID读取的新名称" {
		t.Fatalf("cached membership must not prevent fresh exact-name lookup, got %q", got)
	}
}

func TestDesktopNameProjectionDoesNotUseCodexNamesForClaude(t *testing.T) {
	svc, surface, _ := desktopNameFixture(t)
	svc.root.Instances["inst"].Backend = agentproto.BackendClaude
	if got := svc.buildSnapshot(surface).Attachment.SelectedThreadTitle; got != "dasheng_memory · 旧运行名称" {
		t.Fatalf("Codex desktop name leaked into another backend: %q", got)
	}
}

func TestDesktopBatchNameReadFailureRetainsNativeName(t *testing.T) {
	svc, surface, db := desktopNameFixture(t)
	_ = svc.mergedThreadViews(surface) // cache the prior successful desktop name
	svc.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: "thread", Name: "通知中的新名称"})
	var seq int
	var database, filename string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &database, &filename); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// Only the isolated fixture disappears. The real batch-capable SQLite
	// catalog now fails to open, so projection must not reuse an old name.
	if err := os.Rename(filename, filename+".unavailable"); err != nil {
		t.Fatal(err)
	}
	if got := svc.buildSnapshot(surface).Attachment.SelectedThreadTitle; got != "dasheng_memory · 通知中的新名称" {
		t.Fatalf("batch failure rolled back native name: %q", got)
	}
	for _, view := range svc.mergedThreadViews(surface) {
		if view.ThreadID == "thread" && view.Thread.Name != "通知中的新名称" {
			t.Fatalf("cached list name overrode native name after batch failure: %q", view.Thread.Name)
		}
	}
}

func TestDesktopNameFailedReadWithNewerCachedActivityKeepsNativeName(t *testing.T) {
	svc, surface, _ := desktopNameFixture(t)
	row := state.ThreadRecord{
		ThreadID: "thread", CWD: surface.ClaimedWorkspaceKey,
		Name: "旧桌面名称", PersistedName: "旧桌面名称", LastUsedAt: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC),
	}
	catalog := &fakePersistedThreadCatalog{recent: []state.ThreadRecord{row}, byID: map[string]state.ThreadRecord{"thread": row}}
	svc.SetPersistedThreadCatalog(catalog)
	_ = svc.mergedThreadViews(surface)
	svc.ApplyAgentEvent("inst", agentproto.Event{Kind: agentproto.EventThreadDiscovered, ThreadID: "thread", Name: "刚收到的新名称"})
	catalog.recentErr, catalog.byIDErr = errors.New("database busy"), errors.New("database busy")
	for _, view := range svc.mergedThreadViews(surface) {
		if view.ThreadID == "thread" && view.Thread.Name != "刚收到的新名称" {
			t.Fatalf("cached activity promoted an old explicit name after read failure: %q", view.Thread.Name)
		}
	}
}
