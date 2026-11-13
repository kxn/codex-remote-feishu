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

// After restart the child bot can be detached while the parent and prewarm
// runtimes already report global thread lists. Listing must not require a
// running child instance when a restored room explicitly names that workspace.
func TestPersistedWorkspaceRecoveryDetachedWithoutChildRuntime(t *testing.T) {
	now := time.Date(2026, 10, 22, 8, 22, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	parent := t.TempDir()
	child := filepath.Join(parent, "memory")
	pool := t.TempDir()
	if err := os.MkdirAll(filepath.Join(child, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	rows := []state.ThreadRecord{
		{ThreadID: "migrated", Name: "Memory", CWD: child, Loaded: true, LastUsedAt: now.Add(-time.Minute)},
		{ThreadID: "existing", Name: "Other memory", CWD: filepath.Join(child, "src"), Loaded: true, LastUsedAt: now.Add(-time.Hour)},
	}
	for id, root := range map[string]string{"inst-parent": parent, "inst-pool": pool} {
		svc.UpsertInstance(&state.InstanceRecord{
			InstanceID: id, WorkspaceRoot: root, WorkspaceKey: root,
			Source: "headless", Backend: agentproto.BackendCodex, Managed: true, Online: true,
			Threads: map[string]*state.ThreadRecord{
				"migrated": {ThreadID: "migrated", Name: "Memory", CWD: parent, WorkspaceKey: root},
				"existing": {ThreadID: "existing", Name: "Other memory", CWD: filepath.Join(child, "src"), WorkspaceKey: root},
			},
		})
	}
	svc.SetPersistedThreadCatalog(&fakePersistedThreadCatalog{
		recent: rows, recentWorkspaces: map[string]time.Time{child: now.Add(-time.Minute), filepath.Join(child, "src"): now.Add(-time.Hour)},
		byID: map[string]state.ThreadRecord{"migrated": rows[0], "existing": rows[1]},
	})
	svc.MaterializeSurfaceResume("parent", "parent", "parent", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
	svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID: "feishu:chat:oc_memory", ChatID: "oc_memory", WorkspaceKey: child,
	}})
	const memorySurfaceID = "feishu:memory:chat:oc_memory"
	svc.MaterializeSurfaceResume(memorySurfaceID, "memory", "oc_memory", "user", state.ProductModeNormal, agentproto.BackendCodex, "", "", "")
	owner := svc.root.Surfaces["parent"]
	owner.AttachedInstanceID = "inst-parent"
	owner.ClaimedWorkspaceKey = parent
	svc.bindInstanceClaim(owner, owner.AttachedInstanceID)
	surface := svc.root.Surfaces[memorySurfaceID]
	options := svc.targetPickerSessionOptions(surface, workspaceSelectionEntry{workspaceKey: child}, control.TargetPickerRequestSourceList, true)
	for _, row := range rows {
		if !targetPickerHasSessionOption(options, targetPickerThreadValue(row.ThreadID)) {
			t.Errorf("saved session %s must be listed without a child runtime, got %#v", row.ThreadID, options)
		}
		view := svc.mergedThreadView(surface, row.ThreadID)
		if view == nil || !testutil.SamePath(mergedThreadWorkspaceClaimKey(view), child) {
			t.Errorf("exact lookup must use the same saved child workspace: %#v", view)
			continue
		}
		target := svc.resolveThreadTargetFromView(surface, view)
		if target.Mode == threadAttachUnavailable || target.NoticeCode == "workspace_busy" {
			t.Errorf("child session must be recoverable without claiming parent: %#v", target)
		}
	}
	if surface.AttachedInstanceID != "" || !testutil.SamePath(surface.ClaimedWorkspaceKey, child) || surface.SelectedThreadID != "" {
		t.Fatal("listing must preserve the restored workspace without attaching the detached bot")
	}
	if owner.AttachedInstanceID != "inst-parent" || !testutil.SamePath(owner.ClaimedWorkspaceKey, parent) {
		t.Fatal("listing must not change the parent bot's attachment")
	}
}
