package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestCompleteTargetPickerGitImportEntersNewThreadReadyAndClearsPicker(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-web",
		DisplayName:   "web",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "web",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	record := &activeTargetPickerRecord{
		PickerID:    "picker-1",
		OwnerUserID: "user-1",
		Source:      control.TargetPickerRequestSourceGit,
		Stage:       control.FeishuTargetPickerStageProcessing,
		PendingKind: targetPickerPendingGitImport,
	}
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
	svc.setActiveTargetPicker(surface, record)

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)

	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected git import completion to enter new-thread-ready, got %#v", surface)
	}
	if picker := svc.activeTargetPicker(surface); picker != nil {
		t.Fatalf("expected successful git import completion to clear target picker, got %#v", picker)
	}
	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected same-card success after git import completion, got %#v", events)
	}
	got := events[0].TargetPickerView
	if got.Stage != control.FeishuTargetPickerStageSucceeded || got.StatusTitle != "已进入新会话待命" {
		t.Fatalf("expected succeeded git-import terminal card, got %#v", got)
	}
}

func TestCompleteTargetPickerGitImportReportsStaleFlowAndKeepsDirectory(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()

	svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "git_import_flow_stale" {
		t.Fatalf("expected stale-flow notice, got %#v", events)
	}
	if got := events[0].Notice.Text; got == "" || !containsAll(got, normalizeWorkspaceClaimKey(workspaceRoot), "目录会保留") {
		t.Fatalf("expected stale-flow notice to mention kept directory, got %#v", events[0].Notice)
	}
}

func TestCompleteTargetPickerGitImportAttachFailureMentionsDirectoryPreserved(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-owner",
		DisplayName:   "owner",
		WorkspaceRoot: workspaceRoot,
		WorkspaceKey:  workspaceRoot,
		ShortName:     "owner",
		Online:        true,
		Threads:       map[string]*state.ThreadRecord{},
	})
	ownerSurface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-owner",
		ChatID:           "chat-owner",
		ActorUserID:      "user-owner",
	})
	svc.attachWorkspace(ownerSurface, workspaceRoot)

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	record := &activeTargetPickerRecord{
		PickerID:    "picker-1",
		OwnerUserID: "user-1",
		Source:      control.TargetPickerRequestSourceGit,
		Stage:       control.FeishuTargetPickerStageProcessing,
		PendingKind: targetPickerPendingGitImport,
	}
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
	svc.setActiveTargetPicker(surface, record)

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)

	if len(events) != 1 || events[0].TargetPickerView == nil {
		t.Fatalf("expected failed same-card result on attach failure, got %#v", events)
	}
	got := events[0].TargetPickerView
	if got.Stage != control.FeishuTargetPickerStageFailed || got.StatusTitle != "导入失败" {
		t.Fatalf("expected failed git-import terminal card, got %#v", got)
	}
	var combined []string
	for _, section := range got.StatusSections {
		combined = append(combined, section.Label)
		combined = append(combined, section.Lines...)
	}
	if !containsAll(strings.Join(combined, "\n"), normalizeWorkspaceClaimKey(workspaceRoot), "目录已保留") {
		t.Fatalf("expected failed card to mention preserved directory, got %#v", got)
	}
}

func TestCompleteTargetPickerGitImportStartsFreshWorkspacePreparationOnSameCard(t *testing.T) {
	now := time.Date(2026, 4, 16, 18, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	workspaceRoot := t.TempDir()

	surface := svc.ensureSurface(control.Action{
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
	})
	record := &activeTargetPickerRecord{
		PickerID:    "picker-1",
		OwnerUserID: "user-1",
		Source:      control.TargetPickerRequestSourceGit,
		Stage:       control.FeishuTargetPickerStageProcessing,
		PendingKind: targetPickerPendingGitImport,
		GitRepoURL:  "https://github.com/kxn/codex-remote-feishu.git",
	}
	svc.setActiveOwnerCardFlow(surface, newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning))
	svc.setActiveTargetPicker(surface, record)

	events := svc.CompleteTargetPickerGitImport("surface-1", "picker-1", workspaceRoot)

	if surface.PendingHeadless == nil || !surface.PendingHeadless.PrepareNewThread || !testutil.SamePath(surface.PendingHeadless.ThreadCWD, workspaceRoot) {
		t.Fatalf("expected git import completion to continue into fresh-workspace preparation, got %#v", surface.PendingHeadless)
	}
	if len(events) != 2 || events[0].TargetPickerView == nil || events[1].DaemonCommand == nil {
		t.Fatalf("expected processing card plus headless start command, got %#v", events)
	}
	if got := events[0].TargetPickerView; got.Stage != control.FeishuTargetPickerStageProcessing || got.StatusTitle != "正在接入工作区" {
		t.Fatalf("expected processing same-card transition after clone, got %#v", got)
	} else if got.StatusFooter != "" {
		t.Fatalf("expected git-import processing card to drop trailing footer, got %#v", got)
	}
	if got := events[1].DaemonCommand; got.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected headless start command after clone completion, got %#v", got)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !strings.Contains(text, part) {
			return false
		}
	}
	return true
}
