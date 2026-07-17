package codex

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func mcpOAuthLoginFlowKey(serverName, threadID string) string {
	return strings.TrimSpace(serverName) + "\x00" + strings.TrimSpace(threadID)
}

func (t *Translator) observeMCPOAuthLoginResponse(requestID string, message map[string]any) (Result, bool) {
	pending, exists := t.pendingMCPOAuthLogins[requestID]
	if !exists {
		return Result{}, false
	}
	if errMsg := extractJSONRPCErrorMessage(message); errMsg != "" {
		t.clearPendingMCPOAuthLogin(requestID, pending)
		return Result{
			Suppress: true,
			Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
				Code:             "mcp_oauth_login_failed",
				Layer:            "codex",
				Stage:            "command_response",
				Operation:        string(agentproto.CommandMCPOAuthLogin),
				Message:          "Codex 拒绝了这次 MCP 服务认证请求。",
				Details:          errMsg,
				SurfaceSessionID: pending.Initiator.SurfaceSessionID,
				CommandID:        pending.CommandID,
				ThreadID:         pending.ThreadID,
				RequestID:        requestID,
			})},
		}, true
	}
	authorizationURL := strings.TrimSpace(firstNonEmptyString(
		lookupString(message, "result", "authorization_url"),
		lookupString(message, "result", "authorizationUrl"),
	))
	if authorizationURL == "" {
		t.clearPendingMCPOAuthLogin(requestID, pending)
		return Result{
			Suppress: true,
			Events: []agentproto.Event{agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
				Code:             "mcp_oauth_login_invalid_response",
				Layer:            "codex",
				Stage:            "command_response",
				Operation:        string(agentproto.CommandMCPOAuthLogin),
				Message:          "Codex 返回的 MCP 服务认证链接为空。",
				SurfaceSessionID: pending.Initiator.SurfaceSessionID,
				CommandID:        pending.CommandID,
				ThreadID:         pending.ThreadID,
				RequestID:        requestID,
			})},
		}, true
	}
	pending.AuthorizationURL = authorizationURL
	t.pendingMCPOAuthLogins[requestID] = pending
	return Result{
		Suppress: true,
		Events: []agentproto.Event{{
			Kind:      agentproto.EventMCPOAuthLoginURLReady,
			CommandID: pending.CommandID,
			ThreadID:  pending.ThreadID,
			RequestID: requestID,
			Status:    "pending",
			Initiator: pending.Initiator,
			MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{
				ServerName:       pending.ServerName,
				ThreadID:         pending.ThreadID,
				Scopes:           append([]string(nil), pending.Scopes...),
				TimeoutSecs:      pending.TimeoutSecs,
				AuthorizationURL: authorizationURL,
			},
			Metadata: map[string]any{
				"serverName":       pending.ServerName,
				"authorizationUrl": authorizationURL,
			},
		}},
	}, true
}

func (t *Translator) observeMCPOAuthLoginCompleted(message map[string]any) (Result, bool) {
	params := lookupMap(message, "params")
	serverName := strings.TrimSpace(lookupStringFromAny(params["name"]))
	threadID := strings.TrimSpace(lookupStringFromAny(params["threadId"]))
	if serverName == "" {
		return Result{}, true
	}
	requestID, exists := t.pendingMCPOAuthLoginKeys[mcpOAuthLoginFlowKey(serverName, threadID)]
	if !exists {
		t.debugf("observe mcp oauth login completed without pending flow: server=%s thread=%s", serverName, threadID)
		return t.observeCapabilityState("mcpServer/oauthLogin/completed", message), true
	}
	pending := t.pendingMCPOAuthLogins[requestID]
	t.clearPendingMCPOAuthLogin(requestID, pending)
	success := lookupBoolFromAny(params["success"])
	errorText := strings.TrimSpace(lookupStringFromAny(params["error"]))
	status := "failed"
	if success {
		status = "completed"
	}
	return Result{
		Events: []agentproto.Event{{
			Kind:      agentproto.EventMCPOAuthLoginCompleted,
			CommandID: pending.CommandID,
			ThreadID:  pending.ThreadID,
			RequestID: requestID,
			Status:    status,
			Initiator: pending.Initiator,
			MCPOAuthLogin: &agentproto.MCPOAuthLoginEvent{
				ServerName:       pending.ServerName,
				ThreadID:         pending.ThreadID,
				Scopes:           append([]string(nil), pending.Scopes...),
				TimeoutSecs:      pending.TimeoutSecs,
				AuthorizationURL: pending.AuthorizationURL,
				Success:          success,
				Error:            errorText,
			},
			Metadata: map[string]any{
				"serverName": pending.ServerName,
				"success":    success,
			},
			ErrorMessage: errorText,
		}},
	}, true
}

func (t *Translator) clearPendingMCPOAuthLogin(requestID string, pending pendingMCPOAuthLogin) {
	delete(t.pendingMCPOAuthLogins, requestID)
	delete(t.pendingMCPOAuthLoginKeys, mcpOAuthLoginFlowKey(pending.ServerName, pending.ThreadID))
}
