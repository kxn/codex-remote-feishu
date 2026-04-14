package orchestrator

import (
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

	if len(events) != 1 || events[0].FeishuDirectRequestPrompt == nil {
		t.Fatalf("expected renderable permissions request card, got %#v", events)
	}
	prompt := events[0].FeishuDirectRequestPrompt
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
		RequestID:        "req-perm-1",
		RequestType:      "permissions_request_approval",
		RequestOptionID:  "acceptForSession",
	})

	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one request respond command, got %#v", events)
	}
	response := events[0].Command.Request.Response
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
		RequestID:        "req-mcp-form-1",
		RequestType:      "mcp_server_elicitation",
		RequestOptionID:  "submit",
		RequestAnswers: map[string][]string{
			"token":    []string{"secret-token"},
			"remember": []string{"true"},
		},
	})

	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one mcp request respond command, got %#v", events)
	}
	response := events[0].Command.Request.Response
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
		RequestID:        "req-mcp-url-1",
		RequestType:      "mcp_server_elicitation",
		RequestOptionID:  "accept",
	})

	if len(events) != 1 || events[0].Command == nil {
		t.Fatalf("expected one mcp url respond command, got %#v", events)
	}
	response := events[0].Command.Request.Response
	if response["action"] != "accept" {
		t.Fatalf("expected accept action for url elicitation, got %#v", response)
	}
	if _, ok := response["content"]; !ok {
		t.Fatalf("expected url elicitation response to include content field, got %#v", response)
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
