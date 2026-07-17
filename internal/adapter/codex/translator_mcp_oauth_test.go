package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestTranslateMCPOAuthLoginCommand(t *testing.T) {
	tr := NewTranslator("inst-1")
	payloads, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-oauth-1",
		Kind:      agentproto.CommandMCPOAuthLogin,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{
			ServerName:  "docs",
			ThreadID:    "thread-1",
			Scopes:      []string{"read", "write"},
			TimeoutSecs: 30,
		}},
	})
	if err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}
	if len(payloads) != 1 {
		t.Fatalf("expected one payload, got %d", len(payloads))
	}
	var payload map[string]any
	if err := json.Unmarshal(payloads[0], &payload); err != nil {
		t.Fatalf("payload is not valid json: %v", err)
	}
	if got := payload["method"]; got != "mcpServer/oauth/login" {
		t.Fatalf("method = %v", got)
	}
	params, ok := payload["params"].(map[string]any)
	if !ok {
		t.Fatalf("params missing: %#v", payload)
	}
	if params["name"] != "docs" || params["threadId"] != "thread-1" || params["timeoutSecs"] != float64(30) {
		t.Fatalf("unexpected params: %#v", params)
	}
	scopes, ok := params["scopes"].([]any)
	if !ok || len(scopes) != 2 || scopes[0] != "read" || scopes[1] != "write" {
		t.Fatalf("unexpected scopes: %#v", params["scopes"])
	}
}

func TestMCPOAuthLoginResponseEmitsURLReady(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-oauth-1",
		Kind:      agentproto.CommandMCPOAuthLogin,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		MCP:       agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "docs", ThreadID: "thread-1"}},
	}); err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"relay-mcp-oauth-login-0","result":{"authorization_url":"https://auth.example/consent"}}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if !result.Suppress {
		t.Fatalf("expected response to be suppressed")
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventMCPOAuthLoginURLReady || event.CommandID != "cmd-oauth-1" || event.ThreadID != "thread-1" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event.Initiator.Kind != agentproto.InitiatorRemoteSurface || event.Initiator.SurfaceSessionID != "surface-1" {
		t.Fatalf("unexpected initiator: %#v", event.Initiator)
	}
	if event.MCPOAuthLogin == nil || event.MCPOAuthLogin.ServerName != "docs" || event.MCPOAuthLogin.AuthorizationURL != "https://auth.example/consent" {
		t.Fatalf("unexpected oauth payload: %#v", event.MCPOAuthLogin)
	}
}

func TestMCPOAuthLoginErrorEmitsSystemError(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-oauth-1",
		Kind:      agentproto.CommandMCPOAuthLogin,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		MCP:       agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "stdio-only"}},
	}); err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}
	result, err := tr.ObserveServer([]byte(`{"id":"relay-mcp-oauth-login-0","error":{"message":"OAuth login is only supported for streamable HTTP servers."}}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].Kind != agentproto.EventSystemError || result.Events[0].Problem == nil {
		t.Fatalf("expected system error event, got %#v", result.Events)
	}
	if result.Events[0].Problem.Code != "mcp_oauth_login_failed" || !strings.Contains(result.Events[0].Problem.Details, "streamable HTTP") {
		t.Fatalf("unexpected problem: %#v", result.Events[0].Problem)
	}
}

func TestMCPOAuthLoginCompletedUsesProtocolCorrelation(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-oauth-1",
		Kind:      agentproto.CommandMCPOAuthLogin,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		MCP:       agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "docs", ThreadID: "thread-1"}},
	}); err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"id":"relay-mcp-oauth-login-0","result":{"authorization_url":"https://auth.example/consent"}}`)); err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	ignored, err := tr.ObserveServer([]byte(`{"method":"mcpServer/oauthLogin/completed","params":{"name":"other","threadId":"thread-1","success":true}}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if len(ignored.Events) != 1 || ignored.Events[0].Kind != agentproto.EventCapabilityStateUpdated {
		t.Fatalf("expected unmatched completion to remain state-only, got %#v", ignored.Events)
	}
	result, err := tr.ObserveServer([]byte(`{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","threadId":"thread-1","success":true}}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventMCPOAuthLoginCompleted || event.Status != "completed" {
		t.Fatalf("unexpected completion event: %#v", event)
	}
	if event.MCPOAuthLogin == nil || !event.MCPOAuthLogin.Success || event.MCPOAuthLogin.AuthorizationURL != "https://auth.example/consent" {
		t.Fatalf("unexpected oauth completion payload: %#v", event.MCPOAuthLogin)
	}
}

func TestMCPOAuthLoginCompletedFailure(t *testing.T) {
	tr := NewTranslator("inst-1")
	if _, err := tr.TranslateCommand(agentproto.Command{
		CommandID: "cmd-oauth-1",
		Kind:      agentproto.CommandMCPOAuthLogin,
		Origin:    agentproto.Origin{Surface: "surface-1"},
		MCP:       agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: "docs"}},
	}); err != nil {
		t.Fatalf("TranslateCommand returned error: %v", err)
	}
	if _, err := tr.ObserveServer([]byte(`{"id":"relay-mcp-oauth-login-0","result":{"authorization_url":"https://auth.example/consent"}}`)); err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	result, err := tr.ObserveServer([]byte(`{"method":"mcpServer/oauthLogin/completed","params":{"name":"docs","success":false,"error":"callback timed out"}}`))
	if err != nil {
		t.Fatalf("ObserveServer returned error: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventMCPOAuthLoginCompleted || event.Status != "failed" || event.ErrorMessage != "callback timed out" {
		t.Fatalf("unexpected failure event: %#v", event)
	}
	if event.MCPOAuthLogin == nil || event.MCPOAuthLogin.Success || event.MCPOAuthLogin.Error != "callback timed out" {
		t.Fatalf("unexpected oauth payload: %#v", event.MCPOAuthLogin)
	}
}
