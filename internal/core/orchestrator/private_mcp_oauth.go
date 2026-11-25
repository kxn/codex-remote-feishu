package orchestrator

import (
	"errors"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type privateMCPOAuthRequest struct{ InstanceID, ThreadID, ServerName string }

func (s *Service) startPrivateMCPOAuth(surface *state.SurfaceConsoleRecord, a control.Action) []eventcontract.Event {
	fields := strings.Fields(a.Text)
	if len(fields) < 2 {
		return MCPOAuthNotice(surface.SurfaceSessionID, "mcp_oauth_usage", "请指定需要认证的 MCP 服务名，例如 `/mcpoauth github`。", "")
	}
	server := fields[1]
	if surface.AttachedInstanceID == "" {
		return MCPOAuthNotice(surface.SurfaceSessionID, "not_attached", "当前没有接管任何工作区。请先使用 `/list` 或 `/use` 接管一个实例。", "")
	}
	instance := s.root.Instances[surface.AttachedInstanceID]
	if instance == nil || !instance.Online {
		return MCPOAuthNotice(surface.SurfaceSessionID, "mcp_oauth_instance_offline", "当前接管的本地 Codex 实例不在线，暂时不能发起 MCP 服务认证。", "")
	}
	commandID := s.nextAgentCommandID()
	runtime := s.ensureSurfaceUIRuntime(surface)
	if runtime.PrivateMCPOAuth == nil {
		runtime.PrivateMCPOAuth = map[string]privateMCPOAuthRequest{}
	}
	runtime.PrivateMCPOAuth[commandID] = privateMCPOAuthRequest{instance.InstanceID, surface.SelectedThreadID, server}
	c := &agentproto.Command{CommandID: commandID, Kind: agentproto.CommandMCPOAuthLogin, Origin: agentproto.Origin{Surface: surface.SurfaceSessionID, ChatID: surface.ChatID, MessageID: a.MessageID}, Target: agentproto.Target{ThreadID: surface.SelectedThreadID}, MCP: agentproto.MCPCommand{OAuthLogin: &agentproto.MCPOAuthLoginCommand{ServerName: server, ThreadID: surface.SelectedThreadID}}}
	return []eventcontract.Event{{Kind: eventcontract.KindAgentCommand, GatewayID: surface.GatewayID, SurfaceSessionID: surface.SurfaceSessionID, SourceMessageID: a.MessageID, Command: c}}
}

func (s *Service) applyPrivateMCPOAuthEvent(instance string, event agentproto.Event) ([]eventcontract.Event, bool) {
	if !s.config.PrivateApplication {
		return nil, false
	}
	mcpEvent := event.Kind == agentproto.EventMCPOAuthLoginURLReady || event.Kind == agentproto.EventMCPOAuthLoginCompleted
	if !mcpEvent && event.Kind != agentproto.EventSystemError && event.Kind != agentproto.EventNativeCommandRejected {
		return nil, false
	}
	command := event.CommandID
	if command == "" && event.Problem != nil {
		command = event.Problem.CommandID
	}
	if command == "" {
		return nil, mcpEvent
	}
	for _, surface := range s.root.Surfaces {
		runtime := s.surfaceUIRuntimeState(surface)
		if runtime == nil {
			continue
		}
		pending, ok := runtime.PrivateMCPOAuth[command]
		if !ok || pending.InstanceID != instance {
			continue
		}
		if !mcpEvent {
			if event.Problem != nil && event.Problem.Code == "mcp_oauth_attempt_unverified" {
				delete(runtime.PrivateMCPOAuth, command)
				return MCPOAuthNotice(surface.SurfaceSessionID, "mcp_oauth_unverified", event.Problem.Message, ""), true
			}
			reason := event.ErrorMessage
			if reason == "" && event.Problem != nil {
				reason = event.Problem.Message
			}
			return s.HandlePrivateQueryFailure(surface.SurfaceSessionID, instance, command, errors.New(reason))
		}
		if event.MCPOAuthLogin == nil || event.MCPOAuthLogin.ServerName != pending.ServerName {
			return nil, true
		}
		if event.Kind == agentproto.EventMCPOAuthLoginURLReady {
			// A newly issued URL supersedes an older attempt for this exact
			// surface, carrier, thread and server. Retain unrelated attempts.
			for oldCommand, old := range runtime.PrivateMCPOAuth {
				if oldCommand != command && old == pending {
					delete(runtime.PrivateMCPOAuth, oldCommand)
				}
			}
			return MCPOAuthURLReadyNotice(surface.SurfaceSessionID, event.MCPOAuthLogin), true
		}
		delete(runtime.PrivateMCPOAuth, command)
		return MCPOAuthCompletedNotice(surface.SurfaceSessionID, event.MCPOAuthLogin), true
	}
	return nil, mcpEvent
}
