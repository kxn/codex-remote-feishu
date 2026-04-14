package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execCommandProgressMinInterval = 300 * time.Millisecond

func (s *Service) handleExecCommandItemStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	if strings.TrimSpace(event.ItemKind) != "command_execution" {
		return nil
	}
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil || !s.surfaceAllowsExecCommandProgress(surface) {
		return nil
	}
	command, cwd := execCommandMetadata(event)
	if command == "" {
		return nil
	}
	progress := s.ensureExecCommandProgress(surface, instanceID, event.ThreadID, event.TurnID)
	prevItemID := strings.TrimSpace(progress.ItemID)
	progress.ItemID = event.ItemID
	progress.Command = command
	progress.Commands = appendExecCommandHistory(progress.Commands, command)
	if strings.TrimSpace(cwd) != "" {
		progress.CWD = cwd
	}
	progress.Status = normalizeExecCommandProgressStatus(event.Status, false)
	if prevItemID != "" && prevItemID == strings.TrimSpace(event.ItemID) && !progress.LastEmittedAt.IsZero() && s.now().Sub(progress.LastEmittedAt) < execCommandProgressMinInterval {
		return nil
	}
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) handleExecCommandItemDelta(instanceID string, event agentproto.Event) []control.UIEvent {
	if strings.TrimSpace(event.ItemKind) != "agent_message" || strings.TrimSpace(event.Delta) == "" {
		return nil
	}
	s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
	return nil
}

func (s *Service) handleExecCommandItemCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		if s.eventCarriesAssistantText(instanceID, event) {
			s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		}
		return nil
	case "command_execution":
	default:
		return nil
	}
	surface := s.turnSurface(instanceID, event.ThreadID, event.TurnID)
	if surface == nil {
		return nil
	}
	if !s.surfaceAllowsExecCommandProgress(surface) {
		s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		return nil
	}
	progress := surface.ActiveExecProgress
	if progress == nil || progress.InstanceID != instanceID || progress.ThreadID != event.ThreadID || progress.TurnID != event.TurnID {
		return nil
	}
	if progress.ItemID != "" && strings.TrimSpace(event.ItemID) != "" && progress.ItemID != strings.TrimSpace(event.ItemID) {
		return nil
	}
	command, cwd := execCommandMetadata(event)
	progress.ItemID = event.ItemID
	if command != "" {
		progress.Command = command
	}
	if strings.TrimSpace(cwd) != "" {
		progress.CWD = cwd
	}
	progress.Status = normalizeExecCommandProgressStatus(event.Status, true)
	return nil
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
	return nil
}

func (s *Service) RecordExecCommandProgressMessage(surfaceID, threadID, turnID, itemID, messageID string) {
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
}

func (s *Service) emitExecCommandProgress(surface *state.SurfaceConsoleRecord, progress *state.ExecCommandProgressRecord, threadID, turnID string, final bool) []control.UIEvent {
	if surface == nil || progress == nil {
		return nil
	}
	progress.LastEmittedAt = s.now()
	sourceMessageID := ""
	if binding := s.lookupRemoteTurn(progress.InstanceID, threadID, turnID); binding != nil {
		sourceMessageID = firstNonEmpty(binding.ReplyToMessageID, binding.SourceMessageID)
	}
	return []control.UIEvent{{
		Kind:             control.UIEventExecCommandProgress,
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  sourceMessageID,
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID:  progress.ThreadID,
			TurnID:    progress.TurnID,
			ItemID:    progress.ItemID,
			MessageID: progress.MessageID,
			Commands:  append([]string(nil), progress.Commands...),
			Command:   progress.Command,
			CWD:       progress.CWD,
			Status:    progress.Status,
			Final:     final,
		},
	}}
}

func (s *Service) ensureExecCommandProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.ExecCommandProgressRecord {
	if surface.ActiveExecProgress != nil {
		progress := surface.ActiveExecProgress
		if progress.InstanceID == instanceID && progress.ThreadID == threadID && progress.TurnID == turnID {
			return progress
		}
	}
	surface.ActiveExecProgress = &state.ExecCommandProgressRecord{
		InstanceID: instanceID,
		ThreadID:   threadID,
		TurnID:     turnID,
	}
	return surface.ActiveExecProgress
}

func (s *Service) terminateExecCommandProgressForTurn(instanceID, threadID, turnID string) {
	surface := s.turnSurface(instanceID, threadID, turnID)
	if surface == nil || surface.ActiveExecProgress == nil {
		return
	}
	progress := surface.ActiveExecProgress
	if progress.InstanceID == instanceID && progress.ThreadID == threadID && progress.TurnID == turnID {
		surface.ActiveExecProgress = nil
	}
}

func (s *Service) surfaceAllowsExecCommandProgress(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	return state.NormalizeSurfaceVerbosity(surface.Verbosity) == state.SurfaceVerbosityVerbose
}

func (s *Service) eventCarriesAssistantText(instanceID string, event agentproto.Event) bool {
	if strings.TrimSpace(metadataString(event.Metadata, "text")) != "" {
		return true
	}
	if strings.TrimSpace(event.ItemID) == "" {
		return false
	}
	buf := s.itemBuffers[itemBufferKey(instanceID, event.ThreadID, event.TurnID, event.ItemID)]
	if buf == nil {
		return false
	}
	return strings.TrimSpace(buf.text()) != ""
}

func execCommandMetadata(event agentproto.Event) (string, string) {
	if event.Metadata == nil {
		return "", ""
	}
	command, _ := event.Metadata["command"].(string)
	cwd, _ := event.Metadata["cwd"].(string)
	return strings.TrimSpace(command), strings.TrimSpace(cwd)
}

func appendExecCommandHistory(commands []string, command string) []string {
	command = strings.TrimSpace(command)
	if command == "" {
		return commands
	}
	return append(commands, command)
}

func normalizeExecCommandProgressStatus(status string, final bool) string {
	value := strings.ToLower(strings.TrimSpace(status))
	switch value {
	case "failed", "error":
		return "failed"
	case "interrupted", "cancelled", "canceled":
		return "interrupted"
	case "completed", "ok", "success", "succeeded":
		return "completed"
	case "inprogress", "in_progress", "running":
		return "running"
	case "":
		if final {
			return "completed"
		}
		return "running"
	default:
		if final {
			return value
		}
		return "running"
	}
}
