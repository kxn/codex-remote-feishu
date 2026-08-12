package execprogress

import (
	"strconv"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func UpsertReasoning(progress *state.ExecCommandProgressRecord, event agentproto.Event, backend agentproto.Backend, now time.Time) bool {
	if progress == nil || strings.TrimSpace(event.Delta) == "" {
		return false
	}
	verbosity := state.NormalizeSurfaceVerbosity(progress.Verbosity)
	if verbosity != state.SurfaceVerbosityVerbose && verbosity != state.SurfaceVerbosityChatty {
		return false
	}
	entryItemID := reasoningEntryItemID(event.ItemID, xutil.LookupIntFromAny(event.Metadata["summaryIndex"]))
	record := progress.Reasoning
	if record == nil {
		record = &state.ExecCommandProgressReasoningRecord{}
		progress.Reasoning = record
	}
	previousVerbosity := state.NormalizeSurfaceVerbosity(record.VisibleVerbosity)
	modeChanged := record.VisibleVerbosity != "" && previousVerbosity != verbosity
	if modeChanged {
		freezeReasoningProjection(progress, record, previousVerbosity)
	}
	startSegment := modeChanged || strings.TrimSpace(record.ItemID) != entryItemID || record.VisibleAfterSeq != progress.LastVisibleSeq
	if verbosity == state.SurfaceVerbosityChatty && !startSegment {
		startSegment = reasoningEntrySeq(progress, record.VisibleEntryID) != progress.LastVisibleSeq
	}
	if startSegment {
		record.Buffer = ""
		record.BufferSummaryIndex = xutil.LookupIntFromAny(event.Metadata["summaryIndex"])
		record.VisibleSegment++
		record.VisibleEntryID = ""
	}
	record.ItemID = entryItemID
	record.VisibleVerbosity = verbosity
	record.Active = true
	summaryIndex := xutil.LookupIntFromAny(event.Metadata["summaryIndex"])
	if summaryIndex != record.BufferSummaryIndex {
		record.Buffer = ""
		record.BufferSummaryIndex = summaryIndex
	}
	record.Buffer += event.Delta
	text := extractReasoningSummaryText(record.Buffer, backend)
	if text == "" {
		return false
	}
	if strings.TrimSpace(record.Text) == text && record.VisibleSummaryIndex == summaryIndex && !startSegment {
		return false
	}
	record.Text = text
	record.VisibleSummaryIndex = summaryIndex
	record.LastUpdatedAt = now
	record.Revision++
	if verbosity == state.SurfaceVerbosityChatty {
		if record.VisibleEntryID == "" {
			record.VisibleEntryID = reasoningSegmentEntryID(record.ItemID, record.VisibleSegment)
		}
		UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
			ItemID:  record.VisibleEntryID,
			Kind:    "reasoning_summary",
			Summary: record.Text,
			Status:  "running",
		})
	}
	record.VisibleAfterSeq = progress.LastVisibleSeq
	return true
}

func UpdateReasoningCarrier(record **state.ExecCommandProgressReasoningRecord, event agentproto.Event, backend agentproto.Backend, now time.Time) bool {
	if record == nil || strings.TrimSpace(event.Delta) == "" {
		return false
	}
	entryItemID := reasoningEntryItemID(event.ItemID, xutil.LookupIntFromAny(event.Metadata["summaryIndex"]))
	current := *record
	if current == nil || strings.TrimSpace(current.ItemID) != entryItemID {
		current = &state.ExecCommandProgressReasoningRecord{ItemID: entryItemID}
		*record = current
	}
	current.Active = true
	summaryIndex := xutil.LookupIntFromAny(event.Metadata["summaryIndex"])
	if summaryIndex != current.BufferSummaryIndex {
		current.Buffer = ""
		current.BufferSummaryIndex = summaryIndex
	}
	current.Buffer += event.Delta
	text := extractReasoningSummaryText(current.Buffer, backend)
	if text == "" || (strings.TrimSpace(current.Text) == text && current.VisibleSummaryIndex == summaryIndex) {
		return false
	}
	current.Text = text
	current.VisibleSummaryIndex = summaryIndex
	current.LastUpdatedAt = now
	current.Revision++
	return true
}

func HideReasoningProjection(progress *state.ExecCommandProgressRecord) {
	if progress == nil || progress.Reasoning == nil {
		return
	}
	verbosity := state.NormalizeSurfaceVerbosity(progress.Reasoning.VisibleVerbosity)
	freezeReasoningProjection(progress, progress.Reasoning, verbosity)
	progress.Reasoning = nil
}

func reasoningEntrySeq(progress *state.ExecCommandProgressRecord, itemID string) int {
	if progress == nil || strings.TrimSpace(itemID) == "" {
		return 0
	}
	for i := len(progress.Entries) - 1; i >= 0; i-- {
		if progress.Entries[i].ItemID == itemID {
			return progress.Entries[i].LastSeq
		}
	}
	return 0
}

func reasoningSegmentEntryID(itemID string, segment int) string {
	if segment <= 0 {
		segment = 1
	}
	return strings.TrimSpace(itemID) + "::segment::" + strconv.Itoa(segment)
}

func FreezeReasoningForVisibleAction(progress *state.ExecCommandProgressRecord) {
	if progress == nil || state.NormalizeSurfaceVerbosity(progress.Verbosity) != state.SurfaceVerbosityChatty || progress.Reasoning == nil {
		return
	}
	entryID := strings.TrimSpace(progress.Reasoning.VisibleEntryID)
	if entryID == "" {
		return
	}
	for i := range progress.Entries {
		if progress.Entries[i].ItemID == entryID && progress.Entries[i].Status == "running" {
			progress.Entries[i].Status = "completed"
			return
		}
	}
}

func freezeReasoningProjection(progress *state.ExecCommandProgressRecord, record *state.ExecCommandProgressReasoningRecord, verbosity state.SurfaceVerbosity) {
	if progress == nil || record == nil || strings.TrimSpace(record.Text) == "" {
		return
	}
	switch verbosity {
	case state.SurfaceVerbosityChatty:
		for i := range progress.Entries {
			if progress.Entries[i].ItemID == record.VisibleEntryID {
				progress.Entries[i].Status = "completed"
				return
			}
		}
	case state.SurfaceVerbosityVerbose:
		record.VisibleSegment++
		entryID := reasoningSegmentEntryID(record.ItemID, record.VisibleSegment)
		UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
			ItemID:  entryID,
			Kind:    "reasoning_summary",
			Summary: strings.TrimSpace(record.Text),
			Status:  "completed",
		})
		record.VisibleEntryID = entryID
		record.VisibleAfterSeq = progress.LastVisibleSeq
	}
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

func extractReasoningSummaryText(value string, backend agentproto.Backend) string {
	if backend == agentproto.BackendCodex {
		if text := normalizeReasoningText(extractFirstMarkdownBold(value)); text != "" {
			return text
		}
	}
	return normalizeReasoningText(value)
}

func normalizeReasoningText(text string) string {
	return strings.TrimSpace(text)
}
