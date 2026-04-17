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
	lines := execCommandProgressLines(progress)
	if len(lines) == 0 {
		return nil
	}
	body := strings.Join(lines, "\n")
	elements := execCommandProgressElements(lines)
	operation := Operation{
		GatewayID:        event.GatewayID,
		SurfaceSessionID: event.SurfaceSessionID,
		ChatID:           chatID,
		MessageID:        progress.MessageID,
		CardTitle:        "工作中",
		CardBody:         body,
		CardThemeKey:     cardThemeProgress,
		CardElements:     elements,
		CardUpdateMulti:  true,
		cardEnvelope:     cardEnvelopeV2,
		card:             rawCardDocument("工作中", "", cardThemeProgress, elements),
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
	items := normalizedExecProgressTimeline(progress)
	lines := make([]string, 0, len(items)*2)
	var activeBlock execProgressTimelineBlock
	activeBlockOpen := false
	for _, item := range items {
		if item.entry != nil {
			lines = append(lines, renderExecProgressEntry(*item.entry))
			activeBlock = execProgressTimelineBlock{}
			activeBlockOpen = false
			continue
		}
		if item.row == nil {
			continue
		}
		if !activeBlockOpen || !sameExecProgressTimelineBlock(activeBlock, item.block) {
			lines = append(lines, renderExecProgressBlockHeader(control.ExecCommandProgressBlock{
				BlockID: item.block.BlockID,
				Kind:    item.block.Kind,
				Status:  item.block.Status,
			}))
			lines = append(lines, "  └ "+renderExecProgressBlockRow(*item.row))
			activeBlock = item.block
			activeBlockOpen = true
			continue
		}
		lines = append(lines, "    "+renderExecProgressBlockRow(*item.row))
	}
	if progress.TransientStatus != nil {
		if rendered := renderExecProgressTransientStatus(*progress.TransientStatus); rendered != "" {
			lines = append(lines, rendered)
		}
	}
	return lines
}

type execProgressTimelineBlock struct {
	BlockID string
	Kind    string
	Status  string
}

type execProgressTimelineItem struct {
	seq   int
	order int
	block execProgressTimelineBlock
	row   *control.ExecCommandProgressBlockRow
	entry *control.ExecCommandProgressEntry
}

func normalizedExecProgressTimeline(progress control.ExecCommandProgress) []execProgressTimelineItem {
	blocks := normalizedExecProgressBlocks(progress)
	entries := normalizedExecProgressEntries(progress)
	maxSeq := 0
	for _, block := range blocks {
		for _, row := range block.Rows {
			if row.LastSeq > maxSeq {
				maxSeq = row.LastSeq
			}
		}
	}
	for _, entry := range entries {
		if entry.LastSeq > maxSeq {
			maxSeq = entry.LastSeq
		}
	}
	nextFallbackSeq := maxSeq
	items := make([]execProgressTimelineItem, 0, len(entries))
	order := 0
	for _, block := range blocks {
		blockMeta := execProgressTimelineBlock{
			BlockID: block.BlockID,
			Kind:    block.Kind,
			Status:  block.Status,
		}
		for i := range block.Rows {
			row := block.Rows[i]
			seq := row.LastSeq
			if seq <= 0 {
				nextFallbackSeq++
				seq = nextFallbackSeq
				row.LastSeq = seq
			}
			rowCopy := row
			items = append(items, execProgressTimelineItem{
				seq:   seq,
				order: order,
				block: blockMeta,
				row:   &rowCopy,
			})
			order++
		}
	}
	for i := range entries {
		entry := entries[i]
		seq := entry.LastSeq
		if seq <= 0 {
			nextFallbackSeq++
			seq = nextFallbackSeq
			entry.LastSeq = seq
		}
		entryCopy := entry
		items = append(items, execProgressTimelineItem{
			seq:   seq,
			order: order,
			entry: &entryCopy,
		})
		order++
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].seq != items[j].seq {
			return items[i].seq < items[j].seq
		}
		return items[i].order < items[j].order
	})
	return items
}

func sameExecProgressTimelineBlock(left, right execProgressTimelineBlock) bool {
	return left.BlockID == right.BlockID && left.Kind == right.Kind && left.Status == right.Status
}

func normalizedExecProgressBlocks(progress control.ExecCommandProgress) []control.ExecCommandProgressBlock {
	blocks := make([]control.ExecCommandProgressBlock, 0, len(progress.Blocks))
	for _, block := range progress.Blocks {
		if normalized, ok := normalizeExecProgressBlock(block); ok {
			blocks = append(blocks, normalized)
		}
	}
	return blocks
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

func normalizeExecProgressBlock(block control.ExecCommandProgressBlock) (control.ExecCommandProgressBlock, bool) {
	block.BlockID = strings.TrimSpace(block.BlockID)
	block.Kind = strings.TrimSpace(block.Kind)
	block.Status = strings.TrimSpace(block.Status)
	rows := make([]control.ExecCommandProgressBlockRow, 0, len(block.Rows))
	for _, row := range block.Rows {
		if normalized, ok := normalizeExecProgressBlockRow(row); ok {
			rows = append(rows, normalized)
		}
	}
	block.Rows = rows
	if block.Kind == "" || len(block.Rows) == 0 {
		return control.ExecCommandProgressBlock{}, false
	}
	return block, true
}

func normalizeExecProgressBlockRow(row control.ExecCommandProgressBlockRow) (control.ExecCommandProgressBlockRow, bool) {
	row.RowID = strings.TrimSpace(row.RowID)
	row.Kind = strings.TrimSpace(row.Kind)
	row.Summary = strings.TrimSpace(row.Summary)
	row.Secondary = strings.TrimSpace(row.Secondary)
	items := make([]string, 0, len(row.Items))
	for _, item := range row.Items {
		if text := strings.TrimSpace(item); text != "" {
			items = append(items, text)
		}
	}
	row.Items = items
	switch row.Kind {
	case "read":
		if len(row.Items) == 0 {
			return control.ExecCommandProgressBlockRow{}, false
		}
	case "list", "search":
		if row.Summary == "" {
			return control.ExecCommandProgressBlockRow{}, false
		}
	default:
		if row.Summary == "" && len(row.Items) == 0 {
			return control.ExecCommandProgressBlockRow{}, false
		}
	}
	return row, true
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
		case "context_compaction":
			entry.Label = "整理"
		default:
			entry.Label = "工作中"
		}
	}
	if entry.Summary == "" {
		return control.ExecCommandProgressEntry{}, false
	}
	return entry, true
}

func renderExecProgressBlockHeader(block control.ExecCommandProgressBlock) string {
	switch strings.ToLower(strings.TrimSpace(block.Kind)) {
	case "exploration":
		if strings.EqualFold(strings.TrimSpace(block.Status), "running") {
			return "• 探索中"
		}
		return "• 已探索"
	default:
		return "• 工作中"
	}
}

func renderExecProgressBlockRow(row control.ExecCommandProgressBlockRow) string {
	switch strings.ToLower(strings.TrimSpace(row.Kind)) {
	case "read":
		return "读取 " + truncateExecProgressSummary(strings.Join(execProgressReadNames(row.Items), "、"), 60)
	case "list":
		return "列目录 " + truncateExecProgressSummary(row.Summary, 60)
	case "search":
		summary := row.Summary
		if row.Secondary != "" {
			summary = summary + "（范围：" + row.Secondary + "）"
		}
		return "搜索 " + truncateExecProgressSummary(summary, 60)
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
		names = append(names, name)
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
		return label + "：" + markdownCodeSpan(truncateExecProgressSummary(entry.Summary, 30))
	default:
		return label + "：" + truncateExecProgressSummary(entry.Summary, 40)
	}
}

func renderExecProgressTransientStatus(status control.ExecCommandProgressTransientStatus) string {
	text := strings.TrimSpace(status.Text)
	if text == "" {
		return ""
	}
	return "• " + text
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
