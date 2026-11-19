package orchestrator

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

// A native thread list is global. Before restart, the instance that actually
// ran the thread wins by LastUsedAt. Cold snapshots have no such timestamp and
// can instead select an unrelated instance's copy of the same thread.
func TestPersistedWorkspaceRecoveryKeepsBothSessionsVisibleAfterRestart(t *testing.T) {
	for _, cold := range []bool{false, true} {
		name := "warm"
		if cold {
			name = "cold"
		}
		t.Run(name, func(t *testing.T) {
			now := time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			parent := t.TempDir()
			child := filepath.Join(parent, "memory")
			if err := os.MkdirAll(child, 0o755); err != nil {
				t.Fatal(err)
			}
			persisted := []state.ThreadRecord{
				{ThreadID: "migrated-memory", Name: "Memory", CWD: child, Loaded: true, LastUsedAt: now.Add(-2 * time.Minute)},
				{ThreadID: "existing-memory", Name: "Existing memory", CWD: child, Loaded: true, LastUsedAt: now.Add(-3 * time.Minute)},
			}
			for _, instID := range []string{"inst-a-parent", "inst-z-memory"} {
				root := parent
				if instID == "inst-z-memory" {
					root = child
				}
				threads := map[string]*state.ThreadRecord{}
				for _, row := range persisted {
					cwd := child
					if row.ThreadID == "migrated-memory" {
						cwd = parent // stale native listing after an explicit cwd migration
					}
					thread := &state.ThreadRecord{ThreadID: row.ThreadID, Name: row.Name, CWD: cwd, WorkspaceKey: root}
					if !cold && instID == "inst-z-memory" && row.ThreadID == "existing-memory" {
						thread.LastUsedAt = now.Add(-time.Minute)
						thread.Loaded = true
					}
					threads[row.ThreadID] = thread
				}
				svc.UpsertInstance(&state.InstanceRecord{
					InstanceID: instID, WorkspaceRoot: root, WorkspaceKey: root,
					Source: "headless", Backend: agentproto.BackendCodex,
					Managed: true, Online: true, Threads: threads,
				})
			}
			byID := map[string]state.ThreadRecord{}
			for _, row := range persisted {
				byID[row.ThreadID] = row
			}
			svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{recent: persisted, byID: byID})
			for _, id := range []string{"surface-parent", "surface-memory"} {
				svc.MaterializeSurfaceResume(id, id, id, "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
			}
			parentSurface := svc.root.Surfaces["surface-parent"]
			parentSurface.AttachedInstanceID = "inst-a-parent"
			parentSurface.ClaimedWorkspaceKey = parent
			svc.bindInstanceClaim(parentSurface, parentSurface.AttachedInstanceID)
			memorySurface := svc.root.Surfaces["surface-memory"]
			memorySurface.AttachedInstanceID = "inst-z-memory"
			memorySurface.ClaimedWorkspaceKey = child
			memorySurface.PreparedThreadCWD = child
			memorySurface.RouteMode = state.RouteModeNewThreadReady
			svc.bindInstanceClaim(memorySurface, memorySurface.AttachedInstanceID)

			options := svc.targetPickerSessionOptions(memorySurface, workspaceSelectionEntry{workspaceKey: child}, control.TargetPickerRequestSourceList, true)
			for _, row := range persisted {
				found := false
				for _, option := range options {
					if option.Value == targetPickerThreadValue(row.ThreadID) {
						found = true
					}
				}
				if !found {
					t.Errorf("session %s disappeared from its workspace in %s snapshots: %#v", row.ThreadID, name, options)
				}
				view := svc.mergedThreadView(memorySurface, row.ThreadID)
				if view == nil || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), child) {
					t.Errorf("session %s must resolve to its saved workspace, got %#v", row.ThreadID, view)
					continue
				}
				target := svc.resolveThreadTargetFromView(memorySurface, view)
				if target.Mode == threadAttachUnavailable || target.NoticeCode == "workspace_busy" {
					t.Errorf("session %s must be recoverable without acquiring the parent bot's workspace: %#v", row.ThreadID, target)
				}
				if target.Instance != nil && target.Instance.InstanceID != "inst-z-memory" {
					t.Errorf("session %s selected the wrong runtime: %s", row.ThreadID, target.Instance.InstanceID)
				}
			}
			if !testutil.SamePath(parentSurface.ClaimedWorkspaceKey, parent) || parentSurface.AttachedInstanceID != "inst-a-parent" {
				t.Fatal("listing memory sessions must not retarget the other bot")
			}
		})
	}
}

func TestPersistedWorkspaceRecoveryPreservesStableRoots(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cwd          string
		knownChild   bool
		nativeNewer  bool
		persistedKey bool
		surfaceRoot  string
		wantChild    bool
	}{
		{name: "ordinary subdirectory keeps root", cwd: "src"},
		{name: "known nested workspace wins over cold copies", cwd: "memory", knownChild: true, wantChild: true},
		{name: "a deeper working directory keeps its known workspace", cwd: "memory/src", knownChild: true, wantChild: true},
		{name: "newer native root remains authoritative", cwd: "memory", knownChild: true, nativeNewer: true},
		{name: "explicit persisted root remains authoritative", cwd: "memory", knownChild: true, persistedKey: true},
		{name: "saved nested workspace needs no runtime", cwd: "memory", surfaceRoot: "memory", wantChild: true},
		{name: "saved root also covers deeper cwd", cwd: "memory/src", surfaceRoot: "memory", wantChild: true},
		{name: "ordinary child cwd retains saved parent root", cwd: "src", surfaceRoot: "."},
		{name: "newer native root beats saved child", cwd: "memory", surfaceRoot: "memory", nativeNewer: true},
		{name: "explicit persisted root beats saved child", cwd: "memory", surfaceRoot: "memory", persistedKey: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			parent := t.TempDir()
			child := filepath.Join(parent, "memory")
			cwd := filepath.Join(parent, filepath.FromSlash(tc.cwd))
			if err := os.MkdirAll(cwd, 0o755); err != nil {
				t.Fatal(err)
			}
			native := &state.ThreadRecord{ThreadID: "thread", Name: "Session", CWD: parent, WorkspaceKey: parent}
			if tc.nativeNewer {
				native.CWD = cwd
				native.LastUsedAt = now
			}
			svc.UpsertInstance(&state.InstanceRecord{InstanceID: "inst-parent", WorkspaceRoot: parent, WorkspaceKey: parent, Online: true,
				Threads: map[string]*state.ThreadRecord{"thread": native}})
			if tc.knownChild {
				svc.UpsertInstance(&state.InstanceRecord{InstanceID: "inst-child", WorkspaceRoot: child, WorkspaceKey: child, Online: true})
			}
			row := state.ThreadRecord{ThreadID: "thread", Name: "Session", CWD: cwd, Loaded: true, LastUsedAt: now.Add(-time.Minute)}
			if tc.persistedKey {
				row.WorkspaceKey = parent
			}
			// SQLite RecentWorkspaces groups raw CWD values; these are not stable roots.
			workspaces := map[string]time.Time{cwd: now.Add(-time.Minute)}
			if tc.surfaceRoot != "" {
				svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
					RoomID: "feishu:chat:oc_saved", ChatID: "oc_saved", WorkspaceKey: filepath.Join(parent, tc.surfaceRoot),
				}})
				svc.MaterializeSurfaceResume("feishu:app:chat:oc_saved", "app", "oc_saved", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
			}
			svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{recent: []state.ThreadRecord{row}, recentWorkspaces: workspaces, byID: map[string]state.ThreadRecord{"thread": row}})
			want := parent
			if tc.wantChild {
				want = child
			}
			views := svc.mergedThreadViews(nil)
			if len(views) != 1 || !testutil.SamePath(mergedThreadWorkspaceClaimKey(views[0]), want) {
				got := ""
				if len(views) == 1 {
					got = mergedThreadWorkspaceClaimKey(views[0])
				}
				t.Fatalf("list grouping must use workspace %q, got %q", want, got)
			}
			view := svc.mergedThreadView(nil, "thread")
			if view == nil || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), want) {
				t.Fatalf("single-thread lookup must agree with list grouping %q, got %#v", want, view)
			}
		})
	}
}

func persistedWorkspaceRecoveryFixture(t *testing.T, migrated bool) (*Service, *state.SurfaceConsoleRecord, string, string) {
	t.Helper()
	now := time.Date(2026, 10, 20, 6, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	parent := t.TempDir()
	child := filepath.Join(parent, "memory")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	nativeCWD := child
	if migrated {
		nativeCWD = parent
	}
	for _, id := range []string{"inst-a-parent", "inst-z-memory"} {
		root := parent
		if id == "inst-z-memory" {
			root = child
		}
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID: id, WorkspaceRoot: root, WorkspaceKey: root,
			Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
			Threads: map[string]*state.ThreadRecord{"thread": {ThreadID: "thread", Name: "Memory", CWD: nativeCWD, WorkspaceKey: root}},
		})
	}
	row := state.ThreadRecord{ThreadID: "thread", Name: "Memory", CWD: child, Loaded: true, LastUsedAt: now.Add(-time.Minute)}
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{recent: []state.ThreadRecord{row}, byID: map[string]state.ThreadRecord{"thread": row}})
	svc.MaterializeSurfaceResume("surface", "app", "chat", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
	return svc, svc.root.Surfaces["surface"], parent, child
}

func TestPersistedWorkspaceRecoveryReappliesSavedCWDInsteadOfUsingCurrentParentCopy(t *testing.T) {
	svc, surface, parent, child := persistedWorkspaceRecoveryFixture(t, true)
	surface.AttachedInstanceID = "inst-a-parent"
	surface.ClaimedWorkspaceKey = parent
	svc.bindInstanceClaim(surface, surface.AttachedInstanceID)
	target := svc.resolveThreadTargetFromView(surface, svc.mergedThreadView(surface, "thread"))
	if target.Mode == threadAttachCurrentVisible {
		t.Fatal("the parent snapshot must not bypass restoration of saved workspace and cwd")
	}

	svc.useThread(surface, "thread", true)

	if surface.AttachedInstanceID == "" || surface.SelectedThreadID != "thread" || !testutil.SamePath(surface.ClaimedWorkspaceKey, child) {
		t.Fatalf("selection must restore the saved child workspace, got instance=%s thread=%s workspace=%s", surface.AttachedInstanceID, surface.SelectedThreadID, surface.ClaimedWorkspaceKey)
	}
	thread := svc.root.Instances[surface.AttachedInstanceID].Threads["thread"]
	if !testutil.SamePath(thread.CWD, child) {
		t.Fatalf("selected thread must use saved child cwd, got %s", thread.CWD)
	}
	_, executionCWD, _, _ := freezeRoute(svc.root.Instances[surface.AttachedInstanceID], surface)
	if !testutil.SamePath(executionCWD, child) {
		t.Fatalf("the next prompt must execute in the saved child cwd, got %s", executionCWD)
	}
	if !testutil.SamePath(svc.root.Instances["inst-a-parent"].WorkspaceRoot, parent) {
		t.Fatal("selection must not retarget the parent instance")
	}
}

func TestPersistedWorkspaceRecoveryStartupDoesNotAttachParentCopy(t *testing.T) {
	for _, migrated := range []bool{false, true} {
		for _, preferred := range []string{"inst-a-parent", "inst-z-memory"} {
			name := "existing/" + preferred
			if migrated {
				name = "migrated/" + preferred
			}
			t.Run(name, func(t *testing.T) {
				svc, surface, parent, child := persistedWorkspaceRecoveryFixture(t, migrated)
				_, result := svc.TryAutoResumeHeadlessSurface(surface.SurfaceSessionID, SurfaceResumeAttempt{
					ThreadID: "thread", InstanceID: preferred, WorkspaceKey: child, ThreadCWD: child,
					Backend: agentproto.BackendCodex, ResumeHeadless: true,
				}, true)
				if result.Status != SurfaceResumeStatusThreadAttached || surface.AttachedInstanceID != "inst-z-memory" || !testutil.SamePath(surface.ClaimedWorkspaceKey, child) {
					t.Fatalf("startup must attach the saved child workspace even with stale preferred instance: result=%#v instance=%s workspace=%s", result, surface.AttachedInstanceID, surface.ClaimedWorkspaceKey)
				}
				if !testutil.SamePath(svc.root.Instances["inst-z-memory"].Threads["thread"].CWD, child) || !testutil.SamePath(svc.root.Instances["inst-a-parent"].WorkspaceRoot, parent) {
					t.Fatal("startup must restore the child cwd without retargeting the parent runtime")
				}
			})
		}
	}
}

func TestPersistedWorkspaceRecoveryManagedRestoreRejectsParentVisiblePointer(t *testing.T) {
	svc, surface, _, child := persistedWorkspaceRecoveryFixture(t, true)
	view := svc.mergedThreadView(surface, "thread")
	target := svc.resolveHeadlessRestoreTargetFromView(surface, view)
	if target.Instance == nil || target.Instance.InstanceID != "inst-z-memory" || !testutil.SamePath(threadCWD(target.View), child) {
		t.Fatalf("managed restore must use the child runtime and saved cwd, got %#v", target)
	}
}
