package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execCommandProgressMinInterval = 300 * time.Millisecond

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
	case "process_plan":
		return s.handleProcessPlanProgressUpdated(instanceID, event)
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
	case "agent_message":
		s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		return nil
	case "reasoning_summary":
		return s.handleReasoningSummaryProgressDelta(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleProcessProgressItemCompleted(instanceID string, event agentproto.Event) []eventcontract.Event {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		if s.eventCarriesAssistantText(instanceID, event) {
			s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		}
		return nil
	case "command_execution":
		return s.handleCommandExecutionProgressCompleted(instanceID, event)
	case "file_change":
		return s.handleFileChangeProgressCompleted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressCompleted(instanceID, event)
	case "process_plan":
		return s.handleProcessPlanProgressUpdated(instanceID, event)
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
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	command, cwd := execprogress.CommandMetadata(event)
	if command == "" {
		return nil
	}
	progress := s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	prevItemID := strings.TrimSpace(progress.ItemID)
	progress.ItemID = strings.TrimSpace(event.ItemID)
	progress.Command = command
	progress.Commands = execprogress.AppendCommandHistory(progress.Commands, command)
	if strings.TrimSpace(cwd) != "" {
		progress.CWD = cwd
	}
	progress.Status = execprogress.NormalizeStatus(event.Status, false)
	explorationChanged := false
	if changed, ok := execprogress.UpsertExplorationProgressForCommandExecution(progress, event, false); ok {
		explorationChanged = changed
		progress.ItemID = execprogress.ExplorationBlockID
	} else {
		execprogress.UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
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

func (s *Service) handleWebSearchProgressStarted(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
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
		return nil
	}
	command, cwd := execprogress.CommandMetadata(event)
	if strings.TrimSpace(event.ItemID) != "" {
		progress.ItemID = strings.TrimSpace(event.ItemID)
	}
	if command != "" {
		progress.Command = command
	}
	if strings.TrimSpace(cwd) != "" {
		progress.CWD = cwd
	}
	progress.Status = execprogress.NormalizeStatus(event.Status, true)
	if changed, ok := execprogress.UpsertExplorationProgressForCommandExecution(progress, event, true); ok {
		progress.ItemID = execprogress.ExplorationBlockID
		if changed && s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
			return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
		}
		return nil
	}
	if !execprogress.HasEntry(progress, event.ItemID, "command_execution") {
		return nil
	}
	execprogress.UpsertEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  strings.TrimSpace(event.ItemID),
		Kind:    "command_execution",
		Label:   "执行",
		Summary: firstNonEmpty(command, progress.Command),
		Status:  progress.Status,
	})
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
	if !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleProcessPlanProgressUpdated(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil || !execprogress.UpsertProcessPlan(progress, event) {
		return nil
	}
	progress.ItemID = execprogress.ProcessPlanBlockID
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleDelegatedTaskProgressUpdated(instanceID string, event agentproto.Event) []eventcontract.Event {
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
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
		Summary: strings.TrimSpace(metadataString(event.Metadata, "description")),
		Status:  execprogress.NormalizeStatus(event.Status, event.Kind == agentproto.EventItemCompleted),
	}
	subagentType := strings.TrimSpace(metadataString(event.Metadata, "subagentType"))
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
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := s.activeOrEnsureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if changed, ok := execprogress.UpsertExplorationProgressForDynamicTool(progress, event, false); ok {
		progress.ItemID = execprogress.ExplorationBlockID
		if !changed {
			return nil
		}
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
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
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	progress := activeExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	if progress == nil {
		return nil
	}
	if changed, ok := execprogress.UpsertExplorationProgressForDynamicTool(progress, event, true); ok {
		progress.ItemID = execprogress.ExplorationBlockID
		if !changed {
			return nil
		}
		return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
	}
	entry, groupKey, changed := execprogress.UpsertDynamicToolProgressEntry(progress, event)
	if groupKey == "" || !changed {
		return nil
	}
	progress.ItemID = groupKey
	execprogress.UpsertEntry(progress, entry)
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) finalizeExecCommandProgressForTurn(instanceID, threadID, turnID, turnStatus, finalText string) []eventcontract.Event {
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil || surface.ActiveExecProgress == nil {
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
	if progress.Exploration != nil && strings.TrimSpace(progress.Exploration.Block.Status) == "running" {
		progress.Exploration.Block.Status = status
	}
	if progress.ProcessPlan != nil && strings.TrimSpace(progress.ProcessPlan.Block.Status) == "running" {
		progress.ProcessPlan.Block.Status = status
	}
	_ = finalText
	if status == "" {
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

func (s *Service) emitExecCommandProgress(surface *state.SurfaceConsoleRecord, progress *state.ExecCommandProgressRecord, threadID, turnID string, final bool) []eventcontract.Event {
	if surface == nil || progress == nil {
		return nil
	}
	progress.Verbosity = state.NormalizeSurfaceVerbosity(surface.Verbosity)
	progress.LastEmittedAt = s.now()
	sourceMessageID, _ := s.replyAnchorForTurn(progress.InstanceID, threadID, turnID)
	snapshot := execprogress.Snapshot(progress)
	if snapshot == nil {
		return nil
	}
	snapshot.Final = final
	snapshot.DetourLabel = remoteBindingDetourLabel(s.lookupRemoteTurn(progress.InstanceID, threadID, turnID))
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
