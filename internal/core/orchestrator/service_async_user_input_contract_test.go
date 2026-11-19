package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestAsyncUserInputRejectsPendingProfileRefreshWithoutLosingDraft(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:user-1", "app-1", "chat-1", "user-1", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeWithCodexProfile("feishu:app-1:user:user-2", "app-1", "chat-2", "user-2", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	workspace := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1", Online: true, Source: "headless", Managed: true, Backend: agentproto.BackendCodex, CodexProfileID: "default", WorkspaceRoot: workspace, WorkspaceKey: workspace,
		Threads: map[string]*state.ThreadRecord{"thread-1": {ThreadID: "thread-1", CWD: workspace, Loaded: true}},
	})
	inst := svc.root.Instances["inst-1"]
	surface := svc.root.Surfaces["feishu:app-1:user:user-1"]
	surface.AttachedInstanceID, surface.SelectedThreadID, surface.ClaimedWorkspaceKey, surface.RouteMode = "inst-1", "thread-1", workspace, state.RouteModePinned
	svc.claimThread(surface, inst, "thread-1")
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionTextMessage, SurfaceSessionID: surface.SurfaceSessionID, ActorUserID: "user-1", MessageID: "initial", Text: "请继续"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{Kind: agentproto.EventTurnStarted, ThreadID: "thread-1", TurnID: "turn-1"})
	page := asyncPage(t, svc.ApplyAgentEvent("inst-1", asyncQuestionEvent("item-1")))
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionCodexProfileCommand, SurfaceSessionID: "feishu:app-1:user:user-2", GatewayID: "app-1", ChatID: "chat-2", ActorUserID: "user-2", Text: "/codexprofile team-proxy"})
	if !surface.ContractRefreshPending || svc.surfaceInstanceCompatibleForAttach(surface, inst) {
		t.Fatal("fixture must keep the busy instance on the previous profile")
	}
	before := len(surface.QueueItems)
	action := asyncAnswerAction(page, 0)
	action.SurfaceSessionID = surface.SurfaceSessionID
	events := svc.ApplySurfaceAction(action)
	if containsAsyncCommand(events) || len(surface.QueueItems) != before {
		t.Fatal("answer must not dispatch to the previous profile while refresh is pending")
	}
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "contract_refresh_pending" {
		t.Fatalf("expected pending profile notice, got %#v", events)
	}
	// Restoring compatibility must leave this same question revision answerable.
	inst.CodexProfileID = "team-proxy"
	if !containsAsyncCommand(svc.ApplySurfaceAction(action)) {
		t.Fatal("blocked submission must retain its draft for retry")
	}
}
