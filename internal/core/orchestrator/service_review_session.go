package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func threadIsReview(thread *state.ThreadRecord) bool {
	return thread != nil && thread.Source != nil && thread.Source.IsReview()
}

func (s *Service) validReviewSession(surface *state.SurfaceConsoleRecord) *state.ReviewSessionRecord {
	if surface == nil || surface.ReviewSession == nil {
		return nil
	}
	session := surface.ReviewSession
	if session.Phase != "" && session.Phase != state.ReviewSessionPhaseActive {
		return nil
	}
	parentThreadID := strings.TrimSpace(session.ParentThreadID)
	reviewThreadID := strings.TrimSpace(session.ReviewThreadID)
	if parentThreadID == "" || reviewThreadID == "" {
		return nil
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return nil
	}
	return session
}

func (s *Service) activeReviewSession(surface *state.SurfaceConsoleRecord) *state.ReviewSessionRecord {
	session := s.validReviewSession(surface)
	if session == nil {
		return nil
	}
	selectedThreadID := strings.TrimSpace(surface.SelectedThreadID)
	if selectedThreadID != strings.TrimSpace(session.ParentThreadID) && selectedThreadID != strings.TrimSpace(session.ReviewThreadID) {
		return nil
	}
	return session
}

func (s *Service) ensureReviewSessionParentSelection(surface *state.SurfaceConsoleRecord, session *state.ReviewSessionRecord) {
	if surface == nil || session == nil {
		return
	}
	parentThreadID := strings.TrimSpace(session.ParentThreadID)
	reviewThreadID := strings.TrimSpace(session.ReviewThreadID)
	selectedThreadID := strings.TrimSpace(surface.SelectedThreadID)
	if parentThreadID == "" || selectedThreadID == parentThreadID || selectedThreadID != reviewThreadID {
		return
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return
	}
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimKnownThread(surface, inst, parentThreadID) {
		surface.SelectedThreadID = selectedThreadID
		surface.RouteMode = prevRouteMode
		_ = s.claimKnownThread(surface, inst, selectedThreadID)
		return
	}
	surface.SelectedThreadID = parentThreadID
	surface.RouteMode = prevRouteMode
	s.clearPreparedNewThread(surface)
}

func clearIdleReviewSession(surface *state.SurfaceConsoleRecord) {
	if surface == nil || surface.ReviewSession == nil {
		return
	}
	if strings.TrimSpace(surface.ReviewSession.ActiveTurnID) != "" {
		return
	}
	surface.ReviewSession = nil
}

func (s *Service) ReviewSession(surfaceID string) *state.ReviewSessionRecord {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	session := s.activeReviewSession(surface)
	if session == nil {
		return nil
	}
	copy := *session
	return &copy
}

func (s *Service) reviewSessionSurface(instanceID, threadID string) (*state.SurfaceConsoleRecord, *state.ReviewSessionRecord) {
	threadID = strings.TrimSpace(threadID)
	if strings.TrimSpace(instanceID) == "" || threadID == "" {
		return nil, nil
	}
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		session := s.validReviewSession(surface)
		if session == nil {
			continue
		}
		if strings.TrimSpace(session.ReviewThreadID) == threadID {
			return surface, session
		}
	}
	return nil, nil
}

func reviewSessionCWD(inst *state.InstanceRecord, session *state.ReviewSessionRecord) string {
	if session == nil {
		return ""
	}
	if cwd := strings.TrimSpace(session.ThreadCWD); cwd != "" {
		return cwd
	}
	if inst == nil {
		return ""
	}
	if thread := inst.Threads[strings.TrimSpace(session.ReviewThreadID)]; thread != nil && strings.TrimSpace(thread.CWD) != "" {
		return strings.TrimSpace(thread.CWD)
	}
	if thread := inst.Threads[strings.TrimSpace(session.ParentThreadID)]; thread != nil && strings.TrimSpace(thread.CWD) != "" {
		return strings.TrimSpace(thread.CWD)
	}
	return strings.TrimSpace(inst.WorkspaceRoot)
}

func threadSourceParentThreadID(source *agentproto.ThreadSourceRecord) string {
	if source == nil {
		return ""
	}
	return strings.TrimSpace(source.ParentThreadID)
}

func reviewThreadParentThreadID(thread *state.ThreadRecord, session *state.ReviewSessionRecord) string {
	if thread != nil {
		if parentThreadID := strings.TrimSpace(firstNonEmpty(thread.ForkedFromID, threadSourceParentThreadID(thread.Source))); parentThreadID != "" {
			return parentThreadID
		}
	}
	if session == nil {
		return ""
	}
	return strings.TrimSpace(session.ParentThreadID)
}

func (s *Service) activateReviewSessionRecord(surface *state.SurfaceConsoleRecord, thread *state.ThreadRecord, event agentproto.Event) *state.ReviewSessionRecord {
	if surface == nil {
		return nil
	}
	if surface.ReviewSession == nil {
		surface.ReviewSession = &state.ReviewSessionRecord{}
	}
	session := surface.ReviewSession
	if session.StartedAt.IsZero() {
		session.StartedAt = s.now()
	}
	session.Phase = state.ReviewSessionPhaseActive
	if parentThreadID := reviewThreadParentThreadID(thread, session); parentThreadID != "" {
		session.ParentThreadID = parentThreadID
	}
	if reviewThreadID := strings.TrimSpace(event.ThreadID); reviewThreadID != "" {
		session.ReviewThreadID = reviewThreadID
	}
	if turnID := strings.TrimSpace(event.TurnID); turnID != "" {
		session.ActiveTurnID = turnID
	}
	if thread != nil {
		session.ThreadCWD = firstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(session.ThreadCWD))
	}
	session.LastUpdatedAt = s.now()
	return session
}

func threadSourceFromMetadata(metadata map[string]any) *agentproto.ThreadSourceRecord {
	if len(metadata) == 0 {
		return nil
	}
	switch typed := metadata["threadSource"].(type) {
	case *agentproto.ThreadSourceRecord:
		return agentproto.CloneThreadSourceRecord(typed)
	case agentproto.ThreadSourceRecord:
		copied := typed
		return &copied
	case map[string]any:
		record := &agentproto.ThreadSourceRecord{
			Kind:           agentproto.ThreadSourceKind(strings.TrimSpace(metadataString(typed, "kind"))),
			Name:           strings.TrimSpace(metadataString(typed, "name")),
			ParentThreadID: strings.TrimSpace(metadataString(typed, "parentThreadId")),
		}
		if record.Kind == "" && record.Name == "" && record.ParentThreadID == "" {
			return nil
		}
		return record
	default:
		return nil
	}
}

func (s *Service) maybeActivateReviewSession(instanceID string, event agentproto.Event) {
	if event.Initiator.Kind != agentproto.InitiatorRemoteSurface || strings.TrimSpace(event.Initiator.SurfaceSessionID) == "" {
		return
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return
	}
	thread := inst.Threads[strings.TrimSpace(event.ThreadID)]
	if !threadIsReview(thread) {
		return
	}
	surface := s.root.Surfaces[event.Initiator.SurfaceSessionID]
	if surface == nil {
		return
	}
	if reviewThreadParentThreadID(thread, surface.ReviewSession) == "" {
		return
	}
	s.activateReviewSessionRecord(surface, thread, event)
}

func (s *Service) maybeCompleteReviewSessionTurn(instanceID string, event agentproto.Event) {
	_, session := s.reviewSessionSurface(instanceID, event.ThreadID)
	if session == nil {
		return
	}
	if strings.TrimSpace(event.TurnID) == "" || strings.TrimSpace(session.ActiveTurnID) == strings.TrimSpace(event.TurnID) {
		session.ActiveTurnID = ""
	}
	session.LastUpdatedAt = s.now()
}

func (s *Service) maybeApplyReviewLifecycleItem(instanceID string, event agentproto.Event) bool {
	switch strings.TrimSpace(event.ItemKind) {
	case "entered_review_mode", "exited_review_mode":
	default:
		return false
	}
	var thread *state.ThreadRecord
	if inst := s.root.Instances[instanceID]; inst != nil {
		thread = inst.Threads[strings.TrimSpace(event.ThreadID)]
	}
	surface, session := s.reviewSessionSurface(instanceID, event.ThreadID)
	if surface == nil && event.Initiator.Kind == agentproto.InitiatorRemoteSurface {
		surface = s.root.Surfaces[event.Initiator.SurfaceSessionID]
	}
	if surface == nil {
		return true
	}
	session = s.activateReviewSessionRecord(surface, thread, event)
	if review := strings.TrimSpace(metadataString(event.Metadata, "review")); review != "" {
		if event.ItemKind == "entered_review_mode" {
			session.TargetLabel = review
		} else {
			session.LastReviewText = review
		}
	}
	return true
}
