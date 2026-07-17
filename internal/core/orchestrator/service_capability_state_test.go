package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
