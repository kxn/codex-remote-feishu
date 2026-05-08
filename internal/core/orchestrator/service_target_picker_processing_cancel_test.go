package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestTargetPickerCancelWorktreeProcessingSealsCardAndDispatchesCancel(t *testing.T) {
	now := time.Date(2026, 4, 14, 15, 59, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	record := &activeTargetPickerRecord{
		PickerID:             "picker-1",
		OwnerUserID:          "user-1",
		Source:               control.TargetPickerRequestSourceWorktree,
		Stage:                control.FeishuTargetPickerStageProcessing,
		PendingKind:          targetPickerPendingWorktreeCreate,
		PendingWorkspaceKey:  "/data/dl/projects/repo-login",
		SelectedWorkspaceKey: "/data/dl/projects/repo",
		WorktreeBranchName:   "feat/login",
		WorktreeFinalPath:    "/data/dl/projects/repo-login",
	}
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
	svc.setActiveTargetPicker(surface, record)
	svc.RecordTargetPickerMessage("surface-1", record.PickerID, "om-card-1")

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTargetPickerCancel,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		PickerID:         record.PickerID,
	})
	if len(events) != 2 || events[0].TargetPickerView == nil || events[1].DaemonCommand == nil {
		t.Fatalf("expected cancelled same-card result plus daemon cancel, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageCancelled || got.StatusTitle != "已取消创建" || got.MessageID != "om-card-1" {
		t.Fatalf("expected cancelled worktree terminal card on original owner card, got %#v", got)
	}
	if got := events[1].DaemonCommand; got.Kind != control.DaemonCommandGitWorkspaceWorktreeCancel || got.PickerID != record.PickerID {
		t.Fatalf("expected worktree cancel daemon command, got %#v", got)
	}
	if svc.activeTargetPicker(surface) != nil || svc.activeOwnerCardFlow(surface) != nil {
		t.Fatalf("expected cancel to clear target picker runtime, got %#v", svc.SurfaceUIRuntime("surface-1"))
	}
}
