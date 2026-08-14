package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
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

func TestTargetPickerCancelProcessingMatchesPendingByResolvedWorkspace(t *testing.T) {
	tests := []struct {
		name        string
		source      control.TargetPickerRequestSource
		pendingKind targetPickerPendingKind
		cancelKind  control.DaemonCommandKind
	}{
		{
			name:        "git import",
			source:      control.TargetPickerRequestSourceGit,
			pendingKind: targetPickerPendingGitImport,
			cancelKind:  control.DaemonCommandGitWorkspaceImportCancel,
		},
		{
			name:        "worktree create",
			source:      control.TargetPickerRequestSourceWorktree,
			pendingKind: targetPickerPendingWorktreeCreate,
			cancelKind:  control.DaemonCommandGitWorkspaceWorktreeCancel,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 4, 14, 16, 1, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			surface := svc.ensureSurface(control.Action{
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
			})
			surface.PendingHeadless = &state.HeadlessLaunchRecord{
				InstanceID:       "inst-pending",
				WorkspaceKey:     "/data/dl/projects/repo",
				ThreadCWD:        "/data/dl/projects/repo-login",
				PrepareNewThread: true,
			}
			record := &activeTargetPickerRecord{
				PickerID:             "picker-1",
				OwnerUserID:          "user-1",
				Source:               tc.source,
				Stage:                control.FeishuTargetPickerStageProcessing,
				PendingKind:          tc.pendingKind,
				PendingWorkspaceKey:  "/data/dl/projects/repo-login",
				SelectedWorkspaceKey: "/data/dl/projects/repo",
			}
			svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
			svc.setActiveTargetPicker(surface, record)

			events := svc.ApplySurfaceAction(control.Action{
				Kind:             control.ActionTargetPickerCancel,
				SurfaceSessionID: "surface-1",
				ChatID:           "chat-1",
				ActorUserID:      "user-1",
				PickerID:         record.PickerID,
			})

			var sawCancel bool
			var sawKill bool
			for _, event := range events {
				if event.DaemonCommand == nil {
					continue
				}
				switch event.DaemonCommand.Kind {
				case tc.cancelKind:
					sawCancel = true
				case control.DaemonCommandKillHeadless:
					sawKill = event.DaemonCommand.InstanceID == "inst-pending"
				}
			}
			if !sawCancel || !sawKill {
				t.Fatalf("expected cancel and kill commands for resolved pending workspace, got %#v", events)
			}
			if surface.PendingHeadless != nil {
				t.Fatalf("expected matching pending headless to be consumed on cancel, got %#v", surface.PendingHeadless)
			}
		})
	}
}
