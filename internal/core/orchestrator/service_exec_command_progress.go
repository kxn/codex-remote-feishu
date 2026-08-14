package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const execCommandProgressMinInterval = 300 * time.Millisecond
const execCommandProgressReasoningFlushInterval = time.Second

func (s *Service) handleProcessProgressItemStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		return s.handleAssistantMessageProgressStart(instanceID, event)
	case "command_execution":
		return s.handleCommandExecutionProgressStarted(instanceID, event)
	case "file_change":
		return s.handleFileChangeProgressStarted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressStarted(instanceID, event)
	case "delegated_task":
		return s.handleDelegatedTaskProgressUpdated(instanceID, event)
	case "mcp_tool_call":
		return s.handleMCPToolCallItemStarted(instanceID, event)
	case "dynamic_tool_call":
		return s.handleDynamicToolCallProgressStarted(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleProcessProgressItemDelta(instanceID string, event agentproto.Event) []eventcontract.Event {
	if strings.TrimSpace(event.Delta) == "" {
		return nil
	}
	switch strings.TrimSpace(event.ItemKind) {
	case "reasoning_summary":
		return s.handleReasoningSummaryProgressDelta(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleProcessProgressItemCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		return nil
	case "reasoning_summary":
		return s.handleReasoningSummaryProgressCompleted(instanceID, event)
	case "command_execution":
		return s.handleCommandExecutionProgressCompleted(instanceID, event)
	case "file_change":
		return s.handleFileChangeProgressCompleted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressCompleted(instanceID, event)
	case "delegated_task":
		return s.handleDelegatedTaskProgressUpdated(instanceID, event)
	case "mcp_tool_call":
		return s.handleMCPToolCallItemCompleted(instanceID, event)
	case "dynamic_tool_call":
		return s.handleDynamicToolCallProgressCompleted(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleCommandExecutionProgressStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	command, _ := execprogress.CommandMetadata(event)
	if command == "" && event.Exploration == nil {
		return nil
	}
	progress := s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	prevItemID := strings.TrimSpace(progress.ItemID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	status := execprogress.NormalizeStatus(event.Status, false)
	resolution := execprogress.ResolveExplorationProgressForCommandExecution(progress, event, false)
	switch resolution.Disposition {
	case execprogress.ExplorationDispositionStructured:
		progress.ItemID = execprogress.ExplorationBlockID
	case execprogress.ExplorationDispositionPending:
		return nil
	default:
		execprogress.UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
			ItemID:  progress.ItemID,
			Kind:    "command_execution",
			Label:   "执行",
			Summary: commandExecutionProgressSummary(command, status),
			Status:  status,
		})
	}
	if !resolution.Changed && prevItemID != "" && prevItemID == progress.ItemID && !progress.LastEmittedAt.IsZero() && s.now().Sub(progress.LastEmittedAt) < execCommandProgressMinInterval {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleWebSearchProgressStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	prevItemID := strings.TrimSpace(progress.ItemID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	entry := execprogress.WebSearchEntry(event.Metadata, false)
	entry.ItemID = progress.ItemID
	execprogress.UpsertEntry(progress, entry)
	if prevItemID != "" && prevItemID == progress.ItemID && !progress.LastEmittedAt.IsZero() && s.now().Sub(progress.LastEmittedAt) < execCommandProgressMinInterval {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleCommandExecutionProgressCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		if event.Exploration == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
			return nil
		}
		progress = s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	}
	command, _ := execprogress.CommandMetadata(event)
	itemID := strings.TrimSpace(event.ItemID)
	if itemID == "" {
		itemID = strings.TrimSpace(progress.ItemID)
	}
	if itemID != "" {
		progress.ItemID = itemID
	}
	status := execprogress.NormalizeStatus(event.Status, true)
	resolution := execprogress.ResolveExplorationProgressForCommandExecution(progress, event, true)
	if resolution.Disposition == execprogress.ExplorationDispositionStructured {
		progress.ItemID = execprogress.ExplorationBlockID
		if resolution.Changed && s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
			return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
		}
		return nil
	}
	if resolution.Disposition == execprogress.ExplorationDispositionPending || itemID == "" {
		return nil
	}
	hadEntry := execprogress.HasEntry(progress, itemID, "command_execution")
	if !hadEntry && !resolution.ForceGenericOnFinal {
		return nil
	}
	execprogress.UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  itemID,
		Kind:    "command_execution",
		Label:   "执行",
		Summary: commandExecutionProgressSummary(command, status),
		Status:  status,
	})
	if !hadEntry && s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
	}
	return nil
}

func (s *Service) handleWebSearchProgressCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil || !execprogress.HasEntry(progress, event.ItemID, "web_search") {
		return nil
	}
	if strings.TrimSpace(event.ItemID) != "" {
		progress.ItemID = strings.TrimSpace(event.ItemID)
	}
	entry := execprogress.WebSearchEntry(event.Metadata, true)
	entry.ItemID = progress.ItemID
	execprogress.UpsertEntry(progress, entry)
	if !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleDelegatedTaskProgressUpdated(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	entry := state.ExecCommandProgressEntryRecord{
		ItemID:  strings.TrimSpace(event.ItemID),
		Kind:    "delegated_task",
		Label:   "Task",
		Summary: strings.TrimSpace(xutil.MetadataString(event.Metadata, "description")),
		Status:  execprogress.NormalizeStatus(event.Status, event.Kind == agentproto.EventItemCompleted),
	}
	subagentType := strings.TrimSpace(xutil.MetadataString(event.Metadata, "subagentType"))
	switch {
	case entry.Summary != "" && subagentType != "":
		entry.Summary = subagentType + " · " + entry.Summary
	case entry.Summary == "" && subagentType != "":
		entry.Summary = subagentType
	case entry.Summary == "":
		entry.Summary = "任务处理中"
	}
	execprogress.UpsertEntry(progress, entry)
	progress.ItemID = entry.ItemID
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleDynamicToolCallProgressStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	resolution := execprogress.ResolveExplorationProgressForDynamicTool(progress, event, false)
	if resolution.Disposition == execprogress.ExplorationDispositionStructured {
		progress.ItemID = execprogress.ExplorationBlockID
		if !resolution.Changed {
			return nil
		}
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
	}
	if resolution.Disposition == execprogress.ExplorationDispositionPending {
		return nil
	}
	entry, groupKey, changed := execprogress.UpsertDynamicToolProgressEntry(progress, event)
	if !changed {
		return nil
	}
	progress.ItemID = groupKey
	execprogress.UpsertEntry(progress, entry)
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleDynamicToolCallProgressCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, instanceID, event.ThreadID, event.TurnID, event.ItemKind) {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		if event.Exploration == nil {
			return nil
		}
		progress = s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	}
	resolution := execprogress.ResolveExplorationProgressForDynamicTool(progress, event, true)
	if resolution.Disposition == execprogress.ExplorationDispositionStructured {
		progress.ItemID = execprogress.ExplorationBlockID
		if !resolution.Changed {
			return nil
		}
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
	}
	if resolution.Disposition == execprogress.ExplorationDispositionPending {
		return nil
	}
	entry, groupKey, changed := execprogress.UpsertDynamicToolProgressEntry(progress, event)
	if groupKey == "" || !changed {
		return nil
	}
	progress.ItemID = groupKey
	execprogress.UpsertEntry(progress, entry)
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func commandExecutionProgressSummary(command, status string) string {
	if command = strings.TrimSpace(command); command != "" {
		return command
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "failed":
		return "命令执行失败"
	case "completed":
		return "命令已完成"
	default:
		return "命令执行中"
	}
}

func (s *Service) finalizeExecCommandProgressForTurn(instanceID, threadID, turnID, turnStatus, finalText string) []eventcontract.Event {
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil {
		return nil
	}
	defer clearSurfaceReasoningProgress(surface, instanceID, threadID, turnID)
	if surface.ActiveExecProgress == nil {
		return nil
	}
	progress := surface.ActiveExecProgress
	if progress.InstanceID != instanceID || progress.ThreadID != threadID || progress.TurnID != turnID {
		return nil
	}
	defer s.terminateExecCommandProgressForTurn(instanceID, threadID, turnID)
	status := execprogress.NormalizeStatus(turnStatus, true)
	for i := range progress.Entries {
		if strings.TrimSpace(progress.Entries[i].Status) == "running" || strings.TrimSpace(progress.Entries[i].Status) == "started" {
			progress.Entries[i].Status = status
		}
	}
	finalizeExecCommandProgressReasoning(progress, status)
	execprogress.FinalizeExploration(progress, status)
	_ = finalText
	if status == "" {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, threadID, turnID, false)
}

func (s *Service) RecordExecCommandProgressSegment(surfaceID, threadID, turnID, itemID, messageID string) {
	s.RecordExecCommandProgressSegmentWindow(surfaceID, threadID, turnID, itemID, messageID, 0, 0)
}

func (s *Service) RecordExecCommandProgressSegmentWindow(surfaceID, threadID, turnID, itemID, messageID string, cardStartSeq, cardEndSeq int) {
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
	appended := false
	segment := ensureExecCommandProgressActiveSegment(progress)
	if segment == nil || (strings.TrimSpace(segment.MessageID) != "" && strings.TrimSpace(segment.MessageID) != strings.TrimSpace(messageID)) {
		segment = appendExecCommandProgressSegment(progress, cardStartSeq)
		appended = true
	}
	if segment == nil {
		return
	}
	segment.MessageID = strings.TrimSpace(messageID)
	if cardStartSeq > 0 {
		segment.StartSeq = cardStartSeq
	}
	if cardEndSeq > 0 {
		segment.EndSeq = cardEndSeq
	}
	if appended {
		execprogress.RolloverCarryoverEntries(progress, segment.StartSeq)
	}
}

func (s *Service) ClearExecCommandProgressSegmentMessage(surfaceID, threadID, turnID, itemID, messageID string) {
	if strings.TrimSpace(surfaceID) == "" || strings.TrimSpace(messageID) == "" {
		return
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
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
	for i := range progress.Segments {
		if strings.TrimSpace(progress.Segments[i].MessageID) != strings.TrimSpace(messageID) {
			continue
		}
		progress.Segments[i].MessageID = ""
	}
}

func (s *Service) emitExecCommandProgress(surface *state.SurfaceConsoleRecord, progress *state.ExecCommandProgressRecord, threadID, turnID string, final bool) []eventcontract.Event {
	if surface == nil || progress == nil {
		return nil
	}
	progress.Verbosity = state.NormalizeSurfaceVerbosity(surface.Verbosity)
	progress.LastEmittedAt = s.now()
	if progress.Reasoning != nil {
		progress.Reasoning.LastEmittedRevision = progress.Reasoning.Revision
	}
	sourceMessageID, _ := s.replyAnchorForTurn(progress.InstanceID, threadID, turnID)
	snapshot := execprogress.Snapshot(progress)
	if snapshot == nil {
		return nil
	}
	snapshot.TemporarySessionLabel = s.temporarySessionLabel(surface, progress.InstanceID, threadID, turnID)
	outbound := eventcontract.Event{
		Kind:                eventcontract.KindExecCommandProgress,
		SurfaceSessionID:    surface.SurfaceSessionID,
		SourceMessageID:     sourceMessageID,
		ExecCommandProgress: snapshot,
	}
	if strings.TrimSpace(sourceMessageID) != "" {
		outbound.Meta.MessageDelivery = eventcontract.ReplyThreadAppendOnlyDelivery()
	}
	return []eventcontract.Event{outbound}
}
