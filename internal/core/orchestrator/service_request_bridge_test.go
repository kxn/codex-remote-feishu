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
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
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

func TestClaudeCanUseToolCaptureFeedbackUsesSameRequestDenyMessage(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:    "inst-1",
		DisplayName:   "droid",
		WorkspaceRoot: "/data/dl/droid",
		WorkspaceKey:  "/data/dl/droid",
		ShortName:     "droid",
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-tool-2",
		Metadata: map[string]any{
			"requestType":   "approval",
			"requestMethod": "control_request/can_use_tool",
			"toolName":      "Bash",
			"blockedPath":   "/data/dl/droid",
			"options": []map[string]any{
				{"id": "accept", "label": "允许一次"},
				{"id": "decline", "label": "拒绝"},
			},
		},
	})

	startCapture := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-2",
		Request:          testRequestAction("req-tool-2", "approval", "captureFeedback", nil, 0),
	})
	if len(startCapture) != 1 || startCapture[0].Notice == nil || startCapture[0].Notice.Code != "request_capture_started" {
		t.Fatalf("expected request_capture_started notice, got %#v", startCapture)
	}
	if svc.root.Surfaces["surface-1"].ActiveRequestCapture == nil {
		t.Fatalf("expected can_use_tool feedback to enter capture mode")
	}

	feedback := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-msg-3",
		Text:             "不要直接执行，先列一下当前未提交文件。",
	})
	if len(feedback) != 2 || feedback[1].Command == nil {
		t.Fatalf("expected inline replacement plus request response command, got %#v", feedback)
	}
	req := feedback[1].Command.Request
	if req.Response["decision"] != "decline" {
		t.Fatalf("expected decline decision, got %#v", req)
	}
	if req.Response["message"] != "不要直接执行，先列一下当前未提交文件。" {
		t.Fatalf("expected feedback to stay on request response, got %#v", req.Response)
	}
	if req.InterruptOnDecline {
		t.Fatalf("expected can_use_tool decline-with-feedback to avoid interrupt, got %#v", req)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveRequestCapture != nil {
		t.Fatalf("expected capture to be cleared, got %#v", surface.ActiveRequestCapture)
	}
	if len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected no queued follow-up item for same-request feedback, got %#v", surface.QueuedQueueItemIDs)
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
		Backend:       agentproto.BackendClaude,
		Online:        true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
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
	if len(prompt.Options) != 4 || prompt.Options[0].OptionID != "accept" || prompt.Options[1].OptionID != "acceptForSession" || prompt.Options[2].OptionID != "decline" || prompt.Options[3].OptionID != "revise" {
		t.Fatalf("expected plan confirmation prompt to expose quick decision actions, got %#v", prompt.Options)
	}
	if prompt.Options[1].Label != "配置本会话授权" {
		t.Fatalf("expected session option to open permission panel, got %#v", prompt.Options)
	}
	if !strings.Contains(prompt.HintText, "停止当前 turn") {
		t.Fatalf("hint text = %q, want interrupt guidance", prompt.HintText)
	}
	if !strings.Contains(prompt.HintText, "配置本会话授权") {
		t.Fatalf("hint text = %q, want session-grant guidance", prompt.HintText)
	}
	if !strings.Contains(prompt.HintText, "告诉 Claude 怎么改") {
		t.Fatalf("hint text = %q, want revise guidance", prompt.HintText)
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

func TestPlanConfirmationReviseUsesSameRequestFeedback(t *testing.T) {
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

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
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

	startCapture := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-2",
		Request:          testRequestAction("req-plan-1", "approval", "revise", nil, 0),
	})
	if len(startCapture) != 1 || startCapture[0].Notice == nil || startCapture[0].Notice.Code != "request_capture_started" {
		t.Fatalf("expected request_capture_started notice, got %#v", startCapture)
	}
	if svc.root.Surfaces["surface-1"].ActiveRequestCapture == nil {
		t.Fatalf("expected plan confirmation revise to enter capture mode")
	}

	feedback := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionTextMessage,
		SurfaceSessionID: "surface-1",
		ChatID:           "chat-1",
		ActorUserID:      "user-1",
		MessageID:        "om-msg-3",
		Text:             "先补一条回滚方案，再继续。",
	})
	if len(feedback) != 2 || feedback[1].Command == nil {
		t.Fatalf("expected inline replacement plus request response command, got %#v", feedback)
	}
	req := feedback[1].Command.Request
	if req.Response["decision"] != "revise" {
		t.Fatalf("expected revise decision, got %#v", req)
	}
	if req.Response["message"] != "先补一条回滚方案，再继续。" {
		t.Fatalf("expected revise feedback to stay on request response, got %#v", req.Response)
	}
	if req.InterruptOnDecline {
		t.Fatalf("expected revise to avoid interrupt, got %#v", req)
	}
	surface := svc.root.Surfaces["surface-1"]
	if surface.ActiveRequestCapture != nil {
		t.Fatalf("expected revise capture to be cleared, got %#v", surface.ActiveRequestCapture)
	}
	if len(surface.QueuedQueueItemIDs) != 0 {
		t.Fatalf("expected no queued follow-up item for same-request revise, got %#v", surface.QueuedQueueItemIDs)
	}
}

func TestClaudePlanConfirmationAcceptClearsPlanOverrideOnlyAfterResolved(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-1",
		DisplayName:     "droid",
		WorkspaceRoot:   "/data/dl/droid",
		WorkspaceKey:    "/data/dl/droid",
		ShortName:       "droid",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionUseThread, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", ThreadID: "thread-1"})
	surface := svc.root.Surfaces["surface-1"]
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorLocalUI},
	})

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
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
	svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-accept",
		Request:          testRequestAction("req-plan-1", "approval", "accept", nil, 0),
	})
	if surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("expected plan override to stay until request.resolved, got %#v", surface)
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata: map[string]any{
			"decision": "accept",
		},
	})
	if surface.PlanMode != state.PlanModeSettingOff || surface.PlanModeOverrideSet {
		t.Fatalf("expected request.resolved accept to clear plan override, got %#v", surface)
	}
}

func TestClaudePlanConfirmationDeclineKeepsPlanOverride(t *testing.T) {
	now := time.Date(2026, 5, 4, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.UpsertInstance(&state.InstanceRecord{
		InstanceID:      "inst-1",
		DisplayName:     "droid",
		WorkspaceRoot:   "/data/dl/droid",
		WorkspaceKey:    "/data/dl/droid",
		ShortName:       "droid",
		Backend:         agentproto.BackendClaude,
		ClaudeProfileID: state.DefaultClaudeProfileID,
		Online:          true,
		Threads: map[string]*state.ThreadRecord{
			"thread-1": {ThreadID: "thread-1", Name: "修复登录流程", CWD: "/data/dl/droid", Loaded: true},
		},
	})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionModeCommand, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", Text: "/mode claude"})
	svc.ApplySurfaceAction(control.Action{Kind: control.ActionAttachInstance, SurfaceSessionID: "surface-1", ChatID: "chat-1", ActorUserID: "user-1", InstanceID: "inst-1"})
	surface := svc.root.Surfaces["surface-1"]
	setSurfacePlanModeOverride(surface, state.PlanModeSettingOn)
	surface.PendingRequests = map[string]*state.RequestPromptRecord{
		"req-plan-1": {
			RequestID:    "req-plan-1",
			RequestType:  "approval",
			SemanticKind: control.RequestSemanticPlanConfirmation,
			Backend:      agentproto.BackendClaude,
		},
	}

	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestResolved,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-plan-1",
		Metadata: map[string]any{
			"decision": "decline",
		},
	})
	if surface.PlanMode != state.PlanModeSettingOn || !surface.PlanModeOverrideSet {
		t.Fatalf("expected decline to keep plan override, got %#v", surface)
	}
}
