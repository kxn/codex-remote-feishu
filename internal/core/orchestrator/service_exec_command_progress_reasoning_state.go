package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func reasoningEntriesFromProgress(progress *state.ExecCommandProgressRecord) []state.ExecCommandProgressEntryRecord {
	if progress == nil {
		return nil
	}
	entries := make([]state.ExecCommandProgressEntryRecord, 0, len(progress.Entries))
	for _, entry := range progress.Entries {
		if entry.Kind != "reasoning_summary" {
			continue
		}
		entries = append(entries, execprogress.CloneEntryRecord(entry))
	}
	return entries
}

func maxEntrySeq(entries []state.ExecCommandProgressEntryRecord) int {
	maxSeq := 0
	for _, entry := range entries {
		if entry.LastSeq > maxSeq {
			maxSeq = entry.LastSeq
		}
	}
	return maxSeq
}

func surfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.SurfaceReasoningProgressRecord {
	if surface == nil || surface.ActiveReasoning == nil {
		return nil
	}
	record := surface.ActiveReasoning
	if record.InstanceID != strings.TrimSpace(instanceID) || record.ThreadID != strings.TrimSpace(threadID) || record.TurnID != strings.TrimSpace(turnID) {
		return nil
	}
	return record
}

func ensureSurfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.SurfaceReasoningProgressRecord {
	if surface == nil {
		return nil
	}
	if record := surfaceReasoningProgress(surface, instanceID, threadID, turnID); record != nil {
		return record
	}
	surface.ActiveReasoning = &state.SurfaceReasoningProgressRecord{
		InstanceID: strings.TrimSpace(instanceID),
		ThreadID:   strings.TrimSpace(threadID),
		TurnID:     strings.TrimSpace(turnID),
	}
	return surface.ActiveReasoning
}

func clearSurfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) {
	if surface == nil {
		return
	}
	if surfaceReasoningProgress(surface, instanceID, threadID, turnID) != nil {
		surface.ActiveReasoning = nil
	}
}

func (s *Service) upsertSurfaceReasoningProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string, event agentproto.Event, backend agentproto.Backend, now time.Time) bool {
	record := ensureSurfaceReasoningProgress(surface, instanceID, threadID, turnID)
	if record == nil {
		return false
	}
	progress := &state.ExecCommandProgressRecord{
		Entries:         execprogress.CloneEntryRecords(record.Entries),
		Reasoning:       execprogress.CloneReasoningRecord(record.Reasoning),
		LastVisibleSeq:  maxEntrySeq(record.Entries),
		InstanceID:      record.InstanceID,
		ThreadID:        record.ThreadID,
		TurnID:          record.TurnID,
		ActiveSegmentID: "segment-1",
		Segments:        []state.ExecCommandProgressSegmentRecord{{SegmentID: "segment-1", StartSeq: 1}},
	}
	changed := execprogress.UpsertReasoning(progress, event, backend, now)
	record.Entries = reasoningEntriesFromProgress(progress)
	record.Reasoning = execprogress.CloneReasoningRecord(progress.Reasoning)
	return changed
}

func syncExecCommandProgressReasoning(progress *state.ExecCommandProgressRecord, record *state.SurfaceReasoningProgressRecord) {
	if progress == nil {
		return
	}
	if record == nil {
		execprogress.ReplaceReasoningEntries(progress, nil)
		progress.Reasoning = nil
		return
	}
	execprogress.ReplaceReasoningEntries(progress, record.Entries)
	progress.Reasoning = execprogress.CloneReasoningRecord(record.Reasoning)
}

func syncSurfaceReasoningProgressFromExec(surface *state.SurfaceConsoleRecord, progress *state.ExecCommandProgressRecord) {
	if surface == nil || progress == nil {
		return
	}
	record := surfaceReasoningProgress(surface, progress.InstanceID, progress.ThreadID, progress.TurnID)
	if record == nil {
		return
	}
	record.Entries = reasoningEntriesFromProgress(progress)
	record.Reasoning = execprogress.CloneReasoningRecord(progress.Reasoning)
}

func finalizeExecCommandProgressReasoning(progress *state.ExecCommandProgressRecord, status string) bool {
	if progress == nil {
		return false
	}
	status = strings.TrimSpace(status)
	if status == "" {
		status = "completed"
	}
	changed := false
	if progress.Reasoning != nil && progress.Reasoning.Active {
		progress.Reasoning.Active = false
		changed = true
	}
	for i := range progress.Entries {
		if progress.Entries[i].Kind != "reasoning_summary" {
			continue
		}
		if progress.Entries[i].Status != status {
			progress.Entries[i].Status = status
			changed = true
		}
	}
	return changed
}
