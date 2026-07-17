package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (s *Service) projectCapabilityStateUpdate(instanceID string, update agentproto.CapabilityStateUpdate) []eventcontract.Event {
	if update.MCPServerStartupStatus != nil {
		return s.projectMCPServerStartupStatus(instanceID, update)
	}
	if update.MCPOAuthLoginCompleted != nil {
		return s.projectMCPOAuthLoginCompleted(instanceID, update)
	}
	if update.AccountLoginCompleted != nil {
		return s.projectAccountLoginCompleted(instanceID, update)
	}
	return nil
}

func (s *Service) projectMCPServerStartupStatus(instanceID string, update agentproto.CapabilityStateUpdate) []eventcontract.Event {
	status := update.MCPServerStartupStatus
	if status == nil {
		return nil
	}
	statusValue := strings.ToLower(strings.TrimSpace(status.Status))
	failureReason := strings.TrimSpace(status.FailureReason)
	threadID := firstNonEmpty(status.ThreadID, update.ThreadID)
	serverName := firstNonEmpty(status.Name, "MCP server")
	if statusValue == "running" || statusValue == "ready" || statusValue == "started" {
		s.clearActiveNoticePrefix("mcp_startup", surfaceIDForThread(s, instanceID, threadID), instanceID, threadID, serverName)
		return nil
	}
	if statusValue != "failed" && !strings.EqualFold(failureReason, "reauthenticationRequired") {
		return nil
	}
	surface := s.turnSurface(instanceID, threadID, "")
	if surface == nil {
		return nil
	}
	if !s.allowActiveNotice("mcp_startup", surface.SurfaceSessionID, instanceID, threadID, mcpStartupDedupeKey(serverName, failureReason, statusValue), 30*time.Minute) {
		return nil
	}
	code := "codex_mcp_server_failed"
	text := fmt.Sprintf("MCP server %s 启动失败。", serverName)
	if strings.EqualFold(failureReason, "reauthenticationRequired") {
		code = "codex_mcp_server_reauth_required"
		text = fmt.Sprintf("MCP server %s 需要重新授权。请使用 /mcpoauth %s 重新登录。", serverName, serverName)
	} else if errText := strings.TrimSpace(status.Error); errText != "" {
		text = fmt.Sprintf("MCP server %s 启动失败：%s", serverName, errText)
	}
	payload := control.Notice{
		Code:     code,
		Title:    "MCP server status",
		Text:     text,
		ThemeKey: "warning",
	}
	return []eventcontract.Event{surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: payload},
		eventcontract.EventMeta{},
	)}
}

func mcpStartupDedupeKey(serverName, failureReason, statusValue string) string {
	return strings.Join([]string{
		strings.TrimSpace(serverName),
		strings.TrimSpace(failureReason),
		strings.TrimSpace(statusValue),
	}, " ")
}

func surfaceIDForThread(s *Service, instanceID, threadID string) string {
	if surface := s.turnSurface(instanceID, threadID, ""); surface != nil {
		return surface.SurfaceSessionID
	}
	return ""
}

func (s *Service) projectMCPOAuthLoginCompleted(instanceID string, update agentproto.CapabilityStateUpdate) []eventcontract.Event {
	completed := update.MCPOAuthLoginCompleted
	if completed == nil || completed.Success || strings.TrimSpace(completed.Error) == "" {
		return nil
	}
	threadID := firstNonEmpty(completed.ThreadID, update.ThreadID)
	surface := s.turnSurface(instanceID, threadID, "")
	if surface == nil {
		return nil
	}
	serverName := firstNonEmpty(completed.Name, "MCP server")
	if !s.allowActiveNotice("mcp_oauth_failed", surface.SurfaceSessionID, instanceID, threadID, serverName+" "+completed.Error, 30*time.Minute) {
		return nil
	}
	payload := control.Notice{
		Code:     "codex_mcp_oauth_login_failed",
		Title:    "MCP OAuth login failed",
		Text:     fmt.Sprintf("MCP server %s 授权失败：%s", serverName, strings.TrimSpace(completed.Error)),
		ThemeKey: "warning",
	}
	return []eventcontract.Event{surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: payload},
		eventcontract.EventMeta{},
	)}
}

func (s *Service) projectAccountLoginCompleted(instanceID string, update agentproto.CapabilityStateUpdate) []eventcontract.Event {
	completed := update.AccountLoginCompleted
	if completed == nil || completed.Success || strings.TrimSpace(completed.Error) == "" {
		return nil
	}
	threadID := strings.TrimSpace(update.ThreadID)
	surface := s.turnSurface(instanceID, threadID, "")
	if surface == nil {
		return nil
	}
	loginKey := firstNonEmpty(completed.LoginID, "account")
	if !s.allowActiveNotice("account_login_failed", surface.SurfaceSessionID, instanceID, threadID, loginKey+" "+completed.Error, 30*time.Minute) {
		return nil
	}
	payload := control.Notice{
		Code:     "codex_account_login_failed",
		Title:    "Codex account login failed",
		Text:     fmt.Sprintf("Codex 账号登录失败：%s", strings.TrimSpace(completed.Error)),
		ThemeKey: "warning",
	}
	return []eventcontract.Event{surfaceEventFromPayload(
		surface,
		eventcontract.NoticePayload{Notice: payload},
		eventcontract.EventMeta{},
	)}
}
