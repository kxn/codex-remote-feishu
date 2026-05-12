package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestDetachedBranchTurnKeepsSurfaceSelectionAndDefaultAttachThread(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-main",
		Threads: map[string]*state.ThreadRecord{
			"thread-main": {ThreadID: "thread-main", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ThreadID: "thread-main"})
	surface := svc.root.Surfaces["surface-1"]

	started := startDetachedBranchRemoteTurnForTest(t, svc, surface, "thread-main", "thread-detour", "msg-1", "顺手问个岔题", "turn-detour")
	bindingPlan := remoteBindingPromptDispatchPlan(svc.turns.activeRemote["inst-1"])
	if binding := svc.turns.activeRemote["inst-1"]; binding == nil ||
		binding.SurfaceSessionID != "surface-1" ||
		binding.TurnID != "turn-detour" ||
		binding.ThreadID != "thread-detour" ||
		bindingPlan.SourceThreadID != "thread-main" ||
		bindingPlan.SurfaceBindingPolicy != agentproto.SurfaceBindingPolicyKeepSurfaceSelection {
		t.Fatalf("expected detached branch binding with source/execution split, got %#v", binding)
	}
	for _, event := range started {
		if event.ThreadSelection != nil {
			t.Fatalf("expected detached branch start not to emit thread selection change, got %#v", started)
		}
	}
	if surface.SelectedThreadID != "thread-main" || surface.RouteMode != state.RouteModePinned {
		t.Fatalf("expected surface to keep main selection, got thread=%q route=%q", surface.SelectedThreadID, surface.RouteMode)
	}
	if inst := svc.root.Instances["inst-1"]; inst.ActiveTurnID != "" || inst.ActiveThreadID != "" {
		t.Fatalf("expected detached branch not to pollute tracked active turn, got thread=%q turn=%q", inst.ActiveThreadID, inst.ActiveTurnID)
	}

	dynamic := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventItemCompleted,
		ThreadID:  "thread-detour",
		TurnID:    "turn-detour",
		ItemID:    "tool-1",
		ItemKind:  "dynamic_tool_call",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		Metadata:  map[string]any{"tool": "notes", "text": "补充说明"},
	})
	for _, event := range dynamic {
		if event.ThreadSelection != nil {
			t.Fatalf("expected detached branch dynamic tool not to retarget selection, got %#v", dynamic)
		}
	}
	if surface.SelectedThreadID != "thread-main" {
		t.Fatalf("expected detached branch dynamic tool to keep main selection, got %q", surface.SelectedThreadID)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-detour",
		TurnID:    "turn-detour",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})
	for _, event := range finished {
		if event.ThreadSelection != nil {
			t.Fatalf("expected detached branch completion not to retarget selection, got %#v", finished)
		}
	}
	if got := svc.defaultAttachThread(svc.root.Instances["inst-1"]); got != "thread-main" {
		t.Fatalf("expected default attach target to remain main thread, got %q", got)
	}
}
