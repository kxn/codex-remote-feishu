package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestProfileSwitchReconcilesOtherGatewayHeadlessSurfaces(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeSurfaceResumeWithCodexProvider("feishu:app-1:user:ou_a", "app-1", "ou_a", "ou_a", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeWithCodexProvider("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	workspaceKey := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-b",
		DisplayName:     "repo",
		WorkspaceRoot:   workspaceKey,
		WorkspaceKey:    workspaceKey,
		ShortName:       "repo",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey, Loaded: true},
		},
	})
	surfaceB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	surfaceB.AttachedInstanceID = "inst-b"
	surfaceB.ClaimedWorkspaceKey = workspaceKey
	surfaceB.SelectedThreadID = "thread-1"
	surfaceB.RouteMode = state.RouteModePinned
	surfaceB.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "修复登录流程",
	}
	if !svc.claimKnownThread(surfaceB, svc.root.Instances["inst-b"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_a",
		GatewayID:        "app-1",
		ChatID:           "ou_a",
		ActorUserID:      "ou_a",
		Text:             "/codexprofile team-proxy",
	})

	foundKill := false
	foundStart := false
	for _, event := range events {
		if event.DaemonCommand == nil {
			continue
		}
		switch event.DaemonCommand.Kind {
		case control.DaemonCommandKillHeadless:
			if event.DaemonCommand.InstanceID == "inst-b" {
				foundKill = true
			}
		case control.DaemonCommandStartHeadless:
			if event.DaemonCommand.SurfaceSessionID == surfaceB.SurfaceSessionID &&
				event.DaemonCommand.CodexProviderID == "team-proxy" {
				foundStart = true
			}
		}
	}
	if !foundKill || !foundStart {
		t.Fatalf("expected other gateway surface to restart under new provider, kill=%t start=%t events=%#v", foundKill, foundStart, events)
	}
	if surfaceB.PendingHeadless == nil || surfaceB.PendingHeadless.CodexProviderID != "team-proxy" {
		t.Fatalf("expected pending headless on other surface with new provider, got %#v", surfaceB.PendingHeadless)
	}
}

func TestProfileSwitchDefersBusySurfaceContractRefresh(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeSurfaceResumeWithCodexProvider("feishu:app-1:user:ou_a", "app-1", "ou_a", "ou_a", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)
	svc.MaterializeSurfaceResumeWithCodexProvider("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.ProductModeNormal, agentproto.BackendCodex, "default", "", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	workspaceKey := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-b",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		WorkspaceKey:    workspaceKey,
	})
	surfaceB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	surfaceB.AttachedInstanceID = "inst-b"
	surfaceB.ClaimedWorkspaceKey = workspaceKey
	surfaceB.ActiveQueueItemID = "q1"
	surfaceB.QueueItems = map[string]*state.QueueItemRecord{
		"q1": {ID: "q1", Status: state.QueueItemRunning},
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionCodexProviderCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_a",
		GatewayID:        "app-1",
		ChatID:           "ou_a",
		ActorUserID:      "ou_a",
		Text:             "/codexprofile team-proxy",
	})

	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandKillHeadless && event.DaemonCommand.InstanceID == "inst-b" {
			t.Fatalf("busy surface must not be force-killed: %#v", events)
		}
	}
	if !surfaceB.ContractRefreshPending {
		t.Fatal("expected busy surface to be marked for deferred contract refresh")
	}
}

func TestHandleTextConvergesPendingContractRefresh(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	materializeTestCodexProfiles(svc, state.CodexProfileSummary{ID: "team-proxy", Name: "Team Proxy"})
	svc.MaterializeSurfaceResumeWithCodexProvider("feishu:app-1:user:ou_b", "app-1", "ou_b", "ou_b", state.ProductModeNormal, agentproto.BackendCodex, "team-proxy", "team-proxy", state.SurfaceVerbosityNormal, state.PlanModeSettingOff)

	workspaceKey := t.TempDir()
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-b",
		Backend:         agentproto.BackendCodex,
		CodexProviderID: "default",
		Source:          "headless",
		Managed:         true,
		Online:          true,
		WorkspaceKey:    workspaceKey,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: workspaceKey, Loaded: true},
		},
	})
	surfaceB := svc.root.Surfaces["feishu:app-1:user:ou_b"]
	surfaceB.AttachedInstanceID = "inst-b"
	surfaceB.ClaimedWorkspaceKey = workspaceKey
	surfaceB.SelectedThreadID = "thread-1"
	surfaceB.RouteMode = state.RouteModePinned
	surfaceB.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "thread-1",
		RouteMode: string(state.RouteModePinned),
		Title:     "修复登录流程",
	}
	surfaceB.ContractRefreshPending = true
	if !svc.claimKnownThread(surfaceB, svc.root.Instances["inst-b"], "thread-1") {
		t.Fatal("expected test setup to claim thread")
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "feishu:app-1:user:ou_b",
		GatewayID:        "app-1",
		ChatID:           "ou_b",
		ActorUserID:      "ou_b",
		MessageID:        "om-b-1",
		Text:             "继续",
	})
	if surfaceB.PendingTextInput == nil || surfaceB.PendingTextInput.Text != "继续" {
		t.Fatalf("expected pending text input to be saved for replay, got %#v", surfaceB.PendingTextInput)
	}
	if surfaceB.PendingHeadless == nil || surfaceB.PendingHeadless.CodexProviderID != "team-proxy" {
		t.Fatalf("expected pending contract refresh to start headless with new provider, got %#v", surfaceB.PendingHeadless)
	}
	foundStart := false
	for _, event := range events {
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless &&
			event.DaemonCommand.SurfaceSessionID == surfaceB.SurfaceSessionID &&
			event.DaemonCommand.CodexProviderID == "team-proxy" {
			foundStart = true
		}
	}
	if !foundStart {
		t.Fatalf("expected text action to trigger contract refresh start, events=%#v", events)
	}
	if strings.TrimSpace(surfaceB.CodexProviderID) != "team-proxy" {
		t.Fatalf("expected surface provider to remain team-proxy, got %q", surfaceB.CodexProviderID)
	}
}
