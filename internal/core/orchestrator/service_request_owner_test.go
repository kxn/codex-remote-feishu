package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestResolveRequestPromptUsesFrozenOwnerSurface(t *testing.T) {
	now := time.Date(2026, 5, 8, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-2", GatewayID: "app-1", ChatID: "chat-2", ActorUserID: "user-2", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorRemoteSurface, SurfaceSessionID: "surface-1"},
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "需要确认",
		},
	})
	if len(started) != 1 || started[0].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected request to render on owner surface, got %#v", started)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]
	if record == nil || record.OwnerSurfaceSessionID != "surface-1" || record.OwnerChatID != "chat-1" {
		t.Fatalf("expected request owner to freeze on surface-1, got %#v", record)
	}

	svc.root.Surfaces["surface-2"].PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:             "req-1",
		RequestType:           "approval",
		InstanceID:            "inst-1",
		ThreadID:              "thread-1",
		TurnID:                "turn-1",
		OwnerSurfaceSessionID: "surface-1",
	}
	svc.root.Surfaces["surface-2"].PendingRequestOrder = []string{"req-1"}

	resolved := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
	})
	if len(resolved) != 0 {
		t.Fatalf("expected no activation event when only one request existed, got %#v", resolved)
	}
	if _, ok := svc.root.Surfaces["surface-1"].PendingRequests["req-1"]; ok {
		t.Fatalf("expected owner surface request to be resolved")
	}
	if _, ok := svc.root.Surfaces["surface-2"].PendingRequests["req-1"]; !ok {
		t.Fatalf("expected non-owner surface stray request to remain untouched")
	}
}

func TestResolveRequestPromptFallsBackWhenLegacyRecordHasNoOwner(t *testing.T) {
	now := time.Date(2026, 5, 8, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	surface.PendingRequests["req-legacy-1"] = &state.RequestPromptRecord{
		RequestID:    "req-legacy-1",
		RequestType:  "approval",
		InstanceID:   "inst-1",
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		CardRevision: 1,
	}
	surface.PendingRequestOrder = []string{"req-legacy-1"}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-legacy-1",
	})
	if len(surface.PendingRequests) != 0 {
		t.Fatalf("expected legacy request to resolve through fallback, got %#v", surface.PendingRequests)
	}
}

func TestRestorePendingRequestDispatchFindsFrozenOwnerSurface(t *testing.T) {
	now := time.Date(2026, 5, 8, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:              "inst-1",
		DisplayName:             "droid",
		WorkspaceRoot:           "/data/dl/droid",
		WorkspaceKey:            "/data/dl/droid",
		ShortName:               "droid",
		Online:                  true,
		ObservedFocusedThreadID: "thread-1",
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "主线程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", GatewayID: "app-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-2", GatewayID: "app-1", ChatID: "chat-2", ActorUserID: "user-2", InstanceID: "inst-1"})
	surface1 := svc.root.Surfaces["surface-1"]
	surface2 := svc.root.Surfaces["surface-2"]
	surface1.PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:                "req-1",
		RequestType:              "approval",
		InstanceID:               "inst-1",
		ThreadID:                 "thread-1",
		TurnID:                   "turn-1",
		OwnerSurfaceSessionID:    "surface-1",
		OwnerGatewayID:           "app-1",
		OwnerChatID:              "chat-1",
		PendingDispatchCommandID: "cmd-1",
		CardRevision:             1,
		Phase:                    "waiting_dispatch",
	}
	surface1.PendingRequestOrder = []string{"req-1"}
	surface2.PendingRequests["req-1"] = &state.RequestPromptRecord{
		RequestID:                "req-1",
		RequestType:              "approval",
		InstanceID:               "inst-1",
		ThreadID:                 "thread-1",
		TurnID:                   "turn-1",
		OwnerSurfaceSessionID:    "surface-1",
		PendingDispatchCommandID: "cmd-1",
		CardRevision:             1,
		Phase:                    "waiting_dispatch",
	}
	surface2.PendingRequestOrder = []string{"req-1"}

	restore := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: "cmd-1",
		Accepted:  false,
		Error:     "daemon rejected",
	})
	if len(restore) != 2 {
		t.Fatalf("expected request refresh plus notice, got %#v", restore)
	}
	if restore[0].SurfaceSessionID != "surface-1" || restore[1].SurfaceSessionID != "surface-1" {
		t.Fatalf("expected owner surface to receive restore events, got %#v", restore)
	}
	if prompt := requestPromptFromEvent(t, restore[0]); prompt.Phase != "editing" {
		t.Fatalf("expected request to return to editing phase, got %#v", prompt)
	}
	if pending := surface1.PendingRequests["req-1"]; pending == nil || pending.PendingDispatchCommandID != "" {
		t.Fatalf("expected owner request dispatch marker to clear, got %#v", pending)
	}
	if pending := surface2.PendingRequests["req-1"]; pending == nil || pending.PendingDispatchCommandID != "cmd-1" {
		t.Fatalf("expected stray non-owner request to remain untouched, got %#v", pending)
	}
}
