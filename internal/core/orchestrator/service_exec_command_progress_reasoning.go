package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execCommandProgressTransientAnimationInterval = 1500 * time.Millisecond

func (s *Service) handleAssistantMessageProgressStart(instanceID string, event agentproto.Event) []control.UIEvent {
	return s.clearExecCommandProgressReasoning(instanceID, event.ThreadID, event.TurnID)
}

func (s *Service) handleReasoningSummaryProgressDelta(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || state.NormalizeSurfaceVerbosity(surface.Verbosity) != state.SurfaceVerbosityVerbose {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	if strings.TrimSpace(event.ItemID) != "" {
		progress.ItemID = strings.TrimSpace(event.ItemID)
	}
	if !upsertExecCommandProgressReasoning(progress, event, s.now()) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) clearExecCommandProgressReasoning(instanceID, threadID, turnID string) []control.UIEvent {
	surface := s.turnSurface(instanceID, threadID, turnID)
	progress := activeExecCommandProgress(surface, instanceID, threadID, turnID)
	if !clearExecCommandProgressReasoningRecord(progress) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, threadID, turnID, false)
}

func upsertExecCommandProgressReasoning(progress *state.ExecCommandProgressRecord, event agentproto.Event, now time.Time) bool {
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
		clearExecCommandProgressReasoningRecord(progress)
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
	text := normalizeExecCommandProgressReasoningText(extractFirstMarkdownBold(record.Buffer))
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
	upsertExecCommandProgressEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  record.ItemID,
		Kind:    "reasoning_summary",
		Summary: formatExecCommandProgressReasoningText(record.Text, record.AnimationStep),
		Status:  "running",
	})
	return true
}

func clearExecCommandProgressReasoningRecord(progress *state.ExecCommandProgressRecord) bool {
	if progress == nil {
		return false
	}
	itemID := ""
	if progress.Reasoning != nil {
		itemID = strings.TrimSpace(progress.Reasoning.ItemID)
		progress.Reasoning = nil
	}
	changed := false
	if removeExecCommandProgressEntry(progress, itemID, "reasoning_summary") {
		changed = true
	}
	if itemID == "" && removeExecCommandProgressEntry(progress, "", "reasoning_summary") {
		changed = true
	}
	return changed
}

func execCommandProgressHasVisibleReasoning(progress *state.ExecCommandProgressRecord) bool {
	return progress != nil &&
		progress.Reasoning != nil &&
		strings.TrimSpace(progress.Reasoning.Text) != "" &&
		progressHasEntry(progress, progress.Reasoning.ItemID, "reasoning_summary")
}

func removeExecCommandProgressEntry(progress *state.ExecCommandProgressRecord, itemID, kind string) bool {
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

func normalizeExecCommandProgressReasoningText(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimRight(text, ".")
	text = strings.TrimRight(text, "。")
	text = strings.TrimRight(text, "…")
	return strings.TrimSpace(text)
}

func formatExecCommandProgressReasoningText(text string, step int) string {
	text = normalizeExecCommandProgressReasoningText(text)
	if text == "" {
		return ""
	}
	dotCount := (step % 3) + 1
	return text + strings.Repeat(".", dotCount)
}
