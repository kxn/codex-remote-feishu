package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestClaudeCanUseToolRequestUsesDedicatedBridgeContract(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-tool-1",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestMethod": "control_request/can_use_tool",
			"toolName":      "bash",
			"blockedPath":   "/data/dl/droid",
			"options": []map[string]any{
				{"id": "accept", "label": "允许一次"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", started)
	}
	prompt := requestPromptFromEvent(t, started[0])
	if prompt.SemanticKind != control.RequestSemanticApprovalCanUseTool {
		t.Fatalf("semantic kind = %q, want %q", prompt.SemanticKind, control.RequestSemanticApprovalCanUseTool)
	}
	if !strings.Contains(prompt.HintText, "工具调用") {
		t.Fatalf("hint text = %q, want tool-call guidance", prompt.HintText)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-tool-1", "approval", "accept", nil, 0),
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected inline replacement plus command, got %#v", events)
	}
	req := events[1].Command.Request
	if req.BridgeKind != string(control.RequestBridgeCanUseTool) || req.SemanticKind != control.RequestSemanticApprovalCanUseTool || req.InterruptOnDecline {
		t.Fatalf("unexpected request bridge contract: %#v", req)
	}
}

func TestPlanConfirmationDeclineRequestsInterruptOnDecline(t *testing.T) {
	now := time.Date(2026, 4, 28, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestMethod": "tool/ExitPlanMode",
			"body":          "Claude 计划如下，请确认是否继续。",
			"options": []map[string]any{
				{"id": "accept", "label": "批准"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})
	if len(started) != 1 {
		t.Fatalf("expected one request prompt event, got %#v", started)
	}
	prompt := requestPromptFromEvent(t, started[0])
	if prompt.SemanticKind != control.RequestSemanticPlanConfirmation {
		t.Fatalf("semantic kind = %q, want %q", prompt.SemanticKind, control.RequestSemanticPlanConfirmation)
	}
	if !strings.Contains(prompt.HintText, "停止当前 turn") {
		t.Fatalf("hint text = %q, want interrupt guidance", prompt.HintText)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-2",
		Request:          testRequestAction("req-plan-1", "approval", "decline", nil, 0),
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected inline replacement plus command, got %#v", events)
	}
	req := events[1].Command.Request
	if req.BridgeKind != string(control.RequestBridgePlanConfirmation) || !req.InterruptOnDecline {
		t.Fatalf("unexpected plan bridge contract: %#v", req)
	}
}
