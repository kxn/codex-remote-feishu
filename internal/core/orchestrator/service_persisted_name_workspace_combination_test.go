package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

// Root recovery and name freshness are independent: restoring a globally copied thread
// must fix its route, then a rename without new activity must still project over
// a newer runtime record without changing that recovered route.
func TestPersistedDesktopNameAndNestedWorkspaceRecoveryCompose(t *testing.T) {
	svc, surface, parent, child := persistedWorkspaceRecoveryFixture(t, false)
	row := state.ThreadRecord{
		ThreadID: "thread", Name: "Saved name", PersistedName: "Saved name",
		CWD: child, Loaded: true, LastUsedAt: time.Date(2026, 10, 20, 5, 59, 0, 0, time.UTC),
	}
	catalog := &fakePersistedThreadCatalog{
		recent: []state.ThreadRecord{row}, byID: map[string]state.ThreadRecord{"thread": row},
	}
	svc.SetPersistedThreadCatalog(catalog)
	surface.AttachedInstanceID, surface.ClaimedWorkspaceKey = "inst-a-parent", parent
	svc.bindInstanceClaim(surface, surface.AttachedInstanceID)
	view := svc.mergedThreadView(surface, "thread")
	if view == nil || view.Thread.Name != row.Name || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), child) {
		t.Fatalf("fresh saved name and child root must survive the same metadata merge: %#v", view)
	}
	svc.useThread(surface, "thread", true)
	if surface.AttachedInstanceID != "inst-z-memory" || surface.SelectedThreadID != "thread" || !testutil.SamePath(surface.ClaimedWorkspaceKey, child) {
		t.Fatalf("restoration must select the child runtime: %#v", surface)
	}
	inst := svc.root.Instances[surface.AttachedInstanceID]
	native := inst.Threads["thread"]
	native.Name = "Native name before desktop rename"
	native.LastUsedAt = row.LastUsedAt.Add(time.Hour)
	row.Name, row.PersistedName = "Renamed without activity", "Renamed without activity"
	catalog.recent, catalog.byID["thread"] = []state.ThreadRecord{row}, row
	view = svc.mergedThreadView(surface, "thread")
	if view == nil || view.Thread.Name != row.Name || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), child) {
		t.Fatalf("explicit rename must override newer activity while preserving recovered root: %#v", view)
	}
	views := svc.mergedThreadViews(surface)
	if len(views) != 1 || views[0].Thread.Name != row.Name || !testutil.SamePath(mergedThreadWorkspaceClaimKey(views[0]), child) {
		t.Fatalf("merged list must agree with exact lookup: %#v", views)
	}
	snapshot := svc.buildSnapshot(surface)
	if snapshot.Attachment.SelectedThreadTitle != displayThreadTitle(inst, view.Thread) {
		t.Fatalf("selected snapshot lost the fresh name: %q", snapshot.Attachment.SelectedThreadTitle)
	}
	_, executionCWD, _, _ := freezeRoute(inst, surface)
	if !testutil.SamePath(executionCWD, child) || !testutil.SamePath(surface.ClaimedWorkspaceKey, child) || native.Name != "Native name before desktop rename" {
		t.Fatalf("name projection must preserve runtime name and next prompt cwd: name=%q cwd=%q workspace=%q", native.Name, executionCWD, surface.ClaimedWorkspaceKey)
	}
	if !testutil.SamePath(svc.root.Instances["inst-a-parent"].WorkspaceRoot, parent) {
		t.Fatal("recovery must preserve the parent runtime root")
	}
}
