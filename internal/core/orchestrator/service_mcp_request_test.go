package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPermissionsRequestPromptBecomesRenderableCard(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-perm-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:  agentproto.RequestTypePermissionsRequestApproval,
			Title: "需要授予权限",
			Permissions: &agentproto.PermissionsRequestPrompt{
				Reason: "需要访问 docs.read",
				Permissions: []map[string]any{
					{"name": "docs.read", "title": "Read docs"},
				},
			},
		},
		Metadata: map[string]any{
			"requestType": "permissions_request_approval",
		},
	})

	if len(events) != 1 {
		t.Fatalf("expected renderable permissions request card, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestType != "permissions_request_approval" || len(prompt.Options) != 3 {
		t.Fatalf("unexpected permissions prompt: %#v", prompt)
	}
	if prompt.Options[0].OptionID != "accept" || prompt.Options[1].OptionID != "acceptForSession" || prompt.Options[2].OptionID != "decline" {
		t.Fatalf("unexpected permissions request options: %#v", prompt.Options)
	}
	if record := svc.root.Surfaces["surface-1"].PendingRequests["req-perm-1"]; record == nil || record.RequestType != "permissions_request_approval" {
		t.Fatalf("expected pending permissions request state, got %#v", svc.root.Surfaces["surface-1"].PendingRequests)
	}
}

func TestRespondPermissionsRequestBuildsStructuredGrantPayload(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-perm-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type: agentproto.RequestTypePermissionsRequestApproval,
			Permissions: &agentproto.PermissionsRequestPrompt{
				Permissions: []map[string]any{
					{"name": "docs.read", "title": "Read docs"},
				},
			},
		},
		Metadata: map[string]any{"requestType": "permissions_request_approval"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-1",
		Request:          testRequestAction("req-perm-1", "permissions_request_approval", "acceptForSession", nil, 0),
	})

	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed request replacement plus one request respond command, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || prompt.Phase != "waiting_dispatch" {
		t.Fatalf("expected permissions request to seal before dispatch, got %#v", prompt)
	}
	response := events[1].Command.Request.Response
	if response["scope"] != "session" {
		t.Fatalf("expected session-scoped permission grant, got %#v", response)
	}
	permissions, _ := response["permissions"].([]map[string]any)
	if len(permissions) != 1 || permissions[0]["name"] != "docs.read" {
		t.Fatalf("unexpected granted permissions payload: %#v", response)
	}
}

func TestRespondMCPElicitationFormBuildsStructuredResponse(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-mcp-form-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:  agentproto.RequestTypeMCPServerElicitation,
			Title: "需要处理 MCP 请求",
			MCPElicitation: &agentproto.MCPElicitationPrompt{
				ServerName: "docs",
				Mode:       "form",
				Message:    "请补充返回内容",
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"token":    map[string]any{"type": "string", "title": "Token", "description": "OAuth token"},
						"remember": map[string]any{"type": "boolean", "title": "Remember", "description": "Remember this grant"},
					},
					"required": []any{"token"},
				},
				Meta: map[string]any{"flow": "oauth"},
			},
		},
		Metadata: map[string]any{"requestType": "mcp_server_elicitation"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-2",
		Request: testRequestAction("req-mcp-form-1", "mcp_server_elicitation", "", map[string][]string{
			"token":    {"secret-token"},
			"remember": {"true"},
		}, 0),
		RequestAnswers: map[string][]string{
			"token":    []string{"secret-token"},
			"remember": []string{"true"},
		},
	})

	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed inline replacement plus one mcp request respond command, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed {
		t.Fatalf("expected completed mcp form to render sealed prompt, got %#v", prompt)
	}
	response := events[1].Command.Request.Response
	if response["action"] != "accept" {
		t.Fatalf("expected accept action, got %#v", response)
	}
	content, _ := response["content"].(map[string]any)
	if content["token"] != "secret-token" || content["remember"] != true {
		t.Fatalf("unexpected mcp form content: %#v", response)
	}
	meta, _ := response["_meta"].(map[string]any)
	if meta["flow"] != "oauth" {
		t.Fatalf("expected mcp response to carry prompt meta, got %#v", response)
	}
}

func TestRespondMCPElicitationFormPartialSaveRefreshesCurrentStepInline(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-mcp-form-step-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type:  agentproto.RequestTypeMCPServerElicitation,
			Title: "需要处理 MCP 请求",
			MCPElicitation: &agentproto.MCPElicitationPrompt{
				ServerName: "docs",
				Mode:       "form",
				Message:    "请补充返回内容",
				RequestedSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"mode":  map[string]any{"type": "string", "title": "模式", "enum": []any{"auto", "manual"}},
						"token": map[string]any{"type": "string", "title": "Token"},
					},
					"required": []any{"mode", "token"},
				},
			},
		},
		Metadata: map[string]any{"requestType": "mcp_server_elicitation"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		Request: testRequestAction("req-mcp-form-step-1", "mcp_server_elicitation", "", map[string][]string{
			"mode": {"auto"},
		}, 0),
		RequestAnswers: map[string][]string{
			"mode": {"auto"},
		},
	})
	if len(events) != 1 || !events[0].InlineReplaceCurrentCard {
		t.Fatalf("expected mcp partial save to refresh current card inline, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if prompt.RequestRevision != 2 || prompt.CurrentQuestionIndex != 1 {
		t.Fatalf("expected mcp partial save to advance to next question, got %#v", prompt)
	}
	if !prompt.Questions[0].Answered || prompt.Questions[0].DefaultValue != "auto" {
		t.Fatalf("expected saved mcp answer to remain in refreshed prompt, got %#v", prompt.Questions[0])
	}
}

func TestRespondMCPElicitationURLAcceptBuildsContinuePayload(t *testing.T) {
	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventRequestStarted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		RequestID: "req-mcp-url-1",
		RequestPrompt: &agentproto.RequestPrompt{
			Type: agentproto.RequestTypeMCPServerElicitation,
			MCPElicitation: &agentproto.MCPElicitationPrompt{
				ServerName:    "docs",
				Mode:          "url",
				Message:       "请完成外部授权",
				URL:           "https://example.com/approve",
				ElicitationID: "eli-1",
				Meta:          map[string]any{"flow": "oauth"},
			},
		},
		Metadata: map[string]any{"requestType": "mcp_server_elicitation"},
	})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-3",
		Request:          testRequestAction("req-mcp-url-1", "mcp_server_elicitation", "accept", nil, 0),
	})

	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed request replacement plus one mcp url respond command, got %#v", events)
	}
	prompt := requestPromptFromEvent(t, events[0])
	if !prompt.Sealed || prompt.Phase != "waiting_dispatch" {
		t.Fatalf("expected url elicitation request to seal before dispatch, got %#v", prompt)
	}
	response := events[1].Command.Request.Response
	if response["action"] != "accept" {
		t.Fatalf("expected accept action for url elicitation, got %#v", response)
	}
	if _, ok := response["content"]; !ok {
		t.Fatalf("expected url elicitation response to include content field, got %#v", response)
	}
}

func TestMCPElicitationToolApprovalUsesDedicatedSemanticKindAndSessionMeta(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:          agentproto.EventRequestStarted,
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		RequestID:     "req-mcp-approval-1",
		RequestPrompt: mcpApprovalRequestPrompt([]any{"session", "always"}),
		Metadata:      map[string]any{"requestType": "mcp_server_elicitation"},
	})

	surface := svc.root.Surfaces["surface-1"]
	request := pendingRequestRecord(surface, "req-mcp-approval-1")
	if request == nil {
		t.Fatalf("expected pending mcp approval request")
	}
	if request.SemanticKind != control.RequestSemanticMCPServerElicitationApproval {
		t.Fatalf("semantic kind = %q, want %q", request.SemanticKind, control.RequestSemanticMCPServerElicitationApproval)
	}
	if len(request.Questions) != 0 {
		t.Fatalf("expected mcp approval elicitation to avoid generic form questions, got %#v", request.Questions)
	}
	if len(request.Options) != 4 || request.Options[0].OptionID != "accept" || request.Options[1].OptionID != "acceptForSession" || request.Options[2].OptionID != "decline" || request.Options[3].OptionID != "cancel" {
		t.Fatalf("unexpected mcp approval options: %#v", request.Options)
	}
	if !strings.Contains(request.HintText, "持久允许暂未开放") {
		t.Fatalf("expected hint to mention unsupported persistent approval, got %q", request.HintText)
	}

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-4",
		Request:          testRequestAction("req-mcp-approval-1", "mcp_server_elicitation", "acceptForSession", nil, request.CardRevision),
	})
	if len(events) != 2 || !events[0].InlineReplaceCurrentCard || events[1].Command == nil {
		t.Fatalf("expected sealed inline replacement plus one mcp approval respond command, got %#v", events)
	}
	response := events[1].Command.Request.Response
	if response["action"] != "accept" {
		t.Fatalf("expected accept action, got %#v", response)
	}
	meta, _ := response["_meta"].(map[string]any)
	if meta["codex_approval_kind"] != "mcp_tool_call" || meta["persist"] != "session" {
		t.Fatalf("expected session persist in mcp approval response meta, got %#v", response)
	}
	if response["content"] != nil {
		t.Fatalf("expected mcp approval response content to stay nil, got %#v", response)
	}
}

func TestMCPElicitationToolApprovalAcceptOnceDropsPersistAdvertisement(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:          agentproto.EventRequestStarted,
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		RequestID:     "req-mcp-approval-once",
		RequestPrompt: mcpApprovalRequestPrompt([]any{"session", "always"}),
		Metadata:      map[string]any{"requestType": "mcp_server_elicitation"},
	})

	surface := svc.root.Surfaces["surface-1"]
	request := pendingRequestRecord(surface, "req-mcp-approval-once")
	if request == nil {
		t.Fatalf("expected pending mcp approval request")
	}
	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionRespondRequest,
		SurfaceSessionID: "surface-1",
		MessageID:        "om-card-4",
		Request:          testRequestAction("req-mcp-approval-once", "mcp_server_elicitation", "accept", nil, request.CardRevision),
	})
	if len(events) != 2 || events[1].Command == nil {
		t.Fatalf("expected one mcp approval respond command, got %#v", events)
	}
	response := events[1].Command.Request.Response
	if response["action"] != "accept" {
		t.Fatalf("expected accept action, got %#v", response)
	}
	meta, _ := response["_meta"].(map[string]any)
	if meta["codex_approval_kind"] != "mcp_tool_call" {
		t.Fatalf("expected approval kind to stay in response meta, got %#v", response)
	}
	if _, ok := meta["persist"]; ok {
		t.Fatalf("expected accept-once response to remove persist advertisement, got %#v", response)
	}
}

func TestMCPElicitationToolApprovalAlwaysOnlyDoesNotExposeSessionButton(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	attachMCPRequestTestSurface(svc)
	svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:          agentproto.EventRequestStarted,
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		RequestID:     "req-mcp-approval-always",
		RequestPrompt: mcpApprovalRequestPrompt("always"),
		Metadata:      map[string]any{"requestType": "mcp_server_elicitation"},
	})

	surface := svc.root.Surfaces["surface-1"]
	request := pendingRequestRecord(surface, "req-mcp-approval-always")
	if request == nil {
		t.Fatalf("expected pending mcp approval request")
	}
	if request.SemanticKind != control.RequestSemanticMCPServerElicitationApproval {
		t.Fatalf("semantic kind = %q, want %q", request.SemanticKind, control.RequestSemanticMCPServerElicitationApproval)
	}
	for _, option := range request.Options {
		if option.OptionID == "acceptForSession" {
			t.Fatalf("expected always-only advertisement not to expose session option, got %#v", request.Options)
		}
	}
	if !strings.Contains(request.HintText, "持久允许暂未开放") {
		t.Fatalf("expected always-only hint to mention unsupported persistent approval, got %q", request.HintText)
	}
}

func mcpApprovalRequestPrompt(persist any) *agentproto.RequestPrompt {
	return &agentproto.RequestPrompt{
		Type:  agentproto.RequestTypeMCPServerElicitation,
		Title: "需要确认 MCP 工具调用",
		Body:  "允许 GitHub MCP 查询 issue？",
		MCPElicitation: &agentproto.MCPElicitationPrompt{
			ServerName: "apps",
			Mode:       "form",
			Message:    "允许 GitHub MCP 查询 issue？",
			RequestedSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
			Meta: map[string]any{
				"codex_approval_kind": "mcp_tool_call",
				"persist":             persist,
				"tool_title":          "Search GitHub",
				"tool_params_display": []any{
					map[string]any{"name": "query", "value": "repo:kxn/codex-remote-feishu #669"},
				},
			},
		},
	}
}

func attachMCPRequestTestSurface(svc *Service) {
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
}
