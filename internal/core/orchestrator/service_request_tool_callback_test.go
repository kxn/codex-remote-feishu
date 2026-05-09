package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/frontstagecontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestToolCallbackRequestAutoDispatchesUnsupportedResponseAndClearsOnResolve(t *testing.T) {
	now := time.Date(2026, 4, 26, 15, 0, 0, 0, time.UTC)
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

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-tool-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:    agentproto.RequestTypeToolCallback,
			RawType: "tool_callback",
			Title:   "收到工具回调",
			ToolCallback: &agentproto.ToolCallbackPrompt{
				CallID:   "call-1",
				ToolName: "lookup_ticket",
				Arguments: map[string]any{
					"ticket": "ABC-123",
				},
			},
		},
		Metadata: map[string]any{
			"requestType": "tool_callback",
			"title":       "收到工具回调",
			"tool":        "lookup_ticket",
			"callId":      "call-1",
			"arguments": map[string]any{
				"ticket": "ABC-123",
			},
		},
	})
	if len(events) != 2 || events[0].RequestView == nil || events[1].Command == nil {
		t.Fatalf("expected one read-only request view plus one auto-dispatch command, got %#v", events)
	}

	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestType != "tool_callback" || prompt.Title != "工具回调暂不支持" {
		t.Fatalf("unexpected tool callback prompt: %#v", prompt)
	}
	if !prompt.Sealed || prompt.Phase != frontstagecontract.PhaseWaitingDispatch {
		t.Fatalf("expected tool callback prompt to start sealed in waiting_dispatch, got %#v", prompt)
	}
	if len(prompt.Options) != 0 || len(prompt.Questions) != 0 {
		t.Fatalf("expected tool callback prompt to stay read-only, got %#v", prompt)
	}
	if len(prompt.Sections) < 2 || !containsPromptSectionLine(prompt.Sections[0], "当前工具请求客户端执行一段 dynamic tool callback。") {
		t.Fatalf("expected tool callback intro section, got %#v", prompt.Sections)
	}
	if !strings.Contains(prompt.StatusText, "已自动上报 unsupported") {
		t.Fatalf("expected tool callback waiting status, got %#v", prompt)
	}

	command := events[1].Command
	if command.Kind != agentproto.CommandRequestRespond || command.Request.RequestID != "req-tool-1" {
		t.Fatalf("unexpected tool callback request respond command: %#v", command)
	}
	if command.Request.Response["type"] != "structured" {
		t.Fatalf("expected structured tool callback response, got %#v", command.Request.Response)
	}
	resultPayload, _ := command.Request.Response["result"].(map[string]any)
	if resultPayload["success"] != false {
		t.Fatalf("expected unsupported tool callback result payload, got %#v", command.Request.Response)
	}

	record := svc.root.Surfaces["surface-1"].PendingRequests["req-tool-1"]
	if record == nil || record.RequestType != "tool_callback" || record.PendingDispatchCommandID != command.CommandID || record.LifecycleState != requestLifecycleSubmitting {
		t.Fatalf("expected pending tool callback state to remain until resolve, got %#v", record)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-tool-1",
	})
	if len(svc.root.Surfaces["surface-1"].PendingRequests) != 0 {
		t.Fatalf("expected tool callback request state to clear after resolve, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
}

func TestToolCallbackCommandRejectKeepsPromptSealedAndPointsUserToStop(t *testing.T) {
	now := time.Date(2026, 4, 26, 15, 5, 0, 0, time.UTC)
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

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-tool-2",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:    agentproto.RequestTypeToolCallback,
			RawType: "tool_callback",
			ToolCallback: &agentproto.ToolCallbackPrompt{
				CallID:   "call-2",
				ToolName: "lookup_ticket",
			},
		},
		Metadata: map[string]any{
			"requestType": "tool_callback",
			"tool":        "lookup_ticket",
			"callId":      "call-2",
		},
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected auto-dispatch command, got %#v", events)
	}

	restore := svc.HandleCommandRejected("inst-1", agentproto.CommandAck{
		CommandID: events[1].Command.CommandID,
		Accepted:  false,
		Error:     "translator failed",
	})
	if len(restore) != 2 || !restore[0].InlineReplaceCurrentCard || restore[1].Notice == nil {
		t.Fatalf("expected sealed prompt refresh plus notice after tool callback command reject, got %#v", restore)
	}
	prompt := requestPromptFromEvent(t, restore[0])
	if !prompt.Sealed || prompt.Phase != frontstagecontract.PhaseWaitingDispatch {
		t.Fatalf("expected tool callback reject path to stay sealed, got %#v", prompt)
	}
	if !strings.Contains(prompt.StatusText, "/stop") {
		t.Fatalf("expected tool callback reject path to point user to /stop, got %#v", prompt)
	}
	record := svc.root.Surfaces["surface-1"].PendingRequests["req-tool-2"]
	if record == nil || record.PendingDispatchCommandID != "" || record.Phase != frontstagecontract.PhaseWaitingDispatch || record.LifecycleState != requestLifecycleAwaitingBackendConsume {
		t.Fatalf("expected tool callback record to stay sealed after command reject, got %#v", record)
	}
}
