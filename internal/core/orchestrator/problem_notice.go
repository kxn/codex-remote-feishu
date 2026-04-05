package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const debugErrorNoticeCode = "debug_error"

func NoticeForProblem(problem agentproto.ErrorInfo) control.Notice {
	problem = problem.Normalize()
	title := "链路错误"
	if location := problemLocation(problem); location != "" {
		title += " · " + location
	}

	lines := make([]string, 0, 10)
	if problem.Layer != "" {
		lines = append(lines, "层：`"+problem.Layer+"`")
	}
	if problem.Stage != "" {
		lines = append(lines, "位置：`"+problem.Stage+"`")
	}
	if problem.Operation != "" {
		lines = append(lines, "操作：`"+problem.Operation+"`")
	}
	if problem.CommandID != "" {
		lines = append(lines, "命令：`"+problem.CommandID+"`")
	}
	if problem.ThreadID != "" {
		lines = append(lines, "会话：`"+problem.ThreadID+"`")
	}
	if problem.TurnID != "" {
		lines = append(lines, "Turn：`"+problem.TurnID+"`")
	}
	if problem.Code != "" {
		lines = append(lines, "错误码：`"+problem.Code+"`")
	}
	if problem.Retryable {
		lines = append(lines, "可重试：是")
	}

	if problem.Message != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "摘要："+problem.Message)
	}
	if details := problemDetails(problem); details != "" {
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, "调试信息：\n```text\n"+details+"\n```")
	}
	if len(lines) == 0 {
		lines = append(lines, "发生了未分类的链路错误。")
	}

	return control.Notice{
		Code:     debugErrorNoticeCode,
		Title:    title,
		Text:     strings.TrimSpace(strings.Join(lines, "\n")),
		ThemeKey: "system",
	}
}

func problemLocation(problem agentproto.ErrorInfo) string {
	switch {
	case problem.Layer != "" && problem.Stage != "":
		return problem.Layer + "." + problem.Stage
	case problem.Layer != "":
		return problem.Layer
	case problem.Stage != "":
		return problem.Stage
	default:
		return ""
	}
}

func problemDetails(problem agentproto.ErrorInfo) string {
	details := strings.TrimSpace(problem.Details)
	if details == "" || details == strings.TrimSpace(problem.Message) {
		return ""
	}
	const maxLen = 1500
	if len(details) <= maxLen {
		return details
	}
	return fmt.Sprintf("%s\n...(%d bytes truncated)", details[:maxLen], len(details)-maxLen)
}
