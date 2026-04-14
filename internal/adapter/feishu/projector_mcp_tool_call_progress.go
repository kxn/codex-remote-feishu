package feishu

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func mcpToolCallProgressBody(progress control.MCPToolCallProgress) string {
	name := formatInlineCodeTextTag(formatMCPToolCallName(progress.Server, progress.Tool))
	lines := []string{mcpToolCallProgressPrefix(progress.Status) + "：" + name}
	if progress.Status == "completed" && progress.DurationMS > 0 {
		lines = append(lines, fmt.Sprintf("耗时：%d ms", progress.DurationMS))
	}
	if progress.Status == "failed" && strings.TrimSpace(progress.ErrorMessage) != "" {
		lines = append(lines, "原因："+strings.TrimSpace(progress.ErrorMessage))
	}
	return strings.Join(lines, "\n")
}

func mcpToolCallProgressTheme(progress control.MCPToolCallProgress) string {
	switch strings.ToLower(strings.TrimSpace(progress.Status)) {
	case "completed":
		return cardThemeSuccess
	case "failed":
		return cardThemeError
	default:
		return cardThemeInfo
	}
}

func mcpToolCallProgressPrefix(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed":
		return "完成"
	case "failed":
		return "失败"
	default:
		return "开始"
	}
}

func formatMCPToolCallName(server, tool string) string {
	server = strings.TrimSpace(server)
	tool = strings.TrimSpace(tool)
	switch {
	case server != "" && tool != "":
		return server + "." + tool
	case tool != "":
		return tool
	case server != "":
		return server
	default:
		return "未知 MCP 调用"
	}
}
