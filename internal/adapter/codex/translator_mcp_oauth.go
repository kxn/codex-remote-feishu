package codex

import (
	"reflect"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/jsonrpcutil"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func mcpOAuthLoginFlowKey(serverName, threadID string) string {
	return strings.TrimSpace(serverName) + "\x00" + strings.TrimSpace(threadID)
}

func (t *Translator) observeMCPOAuthLoginResponse(requestID string, message map[string]any) (Result, bool) {
	pending, exists := t.pendingMCPOAuthLogins[requestID]
	if !exists {
		return Result{}, false
	}
	if errMsg := jsonrpcutil.ExtractErrorMessage(message); errMsg != "" {
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
	authorizationURL := strings.TrimSpace(xutil.FirstNonEmpty(
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
	pending.LoginID = strings.TrimSpace(lookupString(message, "result", "loginId"))
	if t.completedMCPOAuthLoginIDs[pending.LoginID] {
		pending.LoginID = ""
	}
	if pending.LoginID != "" {
		if early, exists := t.earlyMCPOAuthCompletions[pending.LoginID]; exists && early == nil {
			delete(t.earlyMCPOAuthCompletions, pending.LoginID)
			pending.LoginID = ""
		}
	}
	if pending.LoginID != "" {
		for otherID, other := range t.pendingMCPOAuthLogins {
			if otherID != requestID && other.LoginID == pending.LoginID {
				pending.LoginID = ""
				break
			}
		}
	}
	t.pendingMCPOAuthLogins[requestID] = pending
	result := Result{
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
	}
	if pending.LoginID == "" {
		// A URL without a verified attempt identifier remains useful, but its
		// eventual completion cannot be assigned safely to this or a later flow.
		t.clearPendingMCPOAuthLogin(requestID, pending)
		result.Events = append(result.Events, agentproto.NewSystemErrorEvent(agentproto.ErrorInfo{
			Code: "mcp_oauth_attempt_unverified", Layer: "codex", Stage: "completion_correlation",
			Operation:        string(agentproto.CommandMCPOAuthLogin),
			Message:          "认证链接已生成，但无法自动确认这次授权结果。完成后可用 /mcp 刷新服务状态，或重新发起 /mcpoauth 重试。",
			SurfaceSessionID: pending.Initiator.SurfaceSessionID, CommandID: pending.CommandID,
			ThreadID: pending.ThreadID, RequestID: requestID,
		}))
	} else if early, ok := t.earlyMCPOAuthCompletions[pending.LoginID]; ok {
		delete(t.earlyMCPOAuthCompletions, pending.LoginID)
		if completion, _ := t.observeMCPOAuthLoginCompleted(early); len(completion.Events) > 0 {
			result.Events = append(result.Events, completion.Events...)
		}
	}
	return result, true
}

func (t *Translator) observeMCPOAuthLoginCompleted(message map[string]any) (Result, bool) {
	params := lookupMap(message, "params")
	serverName := strings.TrimSpace(xutil.LookupStringFromAny(params["name"]))
	threadID := strings.TrimSpace(xutil.LookupStringFromAny(params["threadId"]))
	loginID := strings.TrimSpace(xutil.LookupStringFromAny(params["loginId"]))
	requestID := ""
	if serverName != "" && loginID != "" {
		for id, pending := range t.pendingMCPOAuthLogins {
			if pending.LoginID == loginID && pending.ServerName == serverName && (threadID == "" || pending.ThreadID == threadID) {
				if requestID != "" {
					return Result{Suppress: true}, true
				}
				requestID = id
			}
		}
	}
	if requestID == "" {
		// The notification may race the matching login response. Retain only
		// bounded, unassigned facts; the response must later prove the exact ID.
		if loginID != "" && serverName != "" && !t.completedMCPOAuthLoginIDs[loginID] && len(t.earlyMCPOAuthCompletions) < 64 {
			for _, pending := range t.pendingMCPOAuthLogins {
				if pending.LoginID == "" && pending.ServerName == serverName && (threadID == "" || pending.ThreadID == threadID) {
					if prior, exists := t.earlyMCPOAuthCompletions[loginID]; !exists {
						t.earlyMCPOAuthCompletions[loginID] = xutil.CloneMap(message)
					} else if prior != nil && !reflect.DeepEqual(prior, message) {
						t.earlyMCPOAuthCompletions[loginID] = nil
					}
					break
				}
			}
		}
		t.Debugf("observe mcp oauth login completion without verified attempt: server=%s thread=%s", serverName, threadID)
		return Result{Suppress: true}, true
	}
	pending := t.pendingMCPOAuthLogins[requestID]
	t.completedMCPOAuthLoginIDs[loginID] = true
	t.clearPendingMCPOAuthLogin(requestID, pending)
	success := xutil.LookupBoolFromAny(params["success"])
	errorText := strings.TrimSpace(xutil.LookupStringFromAny(params["error"]))
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
