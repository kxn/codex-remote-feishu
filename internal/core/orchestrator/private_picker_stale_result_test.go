package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"testing"
	"time"
)

func TestTargetPickerStaleReadyCannotOverrideCurrentFailure(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	cwd := t.TempDir()
	surface := svc.ensureSurface(control.Action{SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1"})
	surface.AttachedInstanceID = "missing"
	surface.ClaimedWorkspaceKey = cwd
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = cwd
	svc.UpsertInstance(&state.InstanceRecord{InstanceID: "missing", WorkspaceRoot: cwd, WorkspaceKey: cwd, Online: true})
	surface.ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "pending"}
	record := &activeTargetPickerRecord{PickerID: "picker-1", OwnerUserID: "user-1"}
	flow := newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, record.PickerID, "user-1", now, time.Minute, ownerCardFlowPhaseRunning)
	svc.setActiveOwnerCardFlow(surface, flow)
	svc.setActiveTargetPicker(surface, record)
	events := svc.dispatchTargetPickerConfirmed(surface, flow, record, control.FeishuTargetPickerView{SelectedWorkspaceKey: cwd, SelectedSessionValue: targetPickerNewThreadValue})
	if len(events) == 0 || events[0].TargetPickerView == nil || events[0].TargetPickerView.Stage != control.FeishuTargetPickerStageFailed {
		t.Fatalf("stale ready became success: %+v", events)
	}
}

func TestPendingTargetPickerNeverFinalizesStaleReady(t *testing.T) {
	for _, kind := range []targetPickerPendingKind{targetPickerPendingNewThread, targetPickerPendingUseThread, targetPickerPendingGitImport, targetPickerPendingWorktreeCreate} {
		t.Run(string(kind), func(t *testing.T) {
			now := time.Now()
			svc := newServiceForTest(&now)
			cwd := t.TempDir()
			surface := svc.ensureSurface(control.Action{SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"})
			surface.RouteMode = state.RouteModeNewThreadReady
			surface.PreparedThreadCWD = cwd
			surface.AttachedInstanceID = "stale"
			surface.SelectedThreadID = "thread"
			surface.PendingHeadless = &state.HeadlessLaunchRecord{InstanceID: "new", WorkspaceKey: cwd, ThreadID: "thread", PrepareNewThread: true}
			record := &activeTargetPickerRecord{PickerID: "p", OwnerUserID: "u", Stage: control.FeishuTargetPickerStageProcessing, PendingKind: kind, PendingWorkspaceKey: cwd, PendingThreadID: "thread"}
			flow := newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, "p", "u", now, time.Minute, ownerCardFlowPhaseRunning)
			svc.setActiveOwnerCardFlow(surface, flow)
			svc.setActiveTargetPicker(surface, record)
			events := svc.maybeFinalizePendingTargetPicker(surface, nil, "")
			if len(events) != 0 || svc.activeTargetPicker(surface) == nil || record.Stage != control.FeishuTargetPickerStageProcessing {
				t.Fatalf("pending became terminal: %+v", events)
			}
			events = svc.maybeFinalizePendingTargetPicker(surface, notice(surface, "not_attached", "startup failed"), "")
			if len(events) == 0 || events[0].TargetPickerView == nil || events[0].TargetPickerView.Stage != control.FeishuTargetPickerStageFailed {
				t.Fatalf("failure not surfaced: %+v", events)
			}
		})
	}
}

func TestTargetPickerWorkspaceCompletionPreservesFailureAgainstStaleRoute(t *testing.T) {
	for _, kind := range []string{"local", "git", "worktree"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			svc := newServiceForTest(&now)
			cwd := t.TempDir()
			surface := svc.ensureSurface(control.Action{SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"})
			surface.AttachedInstanceID = "missing"
			surface.ClaimedWorkspaceKey = cwd
			surface.RouteMode = state.RouteModeNewThreadReady
			surface.PreparedThreadCWD = cwd
			svc.UpsertInstance(&state.InstanceRecord{InstanceID: "missing", WorkspaceRoot: cwd, WorkspaceKey: cwd, Online: true})
			surface.ActiveRequestCapture = &state.RequestCaptureRecord{RequestID: "pending"}
			record := &activeTargetPickerRecord{PickerID: "p", OwnerUserID: "u", Stage: control.FeishuTargetPickerStageProcessing}
			flow := newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, "p", "u", now, time.Minute, ownerCardFlowPhaseRunning)
			svc.setActiveOwnerCardFlow(surface, flow)
			svc.setActiveTargetPicker(surface, record)
			var events []eventcontract.Event
			switch kind {
			case "local":
				events = svc.finishPrivateLocalDirectory(surface, flow, record, cwd)
			case "git":
				events = svc.CompleteTargetPickerGitImport("s", "p", cwd)
			case "worktree":
				events = svc.CompleteTargetPickerWorktreeCreate("s", "p", cwd)
			}
			if len(events) == 0 || events[0].TargetPickerView == nil || events[0].TargetPickerView.Stage != control.FeishuTargetPickerStageFailed {
				t.Fatalf("stale route hid failure: %+v", events)
			}
		})
	}
}

func TestTargetPickerStaleSameWorkspaceStartsFreshCarrier(t *testing.T) {
	now := time.Now()
	svc := newServiceForTest(&now)
	cwd := t.TempDir()
	surface := svc.ensureSurface(control.Action{SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u"})
	surface.AttachedInstanceID = "retired"
	surface.ClaimedWorkspaceKey = cwd
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = cwd
	record := &activeTargetPickerRecord{PickerID: "p", OwnerUserID: "u"}
	flow := newOwnerCardFlowRecord(ownerCardFlowKindTargetPicker, "p", "u", now, time.Minute, ownerCardFlowPhaseRunning)
	svc.setActiveOwnerCardFlow(surface, flow)
	svc.setActiveTargetPicker(surface, record)
	events := svc.dispatchTargetPickerConfirmed(surface, flow, record, control.FeishuTargetPickerView{SelectedWorkspaceKey: cwd, SelectedSessionValue: targetPickerNewThreadValue})
	if surface.PendingHeadless == nil || !targetPickerStartedPending(surface, events) || len(events) == 0 || events[0].TargetPickerView == nil || events[0].TargetPickerView.Stage != control.FeishuTargetPickerStageProcessing {
		t.Fatalf("fresh carrier not started: pending=%+v events=%+v", surface.PendingHeadless, events)
	}
}

func TestPrivateReplacementAllowsPickerNavigationButNotRetargetWhilePending(t *testing.T) {
	svc, req := privateUpgradeResumeFixture(t, true)
	svc.config.PrivateApplication = true
	surface := svc.Surface("s")
	surface.PendingHeadless = &state.HeadlessLaunchRecord{Purpose: state.HeadlessLaunchPurposePrivateUpgrade, AutoRestore: true, SourceInstanceID: surface.AttachedInstanceID, InstanceID: "new", ThreadID: surface.SelectedThreadID, ThreadCWD: req.CWD}
	for _, kind := range []control.ActionKind{control.ActionListInstances, control.ActionWorkspaceList, control.ActionTargetPickerSelectWorkspace, control.ActionTargetPickerSelectSession, control.ActionTargetPickerPage, control.ActionTargetPickerCancel} {
		if events := svc.pendingHeadlessActionBlocked(surface, control.Action{Kind: kind, SurfaceSessionID: "s"}); events != nil {
			t.Fatalf("navigation %s blocked: %+v", kind, events)
		}
	}
	if events := svc.pendingHeadlessActionBlocked(surface, control.Action{Kind: control.ActionTargetPickerConfirm, SurfaceSessionID: "s"}); len(events) == 0 {
		t.Fatal("pending replacement permitted route confirmation")
	}
}
