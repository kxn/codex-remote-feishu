package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type mcpToolCallProgressRecord struct {
	SurfaceSessionID string
	InstanceID       string
	ThreadID         string
	TurnID           string
	ItemID           string
	Server           string
	Tool             string
	Status           string
	ErrorMessage     string
	DurationMS       int
}

func mcpToolCallProgressKey(surfaceID, instanceID, threadID, turnID, itemID string) string {
	return strings.Join([]string{surfaceID, instanceID, threadID, turnID, itemID}, "::")
}

func deleteMatchingMCPToolCallProgress(records map[string]*mcpToolCallProgressRecord, instanceID, threadID, turnID string) {
	for key, record := range records {
		if record == nil {
			continue
		}
		if record.InstanceID != instanceID {
			continue
		}
		if threadID != "" && record.ThreadID != threadID {
			continue
		}
		if turnID != "" && record.TurnID != turnID {
			continue
		}
		delete(records, key)
	}
}

func equalMCPToolCallProgressRecord(left, right *mcpToolCallProgressRecord) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	}
	return left.SurfaceSessionID == right.SurfaceSessionID &&
		left.InstanceID == right.InstanceID &&
		left.ThreadID == right.ThreadID &&
		left.TurnID == right.TurnID &&
		left.ItemID == right.ItemID &&
		left.Server == right.Server &&
		left.Tool == right.Tool &&
		left.Status == right.Status &&
		left.ErrorMessage == right.ErrorMessage &&
		left.DurationMS == right.DurationMS
}

func (s *Service) handleMCPToolCallItemStarted(instanceID string, event agentproto.Event) []control.UIEvent {
	return s.handleMCPToolCallItemProgress(instanceID, event, false)
}

func (s *Service) handleMCPToolCallItemCompleted(instanceID string, event agentproto.Event) []control.UIEvent {
	return s.handleMCPToolCallItemProgress(instanceID, event, true)
}

func (s *Service) handleMCPToolCallItemProgress(instanceID string, event agentproto.Event, final bool) []control.UIEvent {
	if strings.TrimSpace(event.ItemKind) != "mcp_tool_call" || strings.TrimSpace(event.ItemID) == "" {
		return nil
	}
	surface := s.surfaceForInitiator(instanceID, event)
	if surface == nil || !s.surfaceAllowsProcessProgress(surface, event.ItemKind) {
		return nil
	}
	record := mcpToolCallProgressRecordFromEvent(surface.SurfaceSessionID, instanceID, event)
	if record == nil {
		return nil
	}
	key := mcpToolCallProgressKey(surface.SurfaceSessionID, instanceID, event.ThreadID, event.TurnID, event.ItemID)
	if existing := s.mcpToolCallProgress[key]; existing != nil && equalMCPToolCallProgressRecord(existing, record) {
		return nil
	}
	progress := s.ensureProgressForMCPToolCall(surface, instanceID, event.ThreadID, event.TurnID, event.ItemID, final)
	if progress == nil {
		return nil
	}
	s.mcpToolCallProgress[key] = record
	progress.ItemID = strings.TrimSpace(event.ItemID)
	upsertExecCommandProgressEntry(progress, mcpToolCallProgressEntry(*record))
	return s.emitExecCommandProgress(surface, progress, event.ThreadID, event.TurnID, false)
}

func (s *Service) ensureProgressForMCPToolCall(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID, itemID string, final bool) *state.ExecCommandProgressRecord {
	if !final {
		return s.activeOrEnsureExecCommandProgress(surface, instanceID, threadID, turnID)
	}
	progress := activeExecCommandProgress(surface, instanceID, threadID, turnID)
	if progress == nil || !progressHasEntry(progress, itemID, "mcp_tool_call") {
		return nil
	}
	return progress
}

func (s *Service) activeOrEnsureExecCommandProgress(surface *state.SurfaceConsoleRecord, instanceID, threadID, turnID string) *state.ExecCommandProgressRecord {
	if progress := activeExecCommandProgress(surface, instanceID, threadID, turnID); progress != nil {
		return progress
	}
	return s.ensureExecCommandProgress(surface, instanceID, threadID, turnID)
}

func mcpToolCallProgressRecordFromEvent(surfaceID, instanceID string, event agentproto.Event) *mcpToolCallProgressRecord {
	status := normalizeMCPToolCallProgressStatus(event)
	if status == "" {
		return nil
	}
	durationMS := 0
	if event.Metadata != nil {
		durationMS = lookupIntFromAny(event.Metadata["durationMs"])
	}
	return &mcpToolCallProgressRecord{
		SurfaceSessionID: surfaceID,
		InstanceID:       instanceID,
		ThreadID:         event.ThreadID,
		TurnID:           event.TurnID,
		ItemID:           strings.TrimSpace(event.ItemID),
		Server:           metadataString(event.Metadata, "server"),
		Tool:             metadataString(event.Metadata, "tool"),
		Status:           status,
		ErrorMessage:     metadataString(event.Metadata, "errorMessage"),
		DurationMS:       durationMS,
	}
}

func normalizeMCPToolCallProgressStatus(event agentproto.Event) string {
	switch event.Kind {
	case agentproto.EventItemStarted:
		return "started"
	case agentproto.EventItemCompleted:
		value := strings.ToLower(strings.TrimSpace(event.Status))
		switch value {
		case "failed", "error":
			return "failed"
		case "completed", "complete", "ok", "success", "succeeded":
			return "completed"
		case "inprogress", "in_progress", "running":
			return "started"
		default:
			if strings.TrimSpace(metadataString(event.Metadata, "errorMessage")) != "" {
				return "failed"
			}
			return "completed"
		}
	default:
		return ""
	}
}

func mcpToolCallProgressEntry(record mcpToolCallProgressRecord) state.ExecCommandProgressEntryRecord {
	name := formatMCPToolCallName(record.Server, record.Tool)
	summary := name
	switch strings.ToLower(strings.TrimSpace(record.Status)) {
	case "completed":
		if record.DurationMS > 0 {
			summary = fmt.Sprintf("%s（%d ms）", name, record.DurationMS)
		}
	case "failed":
		if strings.TrimSpace(record.ErrorMessage) != "" {
			summary = fmt.Sprintf("%s（失败：%s）", name, strings.TrimSpace(record.ErrorMessage))
		} else {
			summary = fmt.Sprintf("%s（失败）", name)
		}
	}
	return state.ExecCommandProgressEntryRecord{
		ItemID:  record.ItemID,
		Kind:    "mcp_tool_call",
		Label:   "MCP",
		Summary: summary,
		Status:  record.Status,
	}
}

func formatMCPToolCallName(server, tool string) string {
	server = strings.TrimSpace(server)
	tool = strings.TrimSpace(tool)
	switch {
	case server != "" && tool != "":
		return server + "." + tool
	case tool != "":
		return tool
	case server != "":
		return server
	default:
		return "未知 MCP 调用"
	}
}
