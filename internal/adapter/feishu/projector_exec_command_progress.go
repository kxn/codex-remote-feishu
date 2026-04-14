package feishu

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func execCommandProgressBody(progress control.ExecCommandProgress) string {
	commands := normalizedExecProgressCommands(progress)
	if len(commands) == 0 {
		return "（暂无可显示命令）"
	}
	lines := make([]string, 0, len(commands))
	for _, command := range commands {
		lines = append(lines, "执行："+formatInlineCodeTextTag(truncateExecProgressSummary(command, 30)))
	}
	return strings.Join(lines, "\n")
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
