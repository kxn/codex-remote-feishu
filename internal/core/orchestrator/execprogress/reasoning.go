package execprogress

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func UpsertReasoning(progress *state.ExecCommandProgressRecord, event agentproto.Event, now time.Time) bool {
	if progress == nil || strings.TrimSpace(event.Delta) == "" {
		return false
	}
	itemID := strings.TrimSpace(event.ItemID)
	if itemID == "" && progress.Reasoning != nil {
		itemID = strings.TrimSpace(progress.Reasoning.ItemID)
	}
	if itemID == "" {
		itemID = "reasoning_summary"
	}
	if progress.Reasoning != nil && strings.TrimSpace(progress.Reasoning.ItemID) != "" && progress.Reasoning.ItemID != itemID {
		ClearReasoningRecord(progress)
	}
	record := progress.Reasoning
	if record == nil {
		record = &state.ExecCommandProgressReasoningRecord{ItemID: itemID}
		progress.Reasoning = record
	}
	record.ItemID = itemID
	summaryIndex := lookupIntFromAny(event.Metadata["summaryIndex"])
	if summaryIndex != record.BufferSummaryIndex {
		record.Buffer = ""
		record.BufferSummaryIndex = summaryIndex
	}
	record.Buffer += event.Delta
	text := normalizeReasoningText(extractFirstMarkdownBold(record.Buffer))
	if text == "" {
		return false
	}
	if strings.TrimSpace(record.Text) == text && record.VisibleSummaryIndex == summaryIndex {
		return false
	}
	record.Text = text
	record.VisibleSummaryIndex = summaryIndex
	record.AnimationStep = 0
	record.LastAnimatedAt = now
	UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  record.ItemID,
		Kind:    "reasoning_summary",
		Summary: FormatReasoningText(record.Text, record.AnimationStep),
		Status:  "running",
	})
	return true
}

func ClearReasoningRecord(progress *state.ExecCommandProgressRecord) bool {
	if progress == nil {
		return false
	}
	itemID := ""
	if progress.Reasoning != nil {
		itemID = strings.TrimSpace(progress.Reasoning.ItemID)
		progress.Reasoning = nil
	}
	changed := false
	if removeEntry(progress, itemID, "reasoning_summary") {
		changed = true
	}
	if itemID == "" && removeEntry(progress, "", "reasoning_summary") {
		changed = true
	}
	return changed
}

func HasVisibleReasoning(progress *state.ExecCommandProgressRecord) bool {
	return progress != nil &&
		progress.Reasoning != nil &&
		strings.TrimSpace(progress.Reasoning.Text) != "" &&
		HasEntry(progress, progress.Reasoning.ItemID, "reasoning_summary")
}

func FormatReasoningText(text string, step int) string {
	text = normalizeReasoningText(text)
	if text == "" {
		return ""
	}
	dotCount := (step % 3) + 1
	return text + strings.Repeat(".", dotCount)
}

func removeEntry(progress *state.ExecCommandProgressRecord, itemID, kind string) bool {
	if progress == nil || len(progress.Entries) == 0 {
		return false
	}
	itemID = strings.TrimSpace(itemID)
	kind = strings.TrimSpace(kind)
	changed := false
	filtered := progress.Entries[:0]
	for _, entry := range progress.Entries {
		match := true
		if itemID != "" && entry.ItemID != itemID {
			match = false
		}
		if kind != "" && entry.Kind != kind {
			match = false
		}
		if match {
			changed = true
			continue
		}
		filtered = append(filtered, entry)
	}
	progress.Entries = filtered
	return changed
}

func extractFirstMarkdownBold(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for i := 0; i+1 < len(value); i++ {
		if value[i] != '*' || value[i+1] != '*' {
			continue
		}
		start := i + 2
		for j := start; j+1 < len(value); j++ {
			if value[j] == '*' && value[j+1] == '*' {
				return strings.TrimSpace(value[start:j])
			}
		}
		return ""
	}
	return ""
}

func normalizeReasoningText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimRight(text, ".")
	text = strings.TrimRight(text, "。")
	text = strings.TrimRight(text, "…")
	return strings.TrimSpace(text)
}

func lookupIntFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float32:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}
