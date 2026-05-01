package execprogress

import (
	"strconv"
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
	summaryIndex := lookupIntFromAny(event.Metadata["summaryIndex"])
	entryItemID := reasoningEntryItemID(itemID, summaryIndex)
	record := progress.Reasoning
	if record == nil || strings.TrimSpace(record.ItemID) != entryItemID {
		record = &state.ExecCommandProgressReasoningRecord{ItemID: entryItemID}
		progress.Reasoning = record
	}
	record.ItemID = entryItemID
	if summaryIndex != record.BufferSummaryIndex {
		record.Buffer = ""
		record.BufferSummaryIndex = summaryIndex
	}
	record.Buffer += event.Delta
	text := extractReasoningSummaryText(record.Buffer)
	if text == "" {
		return false
	}
	if strings.TrimSpace(record.Text) == text && record.VisibleSummaryIndex == summaryIndex {
		return false
	}
	record.Text = text
	record.VisibleSummaryIndex = summaryIndex
	record.LastUpdatedAt = now
	UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  record.ItemID,
		Kind:    "reasoning_summary",
		Summary: record.Text,
		Status:  "running",
	})
	return true
}

func reasoningEntryItemID(itemID string, summaryIndex int) string {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		itemID = "reasoning_summary"
	}
	if summaryIndex <= 0 {
		return itemID
	}
	return itemID + "::summary::" + strconv.Itoa(summaryIndex)
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

func extractReasoningSummaryText(value string) string {
	if text := normalizeReasoningText(extractFirstMarkdownBold(value)); text != "" {
		return text
	}
	return normalizeReasoningText(value)
}

func normalizeReasoningText(text string) string {
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
