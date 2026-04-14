package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
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

func (s *Service) handleMCPToolCallItemProgress(instanceID string, event agentproto.Event) []control.UIEvent {
	if strings.TrimSpace(event.ItemKind) != "mcp_tool_call" || strings.TrimSpace(event.ItemID) == "" {
		return nil
	}
	surface := s.surfaceForInitiator(instanceID, event)
	if surface == nil {
		return nil
	}
	progress := mcpToolCallProgressFromEvent(event)
	if progress == nil {
		return nil
	}
	record := &mcpToolCallProgressRecord{
		SurfaceSessionID: surface.SurfaceSessionID,
		InstanceID:       instanceID,
		ThreadID:         event.ThreadID,
		TurnID:           event.TurnID,
		ItemID:           event.ItemID,
		Server:           progress.Server,
		Tool:             progress.Tool,
		Status:           progress.Status,
		ErrorMessage:     progress.ErrorMessage,
		DurationMS:       progress.DurationMS,
	}
	key := mcpToolCallProgressKey(surface.SurfaceSessionID, instanceID, event.ThreadID, event.TurnID, event.ItemID)
	if existing := s.mcpToolCallProgress[key]; existing != nil && equalMCPToolCallProgressRecord(existing, record) {
		return nil
	}
	s.mcpToolCallProgress[key] = record
	sourceMessageID := ""
	if binding := s.lookupRemoteTurn(instanceID, event.ThreadID, event.TurnID); binding != nil {
		sourceMessageID = binding.ReplyToMessageID
	}
	return []control.UIEvent{{
		Kind:                control.UIEventMCPToolCallProgress,
		GatewayID:           surface.GatewayID,
		SurfaceSessionID:    surface.SurfaceSessionID,
		SourceMessageID:     sourceMessageID,
		MCPToolCallProgress: progress,
	}}
}

func mcpToolCallProgressFromEvent(event agentproto.Event) *control.MCPToolCallProgress {
	status := normalizeMCPToolCallProgressStatus(event)
	if status == "" {
		return nil
	}
	durationMS := 0
	if event.Metadata != nil {
		durationMS = lookupIntFromAny(event.Metadata["durationMs"])
	}
	return &control.MCPToolCallProgress{
		ThreadID:     event.ThreadID,
		TurnID:       event.TurnID,
		ItemID:       event.ItemID,
		Server:       metadataString(event.Metadata, "server"),
		Tool:         metadataString(event.Metadata, "tool"),
		Status:       status,
		ErrorMessage: metadataString(event.Metadata, "errorMessage"),
		DurationMS:   durationMS,
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
