package control

import (
	"sort"
	"strings"
)

type execCommandProgressTimelineSortItem struct {
	item  ExecCommandProgressTimelineItem
	order int
}

func BuildExecCommandProgressTimeline(progress ExecCommandProgress) []ExecCommandProgressTimelineItem {
	items := make([]execCommandProgressTimelineSortItem, 0, len(progress.Entries))
	maxSeq := 0
	order := 0
	for _, block := range progress.Blocks {
		status := strings.TrimSpace(block.Status)
		for _, row := range block.Rows {
			item, ok := execCommandProgressTimelineItemFromBlockRow(row, status)
			if !ok {
				continue
			}
			if item.LastSeq > maxSeq {
				maxSeq = item.LastSeq
			}
			items = append(items, execCommandProgressTimelineSortItem{
				item:  item,
				order: order,
			})
			order++
		}
	}
	for _, entry := range progress.Entries {
		item, ok := execCommandProgressTimelineItemFromEntry(entry)
		if !ok {
			continue
		}
		if item.LastSeq > maxSeq {
			maxSeq = item.LastSeq
		}
		items = append(items, execCommandProgressTimelineSortItem{
			item:  item,
			order: order,
		})
		order++
	}
	if len(items) == 0 {
		for _, command := range execCommandProgressTimelineCommands(progress) {
			items = append(items, execCommandProgressTimelineSortItem{
				item: ExecCommandProgressTimelineItem{
					Kind:    "command_execution",
					Label:   "执行",
					Summary: command,
				},
				order: order,
			})
			order++
		}
	}
	nextFallbackSeq := maxSeq
	for i := range items {
		if items[i].item.LastSeq > 0 {
			continue
		}
		nextFallbackSeq++
		items[i].item.LastSeq = nextFallbackSeq
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].item.LastSeq != items[j].item.LastSeq {
			return items[i].item.LastSeq < items[j].item.LastSeq
		}
		return items[i].order < items[j].order
	})
	out := make([]ExecCommandProgressTimelineItem, 0, len(items))
	for _, item := range items {
		out = append(out, item.item)
	}
	return out
}

func execCommandProgressTimelineItemFromBlockRow(row ExecCommandProgressBlockRow, status string) (ExecCommandProgressTimelineItem, bool) {
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
			return ExecCommandProgressTimelineItem{}, false
		}
	case "list", "search":
		if row.Summary == "" {
			return ExecCommandProgressTimelineItem{}, false
		}
	default:
		if row.Summary == "" && len(row.Items) == 0 {
			return ExecCommandProgressTimelineItem{}, false
		}
	}
	return ExecCommandProgressTimelineItem{
		ID:        row.RowID,
		Kind:      row.Kind,
		Items:     append([]string(nil), row.Items...),
		Summary:   row.Summary,
		Secondary: row.Secondary,
		Status:    strings.TrimSpace(status),
		LastSeq:   row.LastSeq,
	}, true
}

func execCommandProgressTimelineItemFromEntry(entry ExecCommandProgressEntry) (ExecCommandProgressTimelineItem, bool) {
	entry.ItemID = strings.TrimSpace(entry.ItemID)
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Label = strings.TrimSpace(entry.Label)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Status = strings.TrimSpace(entry.Status)
	if entry.Summary == "" && entry.FileChange == nil {
		return ExecCommandProgressTimelineItem{}, false
	}
	return ExecCommandProgressTimelineItem{
		ID:         entry.ItemID,
		Kind:       entry.Kind,
		Label:      entry.Label,
		Summary:    entry.Summary,
		Status:     entry.Status,
		FileChange: entry.FileChange,
		LastSeq:    entry.LastSeq,
	}, true
}

func execCommandProgressTimelineCommands(progress ExecCommandProgress) []string {
	commands := make([]string, 0, len(progress.Commands))
	for _, command := range progress.Commands {
		if text := strings.TrimSpace(command); text != "" {
			commands = append(commands, text)
		}
	}
	if len(commands) > 0 {
		return commands
	}
	if text := strings.TrimSpace(progress.Command); text != "" {
		return []string{text}
	}
	return nil
}
