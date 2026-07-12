package daemon

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (a *App) handleMCPOAuthLoginDaemonCommand(command control.DaemonCommand) []eventcontract.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.shuttingDown {
		return nil
	}
	return a.handleMCPOAuthLoginDaemonCommandLocked(command)
}

func (a *App) handleMCPOAuthLoginDaemonCommandLocked(command control.DaemonCommand) []eventcontract.Event {
	surfaceID := strings.TrimSpace(command.SurfaceSessionID)
	serverName := mcpOAuthServerNameFromCommandText(command.Text)
	if serverName == "" {
		return mcpOAuthNotice(surfaceID, "mcp_oauth_usage", "请指定需要认证的 MCP 服务名，例如 `/mcpoauth github`。", "")
	}
	surface := a.service.Surface(surfaceID)
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return mcpOAuthNotice(surfaceID, "not_attached", "当前没有接管任何工作区。请先使用 `/list` 或 `/use` 接管一个实例。", "")
	}
	instanceID := strings.TrimSpace(surface.AttachedInstanceID)
	inst := a.service.Instance(instanceID)
	if inst == nil || !inst.Online {
		return mcpOAuthNotice(surfaceID, "mcp_oauth_instance_offline", "当前接管的本地 Codex 实例不在线，暂时不能发起 MCP 服务认证。", "")
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	commandID := a.nextCommandID()
	agentCommand := agentproto.Command{
		CommandID: commandID,
		Kind:      agentproto.CommandMCPOAuthLogin,
		Origin: agentproto.Origin{
			Surface:   surfaceID,
			ChatID:    surface.ChatID,
			MessageID: command.SourceMessageID,
		},
		Target: agentproto.Target{
			ThreadID: threadID,
		},
		MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{
			ServerName: serverName,
			ThreadID:   threadID,
		}},
	}
	a.pendingMCPOAuthLogins[commandID] = pendingMCPOAuthLogin{
		SurfaceSessionID: surfaceID,
		InstanceID:       instanceID,
		ThreadID:         threadID,
		ServerName:       serverName,
	}
	a.mu.Unlock()
	err := a.sendAgentCommand(instanceID, agentCommand)
	a.mu.Lock()
	if err != nil {
		delete(a.pendingMCPOAuthLogins, commandID)
		return mcpOAuthNotice(surfaceID, "mcp_oauth_dispatch_failed", "MCP 服务认证请求未成功发送到本地 Codex。", serverName)
	}
	return mcpOAuthNotice(surfaceID, "mcp_oauth_requested", "已向本地 Codex 请求生成 MCP 服务认证链接。", serverName)
}

func (a *App) handleMCPOAuthLoginCommandAckLocked(instanceID string, ack agentproto.CommandAck) ([]eventcontract.Event, bool) {
	commandID := strings.TrimSpace(ack.CommandID)
	pending, ok := a.pendingMCPOAuthLogins[commandID]
	if !ok {
		return nil, false
	}
	if ack.Accepted {
		return nil, true
	}
	delete(a.pendingMCPOAuthLogins, commandID)
	text := "本地 Codex 拒绝了这次 MCP 服务认证请求。"
	problemText := ""
	if ack.Problem != nil {
		problemText = ack.Problem.Error()
	}
	if errText := firstNonEmptyTrimmed(ack.Error, problemText); errText != "" {
		text = fmt.Sprintf("%s %s", text, errText)
	}
	return mcpOAuthNotice(pending.SurfaceSessionID, "mcp_oauth_rejected", text, pending.ServerName), true
}

func (a *App) handleMCPOAuthLoginEventLocked(instanceID string, event agentproto.Event) ([]eventcontract.Event, bool) {
	switch event.Kind {
	case agentproto.EventMCPOAuthLoginURLReady:
	case agentproto.EventMCPOAuthLoginCompleted:
	case agentproto.EventSystemError:
	default:
		return nil, false
	}
	commandID := strings.TrimSpace(event.CommandID)
	if commandID == "" && event.Problem != nil {
		commandID = strings.TrimSpace(event.Problem.CommandID)
	}
	if commandID == "" {
		return nil, event.Kind != agentproto.EventSystemError
	}
	pending, ok := a.pendingMCPOAuthLogins[commandID]
	if !ok {
		return nil, event.Kind != agentproto.EventSystemError
	}
	if pending.InstanceID != "" && pending.InstanceID != strings.TrimSpace(instanceID) {
		return nil, event.Kind != agentproto.EventSystemError
	}
	if event.Kind == agentproto.EventSystemError {
		delete(a.pendingMCPOAuthLogins, commandID)
		reason := ""
		if event.Problem != nil {
			reason = firstNonEmptyTrimmed(event.Problem.Details, event.Problem.Message, event.ErrorMessage)
		} else {
			reason = strings.TrimSpace(event.ErrorMessage)
		}
		return mcpOAuthFailureNotice(pending.SurfaceSessionID, pending.ServerName, reason), true
	}
	if event.MCPOAuthLogin == nil {
		return nil, true
	}
	if event.Kind == agentproto.EventMCPOAuthLoginURLReady {
		return mcpOAuthURLReadyNotice(pending.SurfaceSessionID, event.MCPOAuthLogin), true
	}
	delete(a.pendingMCPOAuthLogins, commandID)
	return mcpOAuthCompletedNotice(pending.SurfaceSessionID, event.MCPOAuthLogin), true
}

func mcpOAuthServerNameFromCommandText(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return ""
	}
	return strings.TrimSpace(fields[1])
}

func firstNonEmptyTrimmed(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func mcpOAuthNotice(surfaceID, code, text, serverName string) []eventcontract.Event {
	notice := control.Notice{
		Code:     code,
		Title:    "MCP 服务认证",
		Text:     text,
		ThemeKey: "info",
	}
	if strings.TrimSpace(serverName) != "" {
		notice.Sections = []control.FeishuCardTextSection{{
			Label: "服务",
			Lines: []string{strings.TrimSpace(serverName)},
		}}
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice:           &notice,
	}}
}

func mcpOAuthURLReadyNotice(surfaceID string, payload *agentproto.MCPOAuthLoginEvent) []eventcontract.Event {
	if payload == nil {
		return nil
	}
	notice := control.Notice{
		Code:     "mcp_oauth_url_ready",
		Title:    "MCP 服务认证",
		Text:     "请打开授权链接完成认证。认证完成后会自动收到结果提示。",
		ThemeKey: "info",
		Sections: []control.FeishuCardTextSection{
			{Label: "服务", Lines: []string{strings.TrimSpace(payload.ServerName)}},
			{Label: "授权链接", Lines: []string{strings.TrimSpace(payload.AuthorizationURL)}},
		},
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice:           &notice,
	}}
}

func mcpOAuthCompletedNotice(surfaceID string, payload *agentproto.MCPOAuthLoginEvent) []eventcontract.Event {
	if payload == nil {
		return nil
	}
	code := "mcp_oauth_completed"
	text := "MCP 服务认证已完成。"
	theme := "success"
	sections := []control.FeishuCardTextSection{{Label: "服务", Lines: []string{strings.TrimSpace(payload.ServerName)}}}
	if !payload.Success {
		code = "mcp_oauth_failed"
		text = "MCP 服务认证失败。"
		theme = "error"
		if errText := strings.TrimSpace(payload.Error); errText != "" {
			sections = append(sections, control.FeishuCardTextSection{Label: "原因", Lines: []string{errText}})
		}
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice: &control.Notice{
			Code:     code,
			Title:    "MCP 服务认证",
			Text:     text,
			ThemeKey: theme,
			Sections: sections,
		},
	}}
}

func mcpOAuthFailureNotice(surfaceID, serverName, reason string) []eventcontract.Event {
	sections := []control.FeishuCardTextSection{}
	if serverName = strings.TrimSpace(serverName); serverName != "" {
		sections = append(sections, control.FeishuCardTextSection{Label: "服务", Lines: []string{serverName}})
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		sections = append(sections, control.FeishuCardTextSection{Label: "原因", Lines: []string{reason}})
	}
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: strings.TrimSpace(surfaceID),
		Notice: &control.Notice{
			Code:     "mcp_oauth_failed",
			Title:    "MCP 服务认证",
			Text:     "MCP 服务认证失败。",
			ThemeKey: "error",
			Sections: sections,
		},
	}}
}
