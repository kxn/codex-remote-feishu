package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/testutil"
)

func TestOpenCodeFreshWorkspacePendingConnectPreservesContract(t *testing.T) {
	now := time.Date(2026, 8, 9, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", "")
	surface := svc.root.Surfaces["surface-1"]
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}
	workspaceRoot := t.TempDir()

	startEvents := svc.startFreshWorkspaceHeadless(surface, workspaceRoot)
	if len(startEvents) != 2 || startEvents[1].DaemonCommand == nil || startEvents[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected fresh workspace start, got %#v", startEvents)
	}
	pending := surface.PendingHeadless
	if pending == nil || pending.Backend != agentproto.BackendOpenCode || pending.OpenCodeProfileID != "op_team" {
		t.Fatalf("expected opencode pending launch, got %#v", pending)
	}

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:           pending.InstanceID,
		DisplayName:          "headless",
		WorkspaceRoot:        workspaceRoot,
		WorkspaceKey:         workspaceRoot,
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    "op_team",
		OpenCodeAdmissionRef: state.NormalizeOpenCodeAdmissionRef(pending.OpenCodeAdmissionRef),
		Source:               "headless",
		Managed:              true,
		Online:               true,
		Threads:              map[string]*state.ThreadRecord{},
	})
	svc.ApplyInstanceConnected(pending.InstanceID)

	if surface.Backend != agentproto.BackendOpenCode || svc.SurfaceOpenCodeProfileID("surface-1") != "op_team" {
		t.Fatalf("expected connected fresh workspace to stay on opencode/op_team, got %#v", surface)
	}
	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected connected fresh workspace to keep opencode admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}
	if snapshot := svc.SurfaceSnapshot("surface-1"); snapshot == nil ||
		snapshot.Backend != agentproto.BackendOpenCode ||
		!testutil.SamePath(snapshot.WorkspaceKey, workspaceRoot) {
		t.Fatalf("expected opencode fresh workspace snapshot, got %#v", snapshot)
	}
}

func TestOpenCodeWorkspaceRouteRestartPendingConnectPreservesContract(t *testing.T) {
	now := time.Date(2026, 8, 9, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurfaceResumeContract("surface-1", "app-1", "chat-1", "user-1", state.HeadlessOpenCodeSurfaceBackendContract("op_team"), "", "")
	surface := svc.root.Surfaces["surface-1"]
	surface.OpenCodeAdmissionRef = &state.OpenCodeAdmissionRef{ProfileRef: state.OpenCodeProfileRef{ID: "op_team", Revision: 7}}
	workspaceRoot := t.TempDir()

	continuation := sHeadlessWorkspaceRouteRestartForTest(svc, surface, workspaceRoot)
	events := svc.restartHeadlessContractContinuation(surface, continuation)
	if len(events) != 2 || events[1].DaemonCommand == nil || events[1].DaemonCommand.Kind != control.DaemonCommandStartHeadless {
		t.Fatalf("expected route restart start, got %#v", events)
	}
	pending := surface.PendingHeadless
	if pending == nil || pending.Purpose != state.HeadlessLaunchPurposeWorkspaceRouteRestart ||
		pending.Backend != agentproto.BackendOpenCode ||
		pending.OpenCodeProfileID != "op_team" ||
		pending.OpenCodeAdmissionRef == nil ||
		pending.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected opencode pending route restart, got %#v", pending)
	}

	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:           pending.InstanceID,
		DisplayName:          "headless",
		WorkspaceRoot:        workspaceRoot,
		WorkspaceKey:         workspaceRoot,
		Backend:              agentproto.BackendOpenCode,
		OpenCodeProfileID:    "op_team",
		OpenCodeAdmissionRef: state.NormalizeOpenCodeAdmissionRef(pending.OpenCodeAdmissionRef),
		Source:               "headless",
		Managed:              true,
		Online:               true,
		Threads:              map[string]*state.ThreadRecord{},
	})
	svc.ApplyInstanceConnected(pending.InstanceID)

	if surface.Backend != agentproto.BackendOpenCode || svc.SurfaceOpenCodeProfileID("surface-1") != "op_team" {
		t.Fatalf("expected connected route restart to stay on opencode/op_team, got %#v", surface)
	}
	if surface.OpenCodeAdmissionRef == nil || surface.OpenCodeAdmissionRef.ProfileRef.Revision != 7 {
		t.Fatalf("expected connected route restart to keep opencode admission ref, got %#v", surface.OpenCodeAdmissionRef)
	}
	if surface.RouteMode != state.RouteModeNewThreadReady || !testutil.SamePath(surface.PreparedThreadCWD, workspaceRoot) {
		t.Fatalf("expected connected route restart to keep new-thread-ready workspace, got %#v", surface)
	}
}

func sHeadlessWorkspaceRouteRestartForTest(svc *Service, surface *state.SurfaceConsoleRecord, workspaceRoot string) headlessContractSwitchContinuation {
	surface.ClaimedWorkspaceKey = workspaceRoot
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = workspaceRoot
	return svc.buildHeadlessWorkspaceRouteRestartContinuation(surface, workspaceRoot, agentproto.BackendOpenCode, true)
}
