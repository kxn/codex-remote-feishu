package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"strings"
)

func MCPOAuthNotice(surfaceID, code, text, serverName string) []eventcontract.Event {
	notice := control.Notice{
		Code:     code,
		Title:    "MCP 服务认证",
		Text:     text,
		ThemeKey: "info",
	}
	if strings.TrimSpace(serverName) != "" {
		notice.Sections = []control.FeishuCardTextSection{{
			Label: "状态",
			Lines: []string{strings.TrimSpace(text)},
		}, {
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

func MCPOAuthURLReadyNotice(surfaceID string, payload *agentproto.MCPOAuthLoginEvent) []eventcontract.Event {
	if payload == nil {
		return nil
	}
	notice := control.Notice{
		Code:     "mcp_oauth_url_ready",
		Title:    "MCP 服务认证",
		Text:     "请打开授权链接完成认证。认证完成后会自动收到结果提示。",
		ThemeKey: "info",
		Sections: []control.FeishuCardTextSection{
			{Label: "状态", Lines: []string{"请打开授权链接完成认证。认证完成后会自动收到结果提示。"}},
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

func MCPOAuthCompletedNotice(surfaceID string, payload *agentproto.MCPOAuthLoginEvent) []eventcontract.Event {
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
	sections = append([]control.FeishuCardTextSection{{Label: "状态", Lines: []string{text}}}, sections...)
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

func MCPOAuthFailureNotice(surfaceID, serverName, reason string) []eventcontract.Event {
	sections := []control.FeishuCardTextSection{{Label: "状态", Lines: []string{"MCP 服务认证失败。"}}}
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
