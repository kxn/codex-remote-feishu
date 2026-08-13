package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func threadIsReview(thread *state.ThreadRecord) bool {
	return thread != nil && thread.Source != nil && thread.Source.IsReview()
}

func (s *Service) validReviewSession(surface *state.SurfaceConsoleRecord) *state.ReviewSessionRecord {
	if surface == nil || surface.ReviewSession == nil {
		return nil
	}
	session := surface.ReviewSession
	if session.Phase != "" && session.Phase != state.ReviewSessionPhaseActive && session.Phase != state.ReviewSessionPhaseReady {
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
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return nil
	}
	if !s.reviewSessionBackendMatches(surface, session) {
		return nil
	}
	return session
}

func (s *Service) reviewSessionBackendMatches(surface *state.SurfaceConsoleRecord, session *state.ReviewSessionRecord) bool {
	if surface == nil || session == nil {
		return false
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return false
	}
	backend := agentproto.NormalizeBackend(session.Backend)
	return s.surfaceBackend(surface) == backend && state.EffectiveInstanceBackend(inst) == backend && reviewExecutorMatchesBackend(session.ExecutorKind, backend)
}

func reviewExecutorMatchesBackend(executor state.ReviewExecutorKind, backend agentproto.Backend) bool {
	if executor == "" {
		return backend == agentproto.BackendCodex
	}
	switch backend {
	case agentproto.BackendCodex:
		return executor == state.ReviewExecutorCodexNative
	case agentproto.BackendClaude:
		return executor == state.ReviewExecutorClaudeForkSession
	case agentproto.BackendOpenCode:
		return executor == state.ReviewExecutorOpenCodeACPFork
	default:
		return false
	}
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

func reviewSessionTurnActive(surface *state.SurfaceConsoleRecord, session *state.ReviewSessionRecord) bool {
	if surface == nil || session == nil {
		return false
	}
	if strings.TrimSpace(session.ActiveTurnID) != "" {
		return true
	}
	reviewThreadID := strings.TrimSpace(session.ReviewThreadID)
	queueItemTargetsReview := func(item *state.QueueItemRecord) bool {
		if item == nil || queuedItemExecutionThreadID(item) != reviewThreadID {
			return false
		}
		switch item.Status {
		case state.QueueItemQueued, state.QueueItemDispatching, state.QueueItemRunning:
			return true
		default:
			return false
		}
	}
	if queueItemTargetsReview(surface.QueueItems[surface.ActiveQueueItemID]) {
		return true
	}
	for _, queueItemID := range surface.QueuedQueueItemIDs {
		if queueItemTargetsReview(surface.QueueItems[queueItemID]) {
			return true
		}
	}
	return false
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
	if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID: inst.InstanceID,
		RouteMode:          prevRouteMode,
		SelectedThreadID:   parentThreadID,
		ThreadClaimPolicy:  surfaceRouteThreadClaimKnown,
	}) {
		return
	}
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

func (s *Service) blockReviewSessionBackendMismatch(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || surface.ReviewSession == nil {
		return nil
	}
	if s.reviewSessionBackendMatches(surface, surface.ReviewSession) {
		return nil
	}
	if !reviewSessionTurnActive(surface, surface.ReviewSession) {
		s.releaseFeishuRoomReviewReservations(surface)
		s.clearPendingReviewStart(surface)
		surface.ReviewSession = nil
	}
	return notice(surface, "review_backend_mismatch", "当前审阅会话与已连接的服务类型不一致，已停止继续路由；请重新进入审阅。")
}

func (s *Service) blockNonTextReviewSessionInput(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || surface.ReviewSession == nil {
		return nil
	}
	if blocked := s.blockReviewSessionBackendMismatch(surface); blocked != nil {
		return blocked
	}
	session := surface.ReviewSession
	if session.AwaitingFollowUpText {
		return notice(surface, "review_follow_up_waiting_text", "当前正在等待一条文字追问；图片和文件不会加入审阅会话。")
	}
	if session.Phase == state.ReviewSessionPhasePending {
		return notice(surface, "review_thread_not_ready", "当前正在进入审阅，请等待审阅会话建立；如需退出，可使用 `/new` 或 `/detach`。")
	}
	if reviewSessionTurnActive(surface, session) || session.Phase == state.ReviewSessionPhaseActive {
		return notice(surface, "review_turn_active", "当前审阅仍在处理中，请等待完成或使用 `/stop`。")
	}
	return notice(surface, "review_follow_up_not_requested", "当前审阅结果仍待处理。请在最新结果卡上选择继续追问、退出审阅或按审阅意见继续修改。")
}

func (s *Service) handleReviewSessionText(surface *state.SurfaceConsoleRecord, action control.Action, text string) ([]eventcontract.Event, bool) {
	if surface == nil || surface.ReviewSession == nil {
		return nil, false
	}
	if blocked := s.blockReviewSessionBackendMismatch(surface); blocked != nil {
		return blocked, true
	}
	session := s.validReviewSession(surface)
	if session == nil {
		return notice(surface, "review_thread_not_ready", "当前正在进入审阅，请等待审阅会话建立；如需退出，可使用 `/new` 或 `/detach`。"), true
	}
	s.ensureReviewSessionParentSelection(surface, session)
	if !session.AwaitingFollowUpText {
		if reviewSessionTurnActive(surface, session) || session.Phase == state.ReviewSessionPhaseActive {
			return notice(surface, "review_turn_active", "当前审阅仍在处理中，请等待完成或使用 `/stop`。"), true
		}
		return notice(surface, "review_follow_up_not_requested", "当前审阅结果仍待处理。请在最新结果卡上选择继续追问、退出审阅或按审阅意见继续修改。"), true
	}
	if strings.TrimSpace(text) == "" || reviewFollowUpHasNonTextInput(action.Inputs) {
		return notice(surface, "review_follow_up_waiting_text", "当前反馈模式只接受一条纯文字追问；图片和文件不会加入审阅会话。"), true
	}

	// The explicit capture is one-shot even if dispatch later fails.
	session.AwaitingFollowUpText = false
	session.LastUpdatedAt = s.now()
	if reviewSessionTurnActive(surface, session) || session.Phase != state.ReviewSessionPhaseReady {
		return notice(surface, "review_turn_active", "当前审阅仍在处理中，请等本轮完成后再继续追问。"), true
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	cwd := reviewSessionCWD(inst, session)
	if strings.TrimSpace(cwd) == "" {
		return notice(surface, "review_thread_not_ready", "当前审阅会话缺少可用的工作目录，请退出后重新进入审阅。"), true
	}
	inputs := []agentproto.Input{{Type: agentproto.InputText, Text: text}}
	return s.enqueueQueueItemWithTarget(
		surface,
		action.MessageID,
		text,
		nil,
		inputs,
		strings.TrimSpace(session.ReviewThreadID),
		cwd,
		surface.RouteMode,
		surface.PromptOverride,
		agentproto.PromptExecutionModeResumeExisting,
		strings.TrimSpace(session.ParentThreadID),
		agentproto.SurfaceBindingPolicyKeepSurfaceSelection,
		agentproto.PromptPurposeReview,
		false,
	), true
}

func reviewFollowUpHasNonTextInput(inputs []agentproto.Input) bool {
	for _, input := range inputs {
		if input.Type != agentproto.InputText {
			return true
		}
	}
	return false
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
		if parentThreadID := strings.TrimSpace(xutil.FirstNonEmpty(thread.ForkedFromID, threadSourceParentThreadID(thread.Source))); parentThreadID != "" {
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
	s.clearPendingReviewStart(surface)
	if parentThreadID := reviewThreadParentThreadID(thread, session); parentThreadID != "" {
		session.ParentThreadID = parentThreadID
	}
	if reviewThreadID := strings.TrimSpace(event.ThreadID); reviewThreadID != "" {
		session.ReviewThreadID = reviewThreadID
	}
	if turnID := strings.TrimSpace(event.TurnID); turnID != "" {
		if strings.TrimSpace(session.InitialTurnID) == "" && strings.TrimSpace(session.LastReviewText) == "" {
			session.InitialTurnID = turnID
		}
		session.ActiveTurnID = turnID
	}
	if thread != nil {
		session.ThreadCWD = xutil.FirstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(session.ThreadCWD))
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
			Kind:           agentproto.ThreadSourceKind(strings.TrimSpace(xutil.MetadataString(typed, "kind"))),
			Name:           strings.TrimSpace(xutil.MetadataString(typed, "name")),
			ParentThreadID: strings.TrimSpace(xutil.MetadataString(typed, "parentThreadId")),
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
	if binding := s.pendingRemoteBindingForEvent(instanceID, event); binding != nil && remoteBindingPromptDispatchPlan(binding).Purpose == agentproto.PromptPurposeReview {
		var item *state.QueueItemRecord
		if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
			item = surface.QueueItems[binding.QueueItemID]
		}
		thread = s.materializeRemoteTurnThread(inst, event.ThreadID, event.CWD, binding, item)
	}
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

func (s *Service) maybeCaptureReviewSessionResultCandidate(instanceID string, event agentproto.Event, itemKind, text string) {
	_, session := s.reviewSessionSurface(instanceID, event.ThreadID)
	if session == nil || strings.TrimSpace(itemKind) != "agent_message" || strings.TrimSpace(event.TurnID) == "" || strings.TrimSpace(session.InitialTurnID) != strings.TrimSpace(event.TurnID) || strings.TrimSpace(session.LastReviewText) != "" {
		return
	}
	if candidate := strings.TrimSpace(text); candidate != "" {
		itemID := strings.TrimSpace(event.ItemID)
		for _, capturedItemID := range session.PendingReviewItemIDs {
			if capturedItemID == itemID && itemID != "" {
				return
			}
		}
		if itemID != "" {
			session.PendingReviewItemIDs = append(session.PendingReviewItemIDs, itemID)
		}
		if strings.TrimSpace(session.PendingReviewText) == "" {
			session.PendingReviewText = candidate
		} else {
			session.PendingReviewText = strings.TrimSpace(session.PendingReviewText) + "\n\n" + candidate
		}
		session.LastUpdatedAt = s.now()
	}
}

func (s *Service) maybeCompleteReviewSessionTurn(instanceID string, event agentproto.Event) {
	surface, session := s.reviewSessionSurface(instanceID, event.ThreadID)
	if session == nil {
		return
	}
	completedActiveTurn := strings.TrimSpace(event.TurnID) == "" || strings.TrimSpace(session.ActiveTurnID) == strings.TrimSpace(event.TurnID)
	completedInitialTurn := strings.TrimSpace(event.TurnID) != "" && strings.TrimSpace(session.InitialTurnID) == strings.TrimSpace(event.TurnID)
	exitRequested := session.ExitRequested && completedActiveTurn
	if completedActiveTurn && completedInitialTurn && strings.TrimSpace(session.LastReviewText) == "" && turnCompletedSuccessfully(event) {
		if reviewText := strings.TrimSpace(session.PendingReviewText); reviewText != "" {
			session.LastReviewText = reviewText
		}
	}
	if completedInitialTurn {
		session.PendingReviewText = ""
		session.PendingReviewItemIDs = nil
	}
	if completedActiveTurn {
		session.ActiveTurnID = ""
	}
	if strings.TrimSpace(session.ActiveTurnID) == "" && strings.TrimSpace(session.LastReviewText) != "" {
		session.Phase = state.ReviewSessionPhaseReady
	}
	session.LastUpdatedAt = s.now()
	if strings.TrimSpace(session.ActiveTurnID) == "" {
		s.releaseFeishuRoomReviewReservations(surface)
	}
	if exitRequested || completedInitialTurn && strings.TrimSpace(session.LastReviewText) == "" {
		s.clearPendingReviewStart(surface)
		surface.ReviewSession = nil
	}
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
	if review := strings.TrimSpace(xutil.MetadataString(event.Metadata, "review")); review != "" {
		if event.ItemKind == "entered_review_mode" {
			session.TargetLabel = review
		} else {
			if strings.TrimSpace(session.LastReviewText) == "" {
				session.LastReviewText = review
			}
			if event.Kind == agentproto.EventItemCompleted {
				session.Phase = state.ReviewSessionPhaseReady
			}
		}
	}
	return true
}
