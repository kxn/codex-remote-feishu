package feishu

import (
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

func (p *Projector) projectExecCommandProgress(chatID string, event eventcontract.Event, progress control.ExecCommandProgress) []Operation {
	renderedLines := execCommandProgressRenderedLines(progress)
	if len(renderedLines) == 0 {
		return nil
	}
	window := execProgressCardWindow(progress, renderedLines)
	if len(window.Lines) == 0 {
		return nil
	}
	lines := execProgressRenderedContent(window.Lines)
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
		ProgressCardStartSeq: window.StartSeq,
		ProgressCardEndSeq:   window.EndSeq,
		cardEnvelope:         cardEnvelopeV2,
		card:                 rawCardDocument("工作中", "", cardThemeProgress, elements),
	}
	if messageID := strings.TrimSpace(activeExecCommandProgressSegmentMessageID(progress)); messageID != "" && !window.NewCard {
		op.Kind = OperationUpdateCard
		op.MessageID = messageID
	} else {
		op.Kind = OperationSendCard
		op = applyReplyLaneToNewOperation(event, op)
	}
	return []Operation{applyDetourHeaderToOperation(op, progress.DetourLabel)}
}

func activeExecCommandProgressSegmentMessageID(progress control.ExecCommandProgress) string {
	if strings.TrimSpace(progress.ActiveSegmentID) != "" {
		for _, segment := range progress.Segments {
			if strings.TrimSpace(segment.SegmentID) == strings.TrimSpace(progress.ActiveSegmentID) {
				return strings.TrimSpace(segment.MessageID)
			}
		}
	}
	if len(progress.Segments) == 0 {
		return ""
	}
	return strings.TrimSpace(progress.Segments[len(progress.Segments)-1].MessageID)
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
	return strings.TrimSpace(strings.TrimRight(line, " "))
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
		return nil
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
			case "delegated_task":
				item.Label = "Task"
			case "mcp_tool_call":
				item.Label = "MCP"
			case "dynamic_tool_call":
				item.Label = "工具"
			case "file_change":
				item.Label = "修改"
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

func renderExecProgressTimelineItem(item control.ExecCommandProgressTimelineItem, verbose bool, fileLabels map[string]string) string {
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
			Kind:       item.Kind,
			Label:      item.Label,
			Summary:    item.Summary,
			FileChange: item.FileChange,
		}, verbose, fileLabels)
	}
}

func renderExecProgressBlockRow(row control.ExecCommandProgressBlockRow) string {
	switch strings.ToLower(strings.TrimSpace(row.Kind)) {
	case "read":
		return execProgressPrefixedMarkdown("读取", strings.Join(execProgressReadNames(row.Items), "、"))
	case "list":
		return execProgressPrefixedMarkdown("列目录", renderExecProgressEntitySummary(row.Summary, 60))
	case "search":
		return execProgressPrefixedMarkdown("搜索", renderExecProgressSearchSummary(row.Summary, row.Secondary, 60))
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

func renderExecProgressEntry(entry control.ExecCommandProgressEntry, verbose bool, fileLabels map[string]string) string {
	label := strings.TrimSpace(entry.Label)
	if label == "" {
		label = "工作中"
	}
	switch strings.TrimSpace(entry.Kind) {
	case "command_execution":
		return execProgressPrefixedMarkdown(label, renderExecProgressEntitySummary(entry.Summary, 30))
	case "reasoning_summary":
		return strings.TrimSpace(entry.Summary)
	case "reasoning_placeholder":
		return strings.TrimSpace(entry.Summary)
	case "web_search":
		if label == "搜索" {
			return execProgressPrefixedMarkdown(label, renderExecProgressSearchSummary(entry.Summary, "", 40))
		}
		return execProgressPrefixedMarkdown(label, renderExecProgressEntitySummary(entry.Summary, 40))
	case "delegated_task":
		return execProgressPrefixedMarkdown(label, truncateExecProgressSummary(entry.Summary, 40))
	case "mcp_tool_call", "dynamic_tool_call":
		return execProgressPrefixedMarkdown(label, renderExecProgressEntitySummary(entry.Summary, 40))
	case "file_change":
		return renderExecProgressFileChangeEntry(entry, verbose, fileLabels)
	case "context_compaction":
		return execProgressPrefixedMarkdown(label, truncateExecProgressSummary(entry.Summary, 40))
	default:
		return execProgressPrefixedMarkdown(label, truncateExecProgressSummary(entry.Summary, 40))
	}
}

func renderExecProgressFileChangeEntry(entry control.ExecCommandProgressEntry, verbose bool, fileLabels map[string]string) string {
	if entry.FileChange == nil {
		return execProgressPrefixedMarkdown(firstNonEmpty(strings.TrimSpace(entry.Label), "修改"), renderExecProgressEntitySummary(entry.Summary, 40))
	}
	file := execProgressFileChangeSummaryEntry(*entry.FileChange)
	line := execProgressPrefixedMarkdown(firstNonEmpty(strings.TrimSpace(entry.Label), "修改"), renderExecProgressFileChangePathMarkdown(file, fileLabels)+"  "+formatFileChangeCountsMarkdown(file.AddedLines, file.RemovedLines))
	if !verbose {
		return line
	}
	diff := truncateExecProgressFileChangeDiff(strings.TrimSpace(entry.FileChange.Diff))
	if diff == "" {
		return line
	}
	return line + "\n" + markdownFencedCodeBlock("diff", diff)
}

func execProgressFileChangeDisplayLabels(items []control.ExecCommandProgressTimelineItem) map[string]string {
	files := make([]control.FileChangeSummaryEntry, 0, len(items))
	for _, item := range items {
		if item.FileChange == nil {
			continue
		}
		files = append(files, execProgressFileChangeSummaryEntry(*item.FileChange))
	}
	return fileChangeDisplayLabels(files)
}

func execProgressFileChangeSummaryEntry(change control.ExecCommandProgressFileChange) control.FileChangeSummaryEntry {
	return control.FileChangeSummaryEntry{
		Path:         strings.TrimSpace(change.Path),
		MovePath:     strings.TrimSpace(change.MovePath),
		AddedLines:   change.AddedLines,
		RemovedLines: change.RemovedLines,
	}
}

func truncateExecProgressFileChangeDiff(diff string) string {
	diff = strings.ReplaceAll(diff, "\r\n", "\n")
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return ""
	}
	const maxLines = 24
	const maxChars = 1200
	lines := strings.Split(diff, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	out := strings.Join(lines, "\n")
	if len([]rune(out)) > maxChars {
		runes := []rune(out)
		out = string(runes[:maxChars])
		truncated = true
	}
	out = strings.TrimRight(out, "\n")
	if truncated {
		out += "\n..."
	}
	return out
}

func execProgressPrefixedMarkdown(label, value string) string {
	label = strings.TrimSpace(label)
	value = strings.TrimSpace(value)
	labelMarkdown := execProgressMarkdownLabel(label)
	switch {
	case labelMarkdown == "":
		return value
	case value == "":
		return labelMarkdown
	default:
		return labelMarkdown + "：" + value
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

func renderExecProgressSearchSummary(summary, secondary string, limit int) string {
	summary = strings.TrimSpace(summary)
	secondary = strings.TrimSpace(secondary)
	if summary == "" {
		return ""
	}
	suffix := ""
	if secondary != "" {
		suffix = "（在 " + markdownCodeSpan(truncateExecProgressSummary(secondary, 24)) + " 内）"
	}
	if !shouldCodeSpanExecProgressSearchSummary(summary) {
		return truncateExecProgressSummary(summary+suffix, limit)
	}
	available := limit - len([]rune(suffix))
	if available <= 3 {
		available = 3
	}
	return markdownCodeSpan(truncateExecProgressSummary(summary, available)) + suffix
}

func shouldCodeSpanExecProgressSearchSummary(summary string) bool {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false
	}
	switch summary {
	case "正在搜索网络", "搜索完成":
		return false
	}
	return true
}

func renderExecProgressEntitySummary(summary string, limit int) string {
	summary = truncateExecProgressSummary(summary, limit)
	if summary == "" {
		return ""
	}
	return markdownCodeSpan(summary)
}

func renderExecProgressFileChangePathMarkdown(file control.FileChangeSummaryEntry, labels map[string]string) string {
	path := strings.TrimSpace(file.Path)
	movePath := strings.TrimSpace(file.MovePath)
	switch {
	case path != "" && movePath != "":
		return markdownCodeSpan(fileChangeDisplayLabel(path, labels)) + " -> " + markdownCodeSpan(fileChangeDisplayLabel(movePath, labels))
	case path != "":
		return markdownCodeSpan(fileChangeDisplayLabel(path, labels))
	case movePath != "":
		return markdownCodeSpan(fileChangeDisplayLabel(movePath, labels))
	default:
		return markdownCodeSpan("(unknown)")
	}
}

func markdownFencedCodeBlock(language, text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	fenceRun := maxBacktickRun(text) + 3
	fence := strings.Repeat("`", fenceRun)
	language = strings.TrimSpace(language)
	if language != "" {
		return fence + language + "\n" + text + "\n" + fence
	}
	return fence + "\n" + text + "\n" + fence
}
