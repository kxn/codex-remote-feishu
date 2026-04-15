package feishu

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (p *Projector) projectExecCommandProgress(chatID string, event control.UIEvent) []Operation {
	if event.ExecCommandProgress == nil {
		return nil
	}
	progress := *event.ExecCommandProgress
	body := execCommandProgressBody(progress)
	operation := Operation{
		GatewayID:        event.GatewayID,
		SurfaceSessionID: event.SurfaceSessionID,
		ChatID:           chatID,
		MessageID:        progress.MessageID,
		ReplyToMessageID: event.SourceMessageID,
		CardTitle:        "处理中",
		CardBody:         body,
		CardThemeKey:     cardThemeInfo,
		CardUpdateMulti:  true,
		cardEnvelope:     cardEnvelopeV2,
		card:             rawCardDocument("处理中", body, cardThemeInfo, nil),
	}
	if strings.TrimSpace(progress.MessageID) != "" {
		operation.Kind = OperationUpdateCard
		operation.ReplyToMessageID = ""
	} else {
		operation.Kind = OperationSendCard
	}
	return []Operation{operation}
}

func execCommandProgressBody(progress control.ExecCommandProgress) string {
	entries := normalizedExecProgressEntries(progress)
	if len(entries) == 0 {
		return "（暂无可显示过程）"
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		lines = append(lines, renderExecProgressEntry(entry))
	}
	return strings.Join(lines, "\n")
}

func normalizedExecProgressEntries(progress control.ExecCommandProgress) []control.ExecCommandProgressEntry {
	entries := make([]control.ExecCommandProgressEntry, 0, len(progress.Entries))
	for _, entry := range progress.Entries {
		if normalized, ok := normalizeExecProgressEntry(entry); ok {
			entries = append(entries, normalized)
		}
	}
	if len(entries) > 0 {
		return entries
	}
	commands := normalizedExecProgressCommands(progress)
	if len(commands) == 0 {
		return nil
	}
	entries = make([]control.ExecCommandProgressEntry, 0, len(commands))
	for _, command := range commands {
		entries = append(entries, control.ExecCommandProgressEntry{
			Kind:    "command_execution",
			Label:   "执行",
			Summary: command,
		})
	}
	return entries
}

func normalizedExecProgressCommands(progress control.ExecCommandProgress) []string {
	commands := make([]string, 0, len(progress.Commands))
	for _, command := range progress.Commands {
		if normalized := normalizeExecProgressCommand(command); normalized != "" {
			commands = append(commands, normalized)
		}
	}
	if len(commands) > 0 {
		return commands
	}
	if normalized := normalizeExecProgressCommand(progress.Command); normalized != "" {
		return []string{normalized}
	}
	return nil
}

func normalizeExecProgressEntry(entry control.ExecCommandProgressEntry) (control.ExecCommandProgressEntry, bool) {
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Label = strings.TrimSpace(entry.Label)
	entry.Summary = strings.TrimSpace(entry.Summary)
	if entry.Kind == "command_execution" {
		entry.Summary = normalizeExecProgressCommand(entry.Summary)
		if entry.Label == "" {
			entry.Label = "执行"
		}
	} else if entry.Label == "" {
		switch entry.Kind {
		case "web_search":
			entry.Label = "搜索"
		case "mcp_tool_call":
			entry.Label = "MCP"
		case "dynamic_tool_call":
			entry.Label = "工具"
		default:
			entry.Label = "处理中"
		}
	}
	if entry.Summary == "" {
		return control.ExecCommandProgressEntry{}, false
	}
	return entry, true
}

func renderExecProgressEntry(entry control.ExecCommandProgressEntry) string {
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = "处理中"
	}
	switch strings.TrimSpace(entry.Kind) {
	case "command_execution":
		return label + "：" + formatInlineCodeTextTag(truncateExecProgressSummary(entry.Summary, 30))
	default:
		return label + "：" + truncateExecProgressSummary(entry.Summary, 40)
	}
}

var shellLCCommandPattern = regexp.MustCompile(`^(?:/usr/bin/|/bin/)?(?:bash|sh|zsh)\s+-lc\s+(.+)$`)

func normalizeExecProgressCommand(command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return ""
	}
	match := shellLCCommandPattern.FindStringSubmatch(command)
	if len(match) == 2 {
		command = strings.TrimSpace(match[1])
	}
	if len(command) >= 2 && command[0] == '"' && command[len(command)-1] == '"' {
		if unquoted, err := strconv.Unquote(command); err == nil {
			command = strings.TrimSpace(unquoted)
		}
	} else if len(command) >= 2 && command[0] == '\'' && command[len(command)-1] == '\'' {
		command = strings.TrimSpace(command[1 : len(command)-1])
	}
	return strings.Join(strings.Fields(command), " ")
}

func truncateExecProgressSummary(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 3 {
		limit = 3
	}
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit-3]) + "..."
}
