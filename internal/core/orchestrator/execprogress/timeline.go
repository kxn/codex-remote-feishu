package execprogress

import (
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type timelineSortItem struct {
	item  control.ExecCommandProgressTimelineItem
	order int
}

func Timeline(progress *state.ExecCommandProgressRecord) []control.ExecCommandProgressTimelineItem {
	if progress == nil {
		return nil
	}
	items := make([]timelineSortItem, 0, len(progress.Entries)+1)
	maxSeq := 0
	order := 0
	if progress.Exploration != nil {
		status := strings.TrimSpace(progress.Exploration.Block.Status)
		for _, row := range progress.Exploration.Block.Rows {
			item, ok := timelineItemFromBlockRow(row, status)
			if !ok {
				continue
			}
			if item.LastSeq > maxSeq {
				maxSeq = item.LastSeq
			}
			items = append(items, timelineSortItem{item: item, order: order})
			order++
		}
	}
	for _, entry := range visibleExecCommandProgressEntries(progress) {
		item, ok := timelineItemFromEntry(entry)
		if !ok {
			continue
		}
		if item.LastSeq > maxSeq {
			maxSeq = item.LastSeq
		}
		items = append(items, timelineSortItem{item: item, order: order})
		order++
	}
	if len(items) == 0 {
		return nil
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
	out := make([]control.ExecCommandProgressTimelineItem, 0, len(items))
	for _, item := range items {
		out = append(out, item.item)
	}
	return out
}

func timelineItemFromBlockRow(row state.ExecCommandProgressBlockRowRecord, status string) (control.ExecCommandProgressTimelineItem, bool) {
	rowID := strings.TrimSpace(row.RowID)
	kind := strings.TrimSpace(row.Kind)
	summary := strings.TrimSpace(row.Summary)
	secondary := strings.TrimSpace(row.Secondary)
	items := make([]string, 0, len(row.Items))
	for _, item := range row.Items {
		if text := strings.TrimSpace(item); text != "" {
			items = append(items, text)
		}
	}
	switch kind {
	case "read":
		if len(items) == 0 {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	case "list", "search":
		if summary == "" {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	default:
		if summary == "" && len(items) == 0 {
			return control.ExecCommandProgressTimelineItem{}, false
		}
	}
	return control.ExecCommandProgressTimelineItem{
		ID:        rowID,
		Kind:      kind,
		Items:     items,
		Summary:   summary,
		Secondary: secondary,
		Status:    strings.TrimSpace(status),
		LastSeq:   row.LastSeq,
	}, true
}

func timelineItemFromEntry(entry state.ExecCommandProgressEntryRecord) (control.ExecCommandProgressTimelineItem, bool) {
	itemID := strings.TrimSpace(entry.ItemID)
	kind := strings.TrimSpace(entry.Kind)
	label := strings.TrimSpace(entry.Label)
	summary := strings.TrimSpace(entry.Summary)
	status := strings.TrimSpace(entry.Status)
	if summary == "" && entry.FileChange == nil {
		return control.ExecCommandProgressTimelineItem{}, false
	}
	return control.ExecCommandProgressTimelineItem{
		ID:         itemID,
		Kind:       kind,
		Label:      label,
		Summary:    summary,
		Status:     status,
		FileChange: CloneFileChange(entry.FileChange),
		LastSeq:    entry.LastSeq,
	}, true
}
