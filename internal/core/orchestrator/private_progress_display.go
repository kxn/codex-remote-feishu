package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// PrivateLiveTurnMatches requires an already projected accepted native turn;
// a live delta alone never creates or resumes an execution binding.
func (s *Service) PrivateLiveTurnMatches(surface, instance, thread, turn, command string) bool {
	b := s.lookupRemoteTurn(instance, thread, turn)
	return b != nil && b.SurfaceSessionID == surface && b.InstanceID == instance && b.ThreadID == thread && b.TurnID == turn && b.CommandID == command
}

// ResolvePrivateProgressDisplayTarget copies only the confirmed predecessor's
// segment bookkeeping into the frozen current view. Timeline remains exactly
// as emitted by the current business event.
func ResolvePrivateProgressDisplayTarget(current, previous control.ExecCommandProgress, messageID string, start, end int) (control.ExecCommandProgress, error) {
	if current.ThreadID == "" || current.TurnID == "" || current.ThreadID != previous.ThreadID || current.TurnID != previous.TurnID || strings.TrimSpace(messageID) == "" {
		return current, fmt.Errorf("progress display identity mismatch")
	}
	stateView := &state.ExecCommandProgressRecord{ActiveSegmentID: previous.ActiveSegmentID}
	for _, segment := range previous.Segments {
		stateView.Segments = append(stateView.Segments, state.ExecCommandProgressSegmentRecord{SegmentID: segment.SegmentID, MessageID: segment.MessageID, StartSeq: segment.StartSeq, EndSeq: segment.EndSeq})
	}
	recordExecCommandProgressSegmentWindow(stateView, messageID, start, end)
	current.ActiveSegmentID = stateView.ActiveSegmentID
	current.Segments = make([]control.ExecCommandProgressSegment, 0, len(stateView.Segments))
	for _, segment := range stateView.Segments {
		current.Segments = append(current.Segments, control.ExecCommandProgressSegment{SegmentID: segment.SegmentID, MessageID: segment.MessageID, StartSeq: segment.StartSeq, EndSeq: segment.EndSeq})
	}
	return current, nil
}
