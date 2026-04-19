package orchestrator

import (
	"encoding/json"
	"fmt"
	"sort"
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

func ExecCommandProgressSnapshot(progress *state.ExecCommandProgressRecord) *control.ExecCommandProgress {
	if progress == nil {
		return nil
	}
	entries := make([]control.ExecCommandProgressEntry, 0, len(progress.Entries))
	for _, entry := range progress.Entries {
		entries = append(entries, control.ExecCommandProgressEntry{
			ItemID:  entry.ItemID,
			Kind:    entry.Kind,
			Label:   entry.Label,
			Summary: entry.Summary,
			Status:  entry.Status,
			LastSeq: entry.LastSeq,
		})
	}
	snapshot := &control.ExecCommandProgress{
		ThreadID:     progress.ThreadID,
		TurnID:       progress.TurnID,
		ItemID:       progress.ItemID,
		MessageID:    progress.MessageID,
		CardStartSeq: progress.CardStartSeq,
		Blocks:       execCommandProgressBlocks(progress),
		Entries:      entries,
		Commands:     append([]string(nil), progress.Commands...),
		Command:      progress.Command,
		CWD:          progress.CWD,
		Status:       progress.Status,
	}
	snapshot.Timeline = control.BuildExecCommandProgressTimeline(*snapshot)
	return snapshot
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
	case "command_execution", "dynamic_tool_call", "web_search", "mcp_tool_call", "context_compaction":
		return state.NormalizeSurfaceVerbosity(surface.Verbosity) == state.SurfaceVerbosityVerbose
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
	if entry.Kind != "reasoning_summary" {
		clearExecCommandProgressReasoningRecord(progress)
	}
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
			if current.LastSeq == 0 {
				progress.LastVisibleSeq++
				current.LastSeq = progress.LastVisibleSeq
			}
			return
		}
	}
	progress.LastVisibleSeq++
	entry.LastSeq = progress.LastVisibleSeq
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

func upsertDynamicToolProgressEntry(progress *state.ExecCommandProgressRecord, event agentproto.Event) (state.ExecCommandProgressEntryRecord, string, bool) {
	if progress == nil {
		return state.ExecCommandProgressEntryRecord{}, "", false
	}
	tool := strings.TrimSpace(metadataString(event.Metadata, "tool"))
	label := dynamicToolProgressLabel(tool)
	arguments := dynamicToolProgressArguments(event.Metadata)
	summary := strings.TrimSpace(dynamicToolProgressSummaryFromMetadata(event.Metadata))
	status := normalizeDynamicToolProgressStatus(event)
	groupKey := dynamicToolGroupKey(progress, event.ItemID, tool)
	if groupKey == "" {
		return state.ExecCommandProgressEntryRecord{}, "", false
	}
	if progress.DynamicToolGroups == nil {
		progress.DynamicToolGroups = map[string]*state.DynamicToolProgressGroupRecord{}
	}
	if progress.DynamicToolItemGroup == nil {
		progress.DynamicToolItemGroup = map[string]string{}
	}
	if itemID := strings.TrimSpace(event.ItemID); itemID != "" {
		progress.DynamicToolItemGroup[itemID] = groupKey
	}
	group := progress.DynamicToolGroups[groupKey]
	if group == nil {
		group = &state.DynamicToolProgressGroupRecord{GroupKey: groupKey}
		progress.DynamicToolGroups[groupKey] = group
	}
	before := state.DynamicToolProgressGroupRecord{
		GroupKey: group.GroupKey,
		Tool:     group.Tool,
		Label:    group.Label,
		Args:     append([]string(nil), group.Args...),
		Summary:  group.Summary,
		Status:   group.Status,
	}
	if strings.TrimSpace(tool) != "" {
		group.Tool = strings.TrimSpace(tool)
	}
	if strings.TrimSpace(label) != "" {
		group.Label = strings.TrimSpace(label)
	}
	if len(arguments) != 0 {
		group.Args = appendUniquePreserveOrder(group.Args, arguments...)
	}
	if strings.TrimSpace(summary) != "" {
		group.Summary = strings.TrimSpace(summary)
	}
	if strings.TrimSpace(status) != "" {
		group.Status = strings.TrimSpace(status)
	}
	entry := state.ExecCommandProgressEntryRecord{
		ItemID:  groupKey,
		Kind:    "dynamic_tool_call",
		Label:   firstNonEmpty(group.Label, "工具"),
		Summary: buildDynamicToolProgressSummary(group),
		Status:  group.Status,
	}
	changed := group.Tool != before.Tool ||
		group.Label != before.Label ||
		group.Summary != before.Summary ||
		group.Status != before.Status ||
		!sameStringSlice(group.Args, before.Args)
	return entry, groupKey, changed
}

func dynamicToolGroupKey(progress *state.ExecCommandProgressRecord, itemID, tool string) string {
	normalizedTool := strings.ToLower(strings.TrimSpace(tool))
	if normalizedTool != "" {
		return "dynamic_tool_call::" + normalizedTool
	}
	itemID = strings.TrimSpace(itemID)
	if itemID != "" && progress != nil && progress.DynamicToolItemGroup != nil {
		if existing := strings.TrimSpace(progress.DynamicToolItemGroup[itemID]); existing != "" {
			return existing
		}
	}
	if itemID != "" {
		return "dynamic_tool_call::item::" + itemID
	}
	return ""
}

func normalizeDynamicToolProgressStatus(event agentproto.Event) string {
	switch event.Kind {
	case agentproto.EventItemStarted:
		return "started"
	case agentproto.EventItemCompleted:
		status := strings.ToLower(strings.TrimSpace(event.Status))
		switch status {
		case "failed", "error":
			return "failed"
		case "completed", "complete", "ok", "success", "succeeded":
			return "completed"
		default:
			if success, ok := event.Metadata["success"].(bool); ok {
				if success {
					return "completed"
				}
				return "failed"
			}
			return "completed"
		}
	default:
		return ""
	}
}

func dynamicToolProgressLabel(tool string) string {
	tool = strings.TrimSpace(tool)
	if tool == "" {
		return "工具"
	}
	return tool
}

func dynamicToolProgressSummaryFromMetadata(metadata map[string]any) string {
	summary := strings.TrimSpace(metadataString(metadata, "text"))
	if summary != "" {
		return summary
	}
	if value := metadata["arguments"]; value != nil {
		return compactStructuredJSON(value)
	}
	return ""
}

func dynamicToolProgressArguments(metadata map[string]any) []string {
	if len(metadata) == 0 {
		return nil
	}
	value := metadata["arguments"]
	if value == nil {
		return nil
	}
	args := extractDynamicToolProgressArguments(value)
	if len(args) != 0 {
		return args
	}
	if compact := compactStructuredJSON(value); compact != "" {
		return []string{compact}
	}
	return nil
}

func extractDynamicToolProgressArguments(value any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	var walk func(key string, raw any)
	walk = func(key string, raw any) {
		switch typed := raw.(type) {
		case string:
			text := strings.TrimSpace(typed)
			if text == "" {
				return
			}
			if key != "" && !dynamicToolArgumentKeyRelevant(key) {
				return
			}
			if _, exists := seen[text]; exists {
				return
			}
			seen[text] = struct{}{}
			out = append(out, text)
		case []string:
			for _, current := range typed {
				walk(key, current)
			}
		case []any:
			for _, current := range typed {
				walk(key, current)
			}
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for current := range typed {
				keys = append(keys, current)
			}
			sort.Strings(keys)
			for _, current := range keys {
				walk(current, typed[current])
			}
		}
	}
	walk("", value)
	return out
}

func dynamicToolArgumentKeyRelevant(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	normalized = strings.ReplaceAll(normalized, "_", "")
	switch {
	case strings.Contains(normalized, "path"):
		return true
	case strings.Contains(normalized, "file"):
		return true
	case strings.Contains(normalized, "query"):
		return true
	case strings.Contains(normalized, "pattern"):
		return true
	case strings.Contains(normalized, "url"):
		return true
	case strings.Contains(normalized, "glob"):
		return true
	case strings.Contains(normalized, "target"):
		return true
	case strings.Contains(normalized, "text"):
		return true
	case strings.Contains(normalized, "name"):
		return true
	default:
		return false
	}
}

func compactStructuredJSON(value any) string {
	if value == nil {
		return ""
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

func buildDynamicToolProgressSummary(group *state.DynamicToolProgressGroupRecord) string {
	if group == nil {
		return ""
	}
	summary := strings.TrimSpace(strings.Join(group.Args, " "))
	if summary == "" {
		summary = strings.TrimSpace(group.Summary)
	}
	if summary == "" {
		switch strings.ToLower(strings.TrimSpace(group.Status)) {
		case "failed":
			summary = "失败"
		case "completed":
			summary = "已完成"
		default:
			summary = "工作中"
		}
	}
	if strings.EqualFold(strings.TrimSpace(group.Status), "failed") && !strings.Contains(summary, "失败") {
		summary = summary + "（失败）"
	}
	return summary
}

func appendUniquePreserveOrder(base []string, values ...string) []string {
	if len(values) == 0 {
		return base
	}
	seen := map[string]struct{}{}
	for _, current := range base {
		text := strings.TrimSpace(current)
		if text == "" {
			continue
		}
		seen[text] = struct{}{}
	}
	for _, current := range values {
		text := strings.TrimSpace(current)
		if text == "" {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		base = append(base, text)
	}
	return base
}

func sameStringSlice(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if strings.TrimSpace(left[i]) != strings.TrimSpace(right[i]) {
			return false
		}
	}
	return true
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
