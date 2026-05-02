package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

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
	ensureExecCommandProgressActiveSegment(surface.ActiveExecProgress)
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
	case "delegated_task":
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
	ensureExecCommandProgressActiveSegment(progress)
	return progress
}

func ensureExecCommandProgressActiveSegment(progress *state.ExecCommandProgressRecord) *state.ExecCommandProgressSegmentRecord {
	if progress == nil {
		return nil
	}
	if strings.TrimSpace(progress.ActiveSegmentID) != "" {
		for i := range progress.Segments {
			if progress.Segments[i].SegmentID == progress.ActiveSegmentID {
				return &progress.Segments[i]
			}
		}
	}
	if len(progress.Segments) == 0 {
		progress.Segments = append(progress.Segments, state.ExecCommandProgressSegmentRecord{
			SegmentID: "segment-1",
			StartSeq:  1,
		})
	}
	if strings.TrimSpace(progress.ActiveSegmentID) == "" {
		progress.ActiveSegmentID = progress.Segments[len(progress.Segments)-1].SegmentID
	}
	for i := range progress.Segments {
		if progress.Segments[i].SegmentID == progress.ActiveSegmentID {
			if progress.Segments[i].StartSeq <= 0 {
				progress.Segments[i].StartSeq = 1
			}
			return &progress.Segments[i]
		}
	}
	progress.ActiveSegmentID = fmt.Sprintf("segment-%d", len(progress.Segments)+1)
	progress.Segments = append(progress.Segments, state.ExecCommandProgressSegmentRecord{
		SegmentID: progress.ActiveSegmentID,
		StartSeq:  1,
	})
	return &progress.Segments[len(progress.Segments)-1]
}

func activeExecCommandProgressSegmentMessageID(progress *state.ExecCommandProgressRecord) string {
	segment := ensureExecCommandProgressActiveSegment(progress)
	if segment == nil {
		return ""
	}
	return strings.TrimSpace(segment.MessageID)
}

func activeExecCommandProgressSegmentStartSeq(progress *state.ExecCommandProgressRecord) int {
	segment := ensureExecCommandProgressActiveSegment(progress)
	if segment == nil {
		return 0
	}
	return segment.StartSeq
}

func appendExecCommandProgressSegment(progress *state.ExecCommandProgressRecord, startSeq int) *state.ExecCommandProgressSegmentRecord {
	if progress == nil {
		return nil
	}
	current := ensureExecCommandProgressActiveSegment(progress)
	if current != nil && startSeq > 0 && current.EndSeq < startSeq-1 {
		current.EndSeq = startSeq - 1
	}
	if startSeq <= 0 {
		startSeq = 1
	}
	progress.ActiveSegmentID = fmt.Sprintf("segment-%d", len(progress.Segments)+1)
	progress.Segments = append(progress.Segments, state.ExecCommandProgressSegmentRecord{
		SegmentID: progress.ActiveSegmentID,
		StartSeq:  startSeq,
	})
	return &progress.Segments[len(progress.Segments)-1]
}
