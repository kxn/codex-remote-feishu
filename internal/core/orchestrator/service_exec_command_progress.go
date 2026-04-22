package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execCommandProgressMinInterval = 300 * time.Millisecond

func (s *Service) handleProcessProgressItemStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		return s.handleAssistantMessageProgressStart(instanceID, event)
	case "command_execution":
		return s.handleCommandExecutionProgressStarted(instanceID, event)
	case "file_change":
		return s.handleFileChangeProgressStarted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressStarted(instanceID, event)
	case "mcp_tool_call":
		return s.handleMCPToolCallItemStarted(instanceID, event)
	case "dynamic_tool_call":
		return s.handleDynamicToolCallProgressStarted(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleProcessProgressItemDelta(instanceID string, event agentproto.Event) []control.UIEvent {
	if strings.TrimSpace(event.Delta) == "" {
		return nil
	}
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		events := s.clearExecCommandProgressReasoning(instanceID, event.ThreadID, event.TurnID)
		s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		return events
	case "reasoning_summary":
		return s.handleReasoningSummaryProgressDelta(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) tickExecCommandProgressAnimations(surface *state.SurfaceConsoleRecord, now time.Time) []control.UIEvent {
	if surface == nil || surface.ActiveExecProgress == nil {
		return nil
	}
	progress := surface.ActiveExecProgress
	if strings.TrimSpace(progress.MessageID) == "" || !execCommandProgressHasVisibleReasoning(progress) {
		return nil
	}
	record := progress.Reasoning
	if record == nil {
		return nil
	}
	if !record.LastAnimatedAt.IsZero() && now.Sub(record.LastAnimatedAt) < execCommandProgressTransientAnimationInterval {
		return nil
	}
	if !progress.LastEmittedAt.IsZero() && now.Sub(progress.LastEmittedAt) < execCommandProgressTransientAnimationInterval {
		return nil
	}
	record.AnimationStep = (record.AnimationStep + 1) % 3
	record.LastAnimatedAt = now
	upsertExecCommandProgressEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  record.ItemID,
		Kind:    "reasoning_summary",
		Summary: formatExecCommandProgressReasoningText(record.Text, record.AnimationStep),
		Status:  "running",
	})
	return s.emitExecCommandProgress(surface, progress, progress.ThreadID, progress.TurnID, false)
}

func (s *Service) handleProcessProgressItemCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		events := s.clearExecCommandProgressReasoning(instanceID, event.ThreadID, event.TurnID)
		if s.eventCarriesAssistantText(instanceID, event) {
			s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		}
		return events
	case "command_execution":
		return s.handleCommandExecutionProgressCompleted(instanceID, event)
	case "file_change":
		return s.handleFileChangeProgressCompleted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressCompleted(instanceID, event)
	case "mcp_tool_call":
		return s.handleMCPToolCallItemCompleted(instanceID, event)
	case "dynamic_tool_call":
		return s.handleDynamicToolCallProgressCompleted(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleCommandExecutionProgressStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	command, cwd := execCommandMetadata(event)
	if command == "" {
		return nil
	}
	progress := s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	prevItemID := strings.TrimSpace(progress.ItemID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	progress.Command = command
	progress.Commands = appendExecCommandHistory(progress.Commands, command)
	if strings.TrimSpace(cwd) != "" {
		progress.CWD = cwd
	}
	progress.Status = normalizeExecCommandProgressStatus(event.Status, false)
	explorationChanged := false
	if changed, ok := upsertExplorationProgressForCommandExecution(progress, event, false); ok {
		explorationChanged = changed
		progress.ItemID = execProgressExplorationBlockID
	} else {
		upsertExecCommandProgressEntry(progress, state.ExecCommandProgressEntryRecord{
			ItemID:  progress.ItemID,
			Kind:    "command_execution",
			Label:   "执行",
			Summary: command,
			Status:  progress.Status,
		})
	}
	if !explorationChanged && prevItemID != "" && prevItemID == progress.ItemID && !progress.LastEmittedAt.IsZero() && s.now().Sub(progress.LastEmittedAt) < execCommandProgressMinInterval {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleWebSearchProgressStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	prevItemID := strings.TrimSpace(progress.ItemID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	entry := webSearchProgressEntry(event.Metadata, false)
	entry.ItemID = progress.ItemID
	upsertExecCommandProgressEntry(progress, entry)
	if prevItemID != "" && prevItemID == progress.ItemID && !progress.LastEmittedAt.IsZero() && s.now().Sub(progress.LastEmittedAt) < execCommandProgressMinInterval {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleCommandExecutionProgressCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	command, cwd := execCommandMetadata(event)
	if strings.TrimSpace(event.ItemID) != "" {
		progress.ItemID = strings.TrimSpace(event.ItemID)
	}
	if command != "" {
		progress.Command = command
	}
	if strings.TrimSpace(cwd) != "" {
		progress.CWD = cwd
	}
	progress.Status = normalizeExecCommandProgressStatus(event.Status, true)
	if changed, ok := upsertExplorationProgressForCommandExecution(progress, event, true); ok {
		progress.ItemID = execProgressExplorationBlockID
		if changed && s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
			return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
		}
		return nil
	}
	if !progressHasEntry(progress, event.ItemID, "command_execution") {
		return nil
	}
	upsertExecCommandProgressEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  strings.TrimSpace(event.ItemID),
		Kind:    "command_execution",
		Label:   "执行",
		Summary: firstNonEmpty(command, progress.Command),
		Status:  progress.Status,
	})
	return nil
}

func (s *Service) handleWebSearchProgressCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil || !progressHasEntry(progress, event.ItemID, "web_search") {
		return nil
	}
	if strings.TrimSpace(event.ItemID) != "" {
		progress.ItemID = strings.TrimSpace(event.ItemID)
	}
	entry := webSearchProgressEntry(event.Metadata, true)
	entry.ItemID = progress.ItemID
	upsertExecCommandProgressEntry(progress, entry)
	if !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleDynamicToolCallProgressStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if changed, ok := upsertExplorationProgressForDynamicTool(progress, event, false); ok {
		progress.ItemID = execProgressExplorationBlockID
		if !changed {
			return nil
		}
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
	}
	entry, groupKey, changed := upsertDynamicToolProgressEntry(progress, event)
	if !changed {
		return nil
	}
	progress.ItemID = groupKey
	upsertExecCommandProgressEntry(progress, entry)
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleDynamicToolCallProgressCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	if changed, ok := upsertExplorationProgressForDynamicTool(progress, event, true); ok {
		progress.ItemID = execProgressExplorationBlockID
		if !changed {
			return nil
		}
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
	}
	entry, groupKey, changed := upsertDynamicToolProgressEntry(progress, event)
	if groupKey == "" || !changed {
		return nil
	}
	progress.ItemID = groupKey
	upsertExecCommandProgressEntry(progress, entry)
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) finalizeExecCommandProgressForTurn(instanceID, threadID, turnID, turnStatus, finalText string) []control.UIEvent {
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil || surface.ActiveExecProgress == nil {
		return nil
	}
	progress := surface.ActiveExecProgress
	if progress.InstanceID != instanceID || progress.ThreadID != threadID || progress.TurnID != turnID {
		return nil
	}
	defer s.terminateExecCommandProgressForTurn(instanceID, threadID, turnID)
	_ = turnStatus
	_ = finalText
	if !clearExecCommandProgressReasoningRecord(progress) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, threadID, turnID, false)
}

func (s *Service) RecordExecCommandProgressMessage(surfaceID, threadID, turnID, itemID, messageID string) {
	s.RecordExecCommandProgressMessageStartSeq(surfaceID, threadID, turnID, itemID, messageID, 0)
}

func (s *Service) RecordExecCommandProgressMessageStartSeq(surfaceID, threadID, turnID, itemID, messageID string, cardStartSeq int) {
	if strings.TrimSpace(surfaceID) == "" || strings.TrimSpace(messageID) == "" {
		return
	}
	surface := s.root.Surfaces[surfaceID]
	if surface == nil || surface.ActiveExecProgress == nil {
		return
	}
	progress := surface.ActiveExecProgress
	if progress.ThreadID != strings.TrimSpace(threadID) || progress.TurnID != strings.TrimSpace(turnID) {
		return
	}
	if strings.TrimSpace(itemID) != "" && progress.ItemID != strings.TrimSpace(itemID) {
		return
	}
	progress.MessageID = strings.TrimSpace(messageID)
	if cardStartSeq > 0 {
		progress.CardStartSeq = cardStartSeq
	}
}

func (s *Service) emitExecCommandProgress(surface *state.SurfaceConsoleRecord, progress *state.ExecCommandProgressRecord, threadID, turnID string, final bool) []control.UIEvent {
	if surface == nil || progress == nil {
		return nil
	}
	progress.Verbosity = state.NormalizeSurfaceVerbosity(surface.Verbosity)
	progress.LastEmittedAt = s.now()
	sourceMessageID, _ := s.replyAnchorForTurn(progress.InstanceID, threadID, turnID)
	snapshot := ExecCommandProgressSnapshot(progress)
	if snapshot == nil {
		return nil
	}
	snapshot.Final = final
	return []control.UIEvent{{
		Kind:                control.UIEventExecCommandProgress,
		SurfaceSessionID:    surface.SurfaceSessionID,
		SourceMessageID:     sourceMessageID,
		ExecCommandProgress: snapshot,
	}}
}
