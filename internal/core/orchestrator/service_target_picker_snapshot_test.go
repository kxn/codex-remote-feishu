package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type navigationReadCountingCatalog struct {
	fakePersistedThreadCatalog
	reads int
}

func (c *navigationReadCountingCatalog) RecentThreadsForBackend(backend agentproto.Backend, limit int) ([]state.ThreadRecord, error) {
	c.reads++
	return c.fakePersistedThreadCatalog.RecentThreadsForBackend(backend, limit)
}

func (c *navigationReadCountingCatalog) RecentWorkspacesForBackend(backend agentproto.Backend, limit int) (map[string]time.Time, error) {
	c.reads++
	return c.fakePersistedThreadCatalog.RecentWorkspacesForBackend(backend, limit)
}

func (c *navigationReadCountingCatalog) ThreadByIDForBackend(backend agentproto.Backend, id string) (*state.ThreadRecord, error) {
	c.reads++
	return c.fakePersistedThreadCatalog.ThreadByIDForBackend(backend, id)
}

func (c *navigationReadCountingCatalog) ExplicitThreadNames(ids []string) (map[string]string, error) {
	c.reads++
	return nil, nil
}

func TestTargetPickerDraftNavigationUsesFrozenOptionsWithoutCatalogReads(t *testing.T) {
	now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	a, b := t.TempDir(), t.TempDir()
	for _, fixture := range []struct{ id, cwd, thread string }{{"a", a, "thread-a"}, {"b", b, "thread-b"}} {
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID: fixture.id, WorkspaceRoot: fixture.cwd, WorkspaceKey: fixture.cwd, Online: true,
			Threads: map[string]*state.ThreadRecord{fixture.thread: {ThreadID: fixture.thread, Name: fixture.thread, CWD: fixture.cwd, Loaded: true, LastUsedAt: now}},
		})
	}
	catalog := &navigationReadCountingCatalog{}
	svc.SetPersistedThreadCatalog(catalog)
	initial := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{Kind: control.ActionListInstances, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"}))
	if catalog.reads == 0 {
		t.Fatal("opening must collect the live catalog read model")
	}
	catalog.reads = 0
	surface := svc.root.Surfaces["surface-1"]
	beforeInstance, beforeThread, beforeRoute := surface.AttachedInstanceID, surface.SelectedThreadID, surface.RouteMode
	for _, action := range []control.Action{
		{Kind: control.ActionTargetPickerSelectWorkspace, WorkspaceKey: a},
		{Kind: control.ActionTargetPickerSelectSession, TargetPickerValue: targetPickerThreadValue("thread-a")},
		{Kind: control.ActionTargetPickerPage, FieldName: frontstagecontract.CardTargetPickerSessionFieldName, Cursor: 0},
		{Kind: control.ActionTargetPickerPage, FieldName: frontstagecontract.CardTargetPickerWorkspaceFieldName, Cursor: 1},
	} {
		action.SurfaceSessionID, action.ActorUserID, action.PickerID = "surface-1", "user-1", initial.PickerID
		if !svc.IsTargetPickerDraftNavigation(action) {
			t.Fatalf("expected pure navigation: %s", action.Kind)
		}
		events := svc.ApplySurfaceAction(action)
		if len(events) != 1 || !events[0].InlineReplaceCurrentCard || events[0].TargetPickerView == nil {
			t.Fatalf("navigation did not replace its card: %#v", events)
		}
	}
	if catalog.reads != 0 {
		t.Fatalf("draft navigation reentered catalog %d times", catalog.reads)
	}
	if surface.AttachedInstanceID != beforeInstance || surface.SelectedThreadID != beforeThread || surface.RouteMode != beforeRoute {
		t.Fatal("draft navigation mutated the route")
	}
	confirm := control.Action{Kind: control.ActionTargetPickerConfirm, SurfaceSessionID: "surface-1", ActorUserID: "user-1", PickerID: initial.PickerID, WorkspaceKey: a, TargetPickerValue: targetPickerThreadValue("thread-a")}
	if svc.IsTargetPickerDraftNavigation(confirm) {
		t.Fatal("confirmation must not bypass live acceptance")
	}
	svc.ApplySurfaceAction(confirm)
	if catalog.reads == 0 {
		t.Fatal("confirmation did not revalidate against the live catalog")
	}
}

func TestTargetPickerDraftNavigationRejectsOtherPagesAndOldPickers(t *testing.T) {
	svc := newServiceForTest(new(time.Time))
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	record := &activeTargetPickerRecord{PickerID: "picker", Source: control.TargetPickerRequestSourceList, Stage: control.FeishuTargetPickerStageEditing, OptionSnapshot: &targetPickerOptionSnapshot{}}
	svc.setActiveTargetPicker(surface, record)
	action := control.Action{Kind: control.ActionTargetPickerSelectWorkspace, SurfaceSessionID: surface.SurfaceSessionID, PickerID: record.PickerID}
	if !svc.IsTargetPickerDraftNavigation(action) {
		t.Fatal("target page should be pure navigation")
	}
	for _, page := range []control.FeishuTargetPickerPage{control.FeishuTargetPickerPageWorktree, control.FeishuTargetPickerPageLocalDirectory, control.FeishuTargetPickerPageGit} {
		record.PageOverride = page
		if svc.IsTargetPickerDraftNavigation(action) {
			t.Fatalf("form page %s must retain ordinary validation", page)
		}
	}
	record.PageOverride = ""
	action.PickerID = "old-picker"
	if svc.IsTargetPickerDraftNavigation(action) {
		t.Fatal("unrelated picker must not use the navigation episode")
	}
}

func TestTargetPickerConfirmationRevalidatesFrozenTarget(t *testing.T) {
	for _, change := range []string{"thread removed", "workspace claimed"} {
		t.Run(change, func(t *testing.T) {
			now := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			cwd := t.TempDir()
			svc.UpsertInstance(&state.InstanceRecord{InstanceID: "instance", WorkspaceRoot: cwd, WorkspaceKey: cwd, Online: true, Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", Name: "thread", CWD: cwd, Loaded: true, LastUsedAt: now}}})
			initial := singleTargetPickerEvent(t, svc.ApplySurfaceAction(control.Action{Kind: control.ActionListInstances, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"}))
			surface := svc.root.Surfaces["surface-1"]
			switch change {
			case "thread removed":
				delete(svc.root.Instances["instance"].Threads, "thread")
			case "workspace claimed":
				svc.MaterializeSurface("other", "other-bot", "other-chat", "other-user")
				if !svc.claimWorkspace(svc.root.Surfaces["other"], cwd) {
					t.Fatal("fixture failed to claim workspace")
				}
			}
			events := svc.ApplySurfaceAction(control.Action{Kind: control.ActionTargetPickerConfirm, SurfaceSessionID: surface.SurfaceSessionID, ActorUserID: "user-1", PickerID: initial.PickerID, WorkspaceKey: cwd, TargetPickerValue: targetPickerThreadValue("thread")})
			view := singleTargetPickerEvent(t, events)
			if surface.AttachedInstanceID != "" || surface.SelectedThreadID != "" || surface.PendingHeadless != nil {
				t.Fatal("confirmation trusted stale target instead of current eligibility")
			}
			if len(view.Messages) == 0 {
				t.Fatal("changed target needs visible re-selection feedback")
			}
		})
	}
}
