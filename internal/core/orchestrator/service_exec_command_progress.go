package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const execCommandProgressMinInterval = 300 * time.Millisecond

func (s *Service) handleProcessProgressItemStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	switch strings.TrimSpace(event.ItemKind) {
	case "command_execution":
		return s.handleCommandExecutionProgressStarted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressStarted(instanceID, event)
	case "mcp_tool_call":
		return s.handleMCPToolCallItemStarted(instanceID, event)
	default:
		return nil
	}
}

func (s *Service) handleProcessProgressItemDelta(instanceID string, event agentproto.Event) []control.UIEvent {
	if strings.TrimSpace(event.ItemKind) != "agent_message" || strings.TrimSpace(event.Delta) == "" {
		return nil
	}
	s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
	return nil
}

func (s *Service) handleProcessProgressItemCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	switch strings.TrimSpace(event.ItemKind) {
	case "agent_message":
		if s.eventCarriesAssistantText(instanceID, event) {
			s.terminateExecCommandProgressForTurn(instanceID, event.ThreadID, event.TurnID)
		}
		return nil
	case "command_execution":
		return s.handleCommandExecutionProgressCompleted(instanceID, event)
	case "web_search":
		return s.handleWebSearchProgressCompleted(instanceID, event)
	case "mcp_tool_call":
		return s.handleMCPToolCallItemCompleted(instanceID, event)
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
	upsertExecCommandProgressEntry(progress, state.ExecCommandProgressEntryRecord{
		ItemID:  progress.ItemID,
		Kind:    "command_execution",
		Label:   "执行",
		Summary: command,
		Status:  progress.Status,
	})
	if prevItemID != "" && prevItemID == progress.ItemID && !progress.LastEmittedAt.IsZero() && s.now().Sub(progress.LastEmittedAt) < execCommandProgressMinInterval {
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
	if progress == nil || !progressHasEntry(progress, event.ItemID, "command_execution") {
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
	entries := make([]control.ExecCommandProgressEntry, 0, len(progress.Entries))
	for _, entry := range progress.Entries {
		entries = append(entries, control.ExecCommandProgressEntry{
			ItemID:  entry.ItemID,
			Kind:    entry.Kind,
			Label:   entry.Label,
			Summary: entry.Summary,
			Status:  entry.Status,
		})
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
			Entries:   entries,
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

func (s *Service) surfaceAllowsProcessProgress(surface *state.SurfaceConsoleRecord, itemKind string) bool {
	if surface == nil {
		return false
	}
	switch strings.TrimSpace(itemKind) {
	case "command_execution":
		return state.NormalizeSurfaceVerbosity(surface.Verbosity) == state.SurfaceVerbosityVerbose
	case "web_search", "mcp_tool_call":
		return state.NormalizeSurfaceVerbosity(surface.Verbosity) != state.SurfaceVerbosityQuiet
	default:
		return false
	}
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

func activeExecCommandProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.ExecCommandProgressRecord {
	if surface == nil || surface.ActiveExecProgress == nil {
		return nil
	}
	progress := surface.ActiveExecProgress
	if progress.InstanceID != instanceID || progress.ThreadID != threadID || progress.TurnID != turnID {
		return nil
	}
	return progress
}

func progressHasEntry(progress *state.ExecCommandProgressRecord, itemID, kind string) bool {
	if progress == nil {
		return false
	}
	itemID = strings.TrimSpace(itemID)
	kind = strings.TrimSpace(kind)
	if itemID == "" {
		return true
	}
	for _, entry := range progress.Entries {
		if entry.ItemID == itemID && (kind == "" || entry.Kind == kind) {
			return true
		}
	}
	return false
}

func upsertExecCommandProgressEntry(progress *state.ExecCommandProgressRecord, entry state.ExecCommandProgressEntryRecord) {
	if progress == nil {
		return
	}
	entry.ItemID = strings.TrimSpace(entry.ItemID)
	entry.Kind = strings.TrimSpace(entry.Kind)
	entry.Label = strings.TrimSpace(entry.Label)
	entry.Summary = strings.TrimSpace(entry.Summary)
	entry.Status = strings.TrimSpace(entry.Status)
	if entry.Summary == "" {
		return
	}
	for i := range progress.Entries {
		current := &progress.Entries[i]
		if entry.ItemID != "" && current.ItemID == entry.ItemID {
			if entry.Kind != "" {
				current.Kind = entry.Kind
			}
			if entry.Label != "" {
				current.Label = entry.Label
			}
			if entry.Summary != "" {
				current.Summary = entry.Summary
			}
			if entry.Status != "" {
				current.Status = entry.Status
			}
			return
		}
	}
	progress.Entries = append(progress.Entries, entry)
}

func webSearchProgressEntry(metadata map[string]any, final bool) state.ExecCommandProgressEntryRecord {
	actionType := strings.TrimSpace(metadataString(metadata, "actionType"))
	query := strings.TrimSpace(metadataString(metadata, "query"))
	url := strings.TrimSpace(metadataString(metadata, "url"))
	pattern := strings.TrimSpace(metadataString(metadata, "pattern"))
	queries := metadataStringSlice(metadata, "queries")
	fallbackQuery := firstNonEmpty(query, firstNonEmptySlice(queries...))
	status := normalizeExecCommandProgressStatus("", final)
	switch actionType {
	case "open_page":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "打开网页",
			Summary: firstNonEmpty(url, fallbackWebSearchSummary(final)),
			Status:  status,
		}
	case "find_in_page":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "页内查找",
			Summary: firstNonEmpty(formatFindInPageSummary(pattern, url), fallbackWebSearchSummary(final)),
			Status:  status,
		}
	case "search":
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "搜索",
			Summary: firstNonEmpty(fallbackQuery, fallbackWebSearchSummary(final)),
			Status:  status,
		}
	default:
		if final {
			return state.ExecCommandProgressEntryRecord{
				Kind:    "web_search",
				Label:   "搜索",
				Summary: firstNonEmpty(fallbackQuery, formatFindInPageSummary(pattern, url), url, "搜索完成"),
				Status:  status,
			}
		}
		return state.ExecCommandProgressEntryRecord{
			Kind:    "web_search",
			Label:   "搜索",
			Summary: firstNonEmpty(fallbackQuery, "正在搜索网络"),
			Status:  status,
		}
	}
}

func metadataStringSlice(metadata map[string]any, key string) []string {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[key]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, current := range typed {
			if text := strings.TrimSpace(current); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, current := range typed {
			if text := lookupStringFromAny(current); text != "" {
				out = append(out, text)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func firstNonEmptySlice(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func formatFindInPageSummary(pattern, url string) string {
	pattern = strings.TrimSpace(pattern)
	url = strings.TrimSpace(url)
	switch {
	case pattern != "" && url != "":
		return fmt.Sprintf("%s @ %s", pattern, url)
	case pattern != "":
		return pattern
	case url != "":
		return url
	default:
		return ""
	}
}

func fallbackWebSearchSummary(final bool) string {
	if final {
		return "搜索完成"
	}
	return "正在搜索网络"
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
