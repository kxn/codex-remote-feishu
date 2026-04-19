package feishu

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func (p *Projector) projectExecCommandProgress(chatID string, event control.UIEvent) []Operation {
	if event.ExecCommandProgress == nil {
		return nil
	}
	progress := *event.ExecCommandProgress
	renderedLines := execCommandProgressRenderedLines(progress)
	if len(renderedLines) == 0 {
		return nil
	}
	cardStartSeq := normalizeExecProgressCardStartSeq(progress, renderedLines)
	chunks := partitionExecProgressChunks(renderedLines, cardStartSeq)
	if len(chunks) == 0 {
		return nil
	}
	ops := make([]Operation, 0, len(chunks))
	for index, chunk := range chunks {
		lines := execProgressRenderedContent(chunk.Lines)
		elements := execCommandProgressElements(lines)
		op := Operation{
			GatewayID:            event.GatewayID,
			SurfaceSessionID:     event.SurfaceSessionID,
			ChatID:               chatID,
			CardTitle:            "工作中",
			CardBody:             strings.Join(lines, "\n"),
			CardThemeKey:         cardThemeProgress,
			CardElements:         elements,
			CardUpdateMulti:      true,
			ProgressCardStartSeq: chunk.StartSeq,
			cardEnvelope:         cardEnvelopeV2,
			card:                 rawCardDocument("工作中", "", cardThemeProgress, elements),
		}
		switch {
		case index == 0 && strings.TrimSpace(progress.MessageID) != "":
			op.Kind = OperationUpdateCard
			op.MessageID = progress.MessageID
		default:
			op.Kind = OperationSendCard
		}
		ops = append(ops, op)
	}
	return ops
}

func execCommandProgressBody(progress control.ExecCommandProgress) string {
	lines := execCommandProgressLines(progress)
	if len(lines) == 0 {
		return "（暂无可显示过程）"
	}
	return strings.Join(lines, "\n")
}

func execCommandProgressElements(lines []string) []map[string]any {
	elements := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		content := execCommandProgressMarkdownLine(line)
		if strings.TrimSpace(content) == "" {
			continue
		}
		elements = append(elements, map[string]any{
			"tag":     "markdown",
			"content": content,
		})
	}
	return elements
}

func execCommandProgressMarkdownLine(line string) string {
	line = strings.TrimRight(line, " ")
	switch {
	case strings.HasPrefix(line, "  └ "):
		return "└ " + strings.TrimSpace(strings.TrimPrefix(line, "  └ "))
	case strings.HasPrefix(line, "    "):
		return "· " + strings.TrimSpace(strings.TrimPrefix(line, "    "))
	default:
		return strings.TrimSpace(line)
	}
}

func execCommandProgressLines(progress control.ExecCommandProgress) []string {
	rendered := execCommandProgressRenderedLines(progress)
	lines := make([]string, 0, len(rendered))
	for _, line := range rendered {
		lines = append(lines, line.Content)
	}
	return lines
}

func normalizedExecProgressTimeline(progress control.ExecCommandProgress) []control.ExecCommandProgressTimelineItem {
	timeline := append([]control.ExecCommandProgressTimelineItem(nil), progress.Timeline...)
	if len(timeline) == 0 {
		timeline = control.BuildExecCommandProgressTimeline(progress)
	}
	items := make([]control.ExecCommandProgressTimelineItem, 0, len(timeline))
	for _, item := range timeline {
		if normalized, ok := normalizeExecProgressTimelineItem(item); ok {
			items = append(items, normalized)
		}
	}
	if len(items) == 0 {
		return nil
	}
	maxSeq := 0
	for _, item := range items {
		if item.LastSeq > maxSeq {
			maxSeq = item.LastSeq
		}
	}
	nextFallbackSeq := maxSeq
	for i := range items {
		if items[i].LastSeq > 0 {
			continue
		}
		nextFallbackSeq++
		items[i].LastSeq = nextFallbackSeq
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].LastSeq < items[j].LastSeq
	})
	return items
}

func normalizeExecProgressTimelineItem(item control.ExecCommandProgressTimelineItem) (control.ExecCommandProgressTimelineItem, bool) {
	item.ID = strings.TrimSpace(item.ID)
	item.Kind = strings.TrimSpace(item.Kind)
	item.Label = strings.TrimSpace(item.Label)
	item.Summary = strings.TrimSpace(item.Summary)
	item.Secondary = strings.TrimSpace(item.Secondary)
	item.Status = strings.TrimSpace(item.Status)
	trimmedItems := make([]string, 0, len(item.Items))
	for _, entry := range item.Items {
		if text := strings.TrimSpace(entry); text != "" {
			trimmedItems = append(trimmedItems, text)
		}
	}
	item.Items = trimmedItems
	switch item.Kind {
	case "read":
		if len(item.Items) == 0 {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	case "list", "search":
		if item.Summary == "" {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	case "command_execution":
		item.Summary = normalizeExecProgressCommand(item.Summary)
		if item.Label == "" {
			item.Label = "执行"
		}
		if item.Summary == "" {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	default:
		if item.Label == "" {
			switch item.Kind {
			case "web_search":
				item.Label = "搜索"
			case "mcp_tool_call":
				item.Label = "MCP"
			case "dynamic_tool_call":
				item.Label = "工具"
			case "context_compaction":
				item.Label = "压缩"
			default:
				item.Label = "工作中"
			}
		} else if item.Kind == "context_compaction" {
			item.Label = "压缩"
		}
		if item.Summary == "" {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	}
	return item, true
}

func renderExecProgressTimelineItem(item control.ExecCommandProgressTimelineItem) string {
	switch strings.ToLower(strings.TrimSpace(item.Kind)) {
	case "read", "list", "search":
		return renderExecProgressBlockRow(control.ExecCommandProgressBlockRow{
			Kind:      item.Kind,
			Items:     append([]string(nil), item.Items...),
			Summary:   item.Summary,
			Secondary: item.Secondary,
		})
	default:
		return renderExecProgressEntry(control.ExecCommandProgressEntry{
			Kind:    item.Kind,
			Label:   item.Label,
			Summary: item.Summary,
		})
	}
}

func renderExecProgressBlockRow(row control.ExecCommandProgressBlockRow) string {
	switch strings.ToLower(strings.TrimSpace(row.Kind)) {
	case "read":
		return execProgressMarkdownLabel("读取") + " " + strings.Join(execProgressReadNames(row.Items), "、")
	case "list":
		return execProgressMarkdownLabel("列目录") + " " + truncateExecProgressSummary(row.Summary, 60)
	case "search":
		summary := row.Summary
		if row.Secondary != "" {
			summary = summary + "（范围：" + row.Secondary + "）"
		}
		return execProgressMarkdownLabel("搜索") + " " + truncateExecProgressSummary(summary, 60)
	default:
		text := row.Summary
		if text == "" && len(row.Items) != 0 {
			text = strings.Join(row.Items, " ")
		}
		return truncateExecProgressSummary(text, 60)
	}
}

func execProgressReadNames(items []string) []string {
	names := make([]string, 0, len(items))
	seen := map[string]bool{}
	for _, item := range items {
		text := strings.TrimSpace(item)
		if text == "" {
			continue
		}
		name := filepath.Base(text)
		if name == "." || name == string(filepath.Separator) || strings.TrimSpace(name) == "" {
			name = text
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, markdownCodeSpan(name))
	}
	return names
}

func renderExecProgressEntry(entry control.ExecCommandProgressEntry) string {
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = "工作中"
	}
	switch strings.TrimSpace(entry.Kind) {
	case "command_execution":
		prefix := execProgressMarkdownLabel(label)
		return prefix + " " + markdownCodeSpan(truncateExecProgressSummary(entry.Summary, 30))
	case "reasoning_summary":
		return truncateExecProgressSummary(entry.Summary, 60)
	default:
		prefix := execProgressMarkdownLabel(label)
		return prefix + " " + truncateExecProgressSummary(entry.Summary, 40)
	}
}

func execProgressMarkdownLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return ""
	}
	return "**" + label + "**"
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
