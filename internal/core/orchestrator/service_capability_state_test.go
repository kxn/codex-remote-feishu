package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCapabilityStateStoresStateOnlyWithoutUIEvents(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventCapabilityStateUpdated,
		ThreadID: "thread-1",
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method: "mcpServer/startupStatus/updated",
			MCPServerStartupStatus: &agentproto.MCPServerStartupStatus{
				ThreadID:      "thread-1",
				Name:          "docs",
				Status:        "failed",
				Error:         "login required",
				FailureReason: "reauthenticationRequired",
			},
		},
	}); len(events) != 0 {
		t.Fatalf("expected capability state to remain state-only, got %#v", events)
	}

	thread := svc.root.Instances["inst-1"].Threads["thread-1"]
	if thread.LastCapabilityState == nil || thread.LastCapabilityState.MCPServerStartupStatus == nil {
		t.Fatalf("expected thread capability state, got %#v", thread)
	}
	if got := thread.LastCapabilityState.MCPServerStartupStatus.FailureReason; got != "reauthenticationRequired" {
		t.Fatalf("expected failure reason to be retained, got %q", got)
	}

	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind: agentproto.EventCapabilityStateUpdated,
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method:        "skills/changed",
			SkillsChanged: true,
		},
	}); len(events) != 0 {
		t.Fatalf("expected skills invalidation to remain state-only, got %#v", events)
	}

	inst := svc.root.Instances["inst-1"]
	if inst.LastCapabilityState == nil || !inst.LastCapabilityState.SkillsChanged {
		t.Fatalf("expected instance capability state, got %#v", inst.LastCapabilityState)
	}
}

func TestCapabilityStateProjectsMCPStartupFailureToAffectedSurface(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{ThreadID: "thread-1", InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventCapabilityStateUpdated,
		ThreadID: "thread-1",
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method: "mcpServer/startupStatus/updated",
			MCPServerStartupStatus: &agentproto.MCPServerStartupStatus{
				ThreadID:      "thread-1",
				Name:          "docs",
				Status:        "failed",
				Error:         "login required",
				FailureReason: "reauthenticationRequired",
			},
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected one MCP startup notice, got %#v", events)
	}
	event := events[0].Normalized()
	if event.Kind != eventcontract.KindNotice || event.SurfaceSessionID != "surface-1" {
		t.Fatalf("expected notice for affected surface, got %#v", event)
	}
	if event.Notice == nil || event.Notice.Code != "codex_mcp_server_reauth_required" {
		t.Fatalf("unexpected MCP notice: %#v", event.Notice)
	}
	if !strings.Contains(event.Notice.Text, "docs") || !strings.Contains(event.Notice.Text, "重新授权") {
		t.Fatalf("expected server name and reauth hint, got %q", event.Notice.Text)
	}
}

func TestCapabilityStateMCPStartupCooldownIncludesFailureReasonAndResetsOnRecovery(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 8, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{ThreadID: "thread-1", InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	base := agentproto.Event{
		Kind:     agentproto.EventCapabilityStateUpdated,
		ThreadID: "thread-1",
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method: "mcpServer/startupStatus/updated",
			MCPServerStartupStatus: &agentproto.MCPServerStartupStatus{
				ThreadID:      "thread-1",
				Name:          "docs",
				Status:        "failed",
				Error:         "login required",
				FailureReason: "reauthenticationRequired",
			},
		},
	}
	if events := svc.ApplyAgentEvent("inst-1", base); len(events) != 1 {
		t.Fatalf("expected first MCP startup notice, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", base); len(events) != 0 {
		t.Fatalf("expected same failure reason to cool down, got %#v", events)
	}

	otherReason := base
	otherReason.CapabilityState = &agentproto.CapabilityStateUpdate{
		Method: "mcpServer/startupStatus/updated",
		MCPServerStartupStatus: &agentproto.MCPServerStartupStatus{
			ThreadID:      "thread-1",
			Name:          "docs",
			Status:        "failed",
			Error:         "server crashed",
			FailureReason: "processExited",
		},
	}
	if events := svc.ApplyAgentEvent("inst-1", otherReason); len(events) != 1 {
		t.Fatalf("expected different failure reason to bypass cooldown, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", otherReason); len(events) != 0 {
		t.Fatalf("expected same alternate failure reason to cool down, got %#v", events)
	}

	recovered := base
	recovered.CapabilityState = &agentproto.CapabilityStateUpdate{
		Method: "mcpServer/startupStatus/updated",
		MCPServerStartupStatus: &agentproto.MCPServerStartupStatus{
			ThreadID: "thread-1",
			Name:     "docs",
			Status:   "running",
		},
	}
	if events := svc.ApplyAgentEvent("inst-1", recovered); len(events) != 0 {
		t.Fatalf("expected recovery to stay state-only, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", base); len(events) != 1 {
		t.Fatalf("expected recovery to reset failure cooldown, got %#v", events)
	}
	if events := svc.ApplyAgentEvent("inst-1", otherReason); len(events) != 1 {
		t.Fatalf("expected recovery to reset alternate failure cooldown, got %#v", events)
	}
}

func TestCapabilityStateDoesNotProjectLowValueState(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind: agentproto.EventCapabilityStateUpdated,
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method:        "skills/changed",
			SkillsChanged: true,
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected skills changed to remain state-only, got %#v", events)
	}
}

func TestCapabilityStateProjectsPassiveOAuthAndAccountLoginFailures(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})
	svc.root.Surfaces["surface-1"] = &state.SurfaceConsoleRecord{
		SurfaceSessionID:   "surface-1",
		GatewayID:          "gateway-1",
		AttachedInstanceID: "inst-1",
		SelectedThreadID:   "thread-1",
		RouteMode:          state.RouteModePinned,
	}
	svc.threadClaims["thread-1"] = &threadClaimRecord{ThreadID: "thread-1", InstanceID: "inst-1", SurfaceSessionID: "surface-1"}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventCapabilityStateUpdated,
		ThreadID: "thread-1",
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method: "mcpServer/oauthLogin/completed",
			MCPOAuthLoginCompleted: &agentproto.MCPOAuthLoginCompletionState{
				Name:     "docs",
				ThreadID: "thread-1",
				Success:  false,
				Error:    "callback timed out",
			},
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_mcp_oauth_login_failed" {
		t.Fatalf("expected passive MCP OAuth failure notice, got %#v", events)
	}

	events = svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventCapabilityStateUpdated,
		ThreadID: "thread-1",
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method:   "account/login/completed",
			ThreadID: "thread-1",
			AccountLoginCompleted: &agentproto.AccountLoginCompletionState{
				LoginID: "login-1",
				Success: false,
				Error:   "browser flow expired",
			},
		},
	})
	if len(events) != 1 || events[0].Notice == nil || events[0].Notice.Code != "codex_account_login_failed" {
		t.Fatalf("expected account login failure notice, got %#v", events)
	}
}

func TestCapabilityStateDoesNotProjectLoginFailureWithoutSurface(t *testing.T) {
	now := time.Date(2026, 7, 17, 18, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID: "inst-1",
		Online:     true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Loaded: true},
		},
	})

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventCapabilityStateUpdated,
		ThreadID: "thread-1",
		CapabilityState: &agentproto.CapabilityStateUpdate{
			Method:   "account/login/completed",
			ThreadID: "thread-1",
			AccountLoginCompleted: &agentproto.AccountLoginCompletionState{
				LoginID: "login-1",
				Error:   "browser flow expired",
			},
		},
	})
	if len(events) != 0 {
		t.Fatalf("expected no login failure notice without affected surface, got %#v", events)
	}
}
