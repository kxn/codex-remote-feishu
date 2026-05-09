package orchestrator

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPendingRequestsRenderSeriallyAndPromoteNextOnResolve(t *testing.T) {
	now := time.Date(2026, 5, 7, 10, 0, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "第一条确认",
			"body":        "请先处理第一条请求。",
		},
	})
	if len(first) != 1 {
		t.Fatalf("expected first request to render immediately, got %#v", first)
	}
	firstPrompt := requestPromptFromEvent(t, first[0])
	if firstPrompt.RequestID != "req-1" {
		t.Fatalf("unexpected first prompt: %#v", firstPrompt)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-2",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "第二条确认",
			"body":        "请在第一条之后处理第二条请求。",
		},
	})
	if len(second) != 0 {
		t.Fatalf("expected second request to stay queued until first resolves, got %#v", second)
	}

	surface := svc.root.Surfaces["surface-1"]
	if queued := surface.PendingRequests["req-2"]; queued == nil || queued.LifecycleState != requestLifecycleQueuedInactive {
		t.Fatalf("expected req-2 to stay queued_inactive before promotion, got %#v", queued)
	}
	if got, want := surface.PendingRequestOrder, []string{"req-1", "req-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pending request order = %#v, want %#v", got, want)
	}
	if active := activePendingRequest(surface); active == nil || active.RequestID != "req-1" {
		t.Fatalf("expected req-1 to remain active, got %#v", active)
	}

	dispatch := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-1", "approval", "accept", nil, 0),
	})
	if len(dispatch) != 2 || !dispatch[0].InlineReplaceCurrentCard || dispatch[1].Command == nil {
		t.Fatalf("expected sealed first prompt plus request.respond command, got %#v", dispatch)
	}
	if prompt := requestPromptFromEvent(t, dispatch[0]); prompt.RequestID != "req-1" || prompt.Phase != frontstagecontract.PhaseWaitingDispatch {
		t.Fatalf("expected first prompt to stay active in waiting_dispatch, got %#v", prompt)
	} else if !strings.Contains(prompt.StatusText, "正在提交当前确认") {
		t.Fatalf("expected submitting status text, got %#v", prompt)
	}
	if record := surface.PendingRequests["req-1"]; record == nil || record.LifecycleState != requestLifecycleSubmitting || record.PendingDispatchCommandID != dispatch[1].Command.CommandID {
		t.Fatalf("expected req-1 to enter submitting state, got %#v", record)
	}

	accepted := svc.HandleCommandAccepted("inst-1", agentproto.CommandAck{
		CommandID: dispatch[1].Command.CommandID,
		Accepted:  true,
	})
	if len(accepted) != 0 {
		t.Fatalf("expected request accepted transition to be runtime-only, got %#v", accepted)
	}
	if record := surface.PendingRequests["req-1"]; record == nil || record.LifecycleState != requestLifecycleAwaitingBackendConsume || record.PendingDispatchCommandID != "" {
		t.Fatalf("expected req-1 to wait on backend consume after ack.accepted, got %#v", record)
	}
	if prompt := svc.requestPromptView(surface.PendingRequests["req-1"], ""); prompt.Phase != frontstagecontract.PhaseWaitingDispatch || !prompt.Sealed {
		t.Fatalf("expected accepted request to stay sealed in waiting_dispatch projection, got %#v", prompt)
	} else if !strings.Contains(prompt.StatusText, "已提交当前确认") {
		t.Fatalf("expected awaiting_backend_consume status text, got %#v", prompt)
	}
	if active := activePendingRequest(surface); active == nil || active.RequestID != "req-1" {
		t.Fatalf("expected req-1 to stay active until resolved, got %#v", active)
	}

	resolved := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
	})
	if len(resolved) != 1 {
		t.Fatalf("expected second queued request to activate after first resolve, got %#v", resolved)
	}
	secondPrompt := requestPromptFromEvent(t, resolved[0])
	if secondPrompt.RequestID != "req-2" {
		t.Fatalf("expected req-2 to become visible after req-1 resolve, got %#v", secondPrompt)
	}
	if got, want := surface.PendingRequestOrder, []string{"req-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pending request order after resolve = %#v, want %#v", got, want)
	}
	if active := activePendingRequest(surface); active == nil || active.RequestID != "req-2" {
		t.Fatalf("expected req-2 to become active, got %#v", active)
	}
}

func TestQueuedToolCallbackAutoDispatchesOnlyAfterEarlierRequestResolves(t *testing.T) {
	now := time.Date(2026, 5, 7, 10, 5, 0, 0, time.UTC)
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
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
		Metadata: map[string]any{
			"requestType": "approval",
			"title":       "先处理这条确认",
		},
	})
	if len(first) != 1 {
		t.Fatalf("expected first approval to render, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-tool-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:    agentproto.RequestTypeToolCallback,
			RawType: "tool_callback",
			ToolCallback: &agentproto.ToolCallbackPrompt{
				CallID:   "call-1",
				ToolName: "lookup_ticket",
			},
		},
		Metadata: map[string]any{
			"requestType": "tool_callback",
			"tool":        "lookup_ticket",
			"callId":      "call-1",
		},
	})
	if len(second) != 0 {
		t.Fatalf("expected queued tool callback to stay hidden until earlier request resolves, got %#v", second)
	}
	if record := svc.root.Surfaces["surface-1"].PendingRequests["req-tool-1"]; record == nil || record.PendingDispatchCommandID != "" || record.LifecycleState != requestLifecycleQueuedInactive {
		t.Fatalf("expected queued tool callback to stay undispatched, got %#v", record)
	}

	resolved := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-1",
	})
	if len(resolved) != 2 || resolved[0].RequestView == nil || resolved[1].Command == nil {
		t.Fatalf("expected queued tool callback to auto-dispatch only after req-1 resolved, got %#v", resolved)
	}
	prompt := requestPromptFromEvent(t, resolved[0])
	if prompt.RequestID != "req-tool-1" || prompt.Phase != frontstagecontract.PhaseWaitingDispatch {
		t.Fatalf("expected tool callback prompt to activate in waiting_dispatch, got %#v", prompt)
	}
	if resolved[1].Command.Kind != agentproto.CommandRequestRespond || resolved[1].Command.Request.RequestID != "req-tool-1" {
		t.Fatalf("expected auto-dispatch request.respond for queued tool callback, got %#v", resolved[1].Command)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-tool-1"]
	if record == nil || record.PendingDispatchCommandID != resolved[1].Command.CommandID || record.LifecycleState != requestLifecycleSubmitting {
		t.Fatalf("expected queued tool callback to capture dispatch command after activation, got %#v", record)
	}
}
