package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestCompleteTargetPickerWorktreeCreateAllowsInternalWorkspaceReattachWhilePickerProcessing(t *testing.T) {
	now := time.Date(2026, 4, 14, 16, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	currentWorkspace := createTargetPickerGitRepo(t)
	createdWorkspace := createTargetPickerGitRepo(t)

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-current",
		DisplayName:   "current",
		WorkspaceRoot: currentWorkspace,
		WorkspaceKey:  currentWorkspace,
		ShortName:     "current",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-created",
		DisplayName:   "created",
		WorkspaceRoot: createdWorkspace,
		WorkspaceKey:  createdWorkspace,
		ShortName:     "created",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	svc.attachWorkspace(surface, currentWorkspace)

	record := &activeTargetPickerRecord{
		PickerID:             "picker-1",
		OwnerUserID:          "user-1",
		Source:               control.TargetPickerRequestSourceWorktree,
		Stage:                control.FeishuTargetPickerStageProcessing,
		PendingKind:          targetPickerPendingWorktreeCreate,
		SelectedWorkspaceKey: currentWorkspace,
		WorktreeBranchName:   "feat/login",
		WorktreeFinalPath:    createdWorkspace,
	}
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
	svc.setActiveTargetPicker(surface, record)

	events := svc.CompleteTargetPickerWorktreeCreate("surface-1", "picker-1", createdWorkspace)

	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, createdWorkspace) {
		t.Fatalf("expected created worktree workspace to enter new-thread-ready despite active picker processing, got %#v", surface)
	}
	if picker := svc.activeTargetPicker(surface); picker != nil {
		t.Fatalf("expected successful worktree completion to clear target picker after internal reattach, got %#v", picker)
	}
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected same-card success after internal reattach, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded terminal card after internal reattach, got %#v", got)
	}
}
