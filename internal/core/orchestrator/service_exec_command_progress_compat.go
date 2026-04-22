package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func ExecCommandProgressSnapshot(progress *state.ExecCommandProgressRecord) *control.ExecCommandProgress {
	return execprogress.Snapshot(progress)
}

func execCommandMetadata(event agentproto.Event) (string, string) {
	return execprogress.CommandMetadata(event)
}

func appendExecCommandHistory(commands []string, command string) []string {
	return execprogress.AppendCommandHistory(commands, command)
}

func progressHasEntry(progress *state.ExecCommandProgressRecord, itemID, kind string) bool {
	return execprogress.HasEntry(progress, itemID, kind)
}

func upsertExecCommandProgressEntry(progress *state.ExecCommandProgressRecord, entry state.ExecCommandProgressEntryRecord) {
	execprogress.UpsertEntry(progress, entry)
}

func webSearchProgressEntry(metadata map[string]any, final bool) state.ExecCommandProgressEntryRecord {
	return execprogress.WebSearchEntry(metadata, final)
}

func upsertDynamicToolProgressEntry(progress *state.ExecCommandProgressRecord, event agentproto.Event) (state.ExecCommandProgressEntryRecord, string, bool) {
	return execprogress.UpsertDynamicToolProgressEntry(progress, event)
}

func normalizeExecCommandProgressStatus(status string, final bool) string {
	return execprogress.NormalizeStatus(status, final)
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
	case "file_change", "mcp_tool_call", "context_compaction":
		return state.NormalizeSurfaceVerbosity(surface.Verbosity) != state.SurfaceVerbosityQuiet
	case "command_execution", "dynamic_tool_call", "web_search":
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
