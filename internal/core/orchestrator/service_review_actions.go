package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

const reviewApplyPromptPrefix = "请根据以下审阅意见继续修改：\n\n"

func (s *Service) startReviewFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if support, ok := s.reviewCommandSupportForSurface(surface, ""); ok && !support.DispatchAllowed {
		return s.commandSupportNotice(surface, support)
	}
	return s.startReview(surface, s.resolveUncommittedReviewStartFromFinalCard(surface, action))
}

func (s *Service) beginReviewSessionFollowUp(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || surface.ReviewSession == nil {
		return notice(surface, "review_session_inactive", "当前没有可继续追问的审阅会话。")
	}
	if blocked := s.blockReviewSessionBackendMismatch(surface); blocked != nil {
		return blocked
	}
	session := s.validReviewSession(surface)
	if session == nil {
		return notice(surface, "review_result_not_ready", "当前审阅结果尚未就绪，请等本轮审阅完成后再继续追问。")
	}
	if blocked := reviewSessionActionCardExpired(surface, session, action); blocked != nil {
		return blocked
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		return notice(surface, "request_pending", pendingRequestNoticeText(pending))
	}
	if reviewSessionTurnActive(surface, session) {
		return notice(surface, "review_turn_active", "当前审阅仍在处理中，请等本轮完成后再继续追问。")
	}
	if session.Phase != state.ReviewSessionPhaseReady || strings.TrimSpace(session.LastReviewText) == "" {
		return notice(surface, "review_result_not_ready", "当前审阅结果尚未就绪，请等本轮审阅完成后再继续追问。")
	}
	s.ensureReviewSessionParentSelection(surface, session)
	session.AwaitingFollowUpText = true
	session.LastUpdatedAt = s.now()
	return notice(surface, "review_follow_up_waiting_text", "请发送一条文字追问审阅；下一条文字只会发送到当前审阅会话。")
}

func (s *Service) discardReviewSession(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || surface.ReviewSession == nil {
		return notice(surface, "review_session_inactive", "当前没有进行中的审阅会话。")
	}
	if blocked := s.blockReviewSessionBackendMismatch(surface); blocked != nil {
		return blocked
	}
	session := s.validReviewSession(surface)
	if session == nil {
		return notice(surface, "review_result_not_ready", "当前审阅会话尚未建立完成，请等待完成或使用 `/stop`。")
	}
	if blocked := reviewSessionActionCardExpired(surface, session, action); blocked != nil {
		return blocked
	}
	if reviewSessionTurnActive(surface, session) {
		if session.ExitRequested {
			return notice(surface, "review_discard_requested", "正在停止当前审阅；完成后会自动退出。")
		}
		threadID := strings.TrimSpace(session.ReviewThreadID)
		turnID := strings.TrimSpace(session.ActiveTurnID)
		if threadID == "" || turnID == "" {
			return notice(surface, "review_turn_active", "当前审阅仍在启动或处理中，请等待进入可停止状态后重试，或使用 `/stop`。")
		}
		session.ExitRequested = true
		session.LastUpdatedAt = s.now()
		s.markRemoteTurnInterruptRequested(surface.AttachedInstanceID, threadID, turnID)
		return []eventcontract.Event{
			{
				Kind:             eventcontract.KindAgentCommand,
				SurfaceSessionID: surface.SurfaceSessionID,
				Command: &agentproto.Command{
					Kind: agentproto.CommandTurnInterrupt,
					Origin: agentproto.Origin{
						Surface: surface.SurfaceSessionID,
						UserID:  surface.ActorUserID,
						ChatID:  surface.ChatID,
					},
					Target: agentproto.Target{ThreadID: threadID, TurnID: turnID},
				},
			},
			{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code:     "review_discard_requested",
					Title:    "正在退出审阅",
					Text:     "已发送停止请求；当前审阅结束后会自动退出。",
					ThemeKey: "system",
				},
			},
		}
	}
	s.ensureReviewSessionParentSelection(surface, session)
	s.releaseFeishuRoomReviewReservations(surface)
	s.clearPendingReviewStart(surface)
	surface.ReviewSession = nil
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:     "review_discarded",
			Title:    "已退出审阅",
			Text:     "已退出当前审阅会话。",
			ThemeKey: "system",
		},
	}}
}

func (s *Service) applyReviewSessionResult(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if surface.ReviewSession == nil {
		return notice(surface, "review_session_inactive", "当前没有进行中的审阅会话。")
	}
	if blocked := s.blockReviewSessionBackendMismatch(surface); blocked != nil {
		return blocked
	}
	session := s.validReviewSession(surface)
	if session == nil {
		return notice(surface, "review_result_not_ready", "当前审阅结果尚未就绪，请等本轮审阅完成后再继续修改。")
	}
	if blocked := reviewSessionActionCardExpired(surface, session, action); blocked != nil {
		return blocked
	}
	parentThreadID := strings.TrimSpace(session.ParentThreadID)
	reviewText := strings.TrimSpace(session.LastReviewText)
	if parentThreadID == "" {
		return notice(surface, "review_parent_thread_missing", "当前审阅会话缺少原始会话上下文，请重新进入审阅后再试。")
	}
	if reviewSessionTurnActive(surface, session) {
		return notice(surface, "review_turn_active", "当前审阅仍在处理中，请等本轮完成后再继续修改。")
	}
	if session.Phase != state.ReviewSessionPhaseReady || reviewText == "" {
		return notice(surface, "review_result_not_ready", "当前审阅结果尚未就绪，请等本轮审阅完成后再继续修改。")
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	cwd := reviewSessionCWD(inst, session)
	if strings.TrimSpace(cwd) == "" {
		return notice(surface, "review_parent_cwd_missing", "当前无法恢复原始会话的工作目录，请重新选择会话后再继续修改。")
	}
	if !s.feishuRoomActiveSlotAvailableAfterReviewRelease(surface) {
		return notice(surface, "room_workspace_active", "当前群内已有机器人正在处理这个 workspace，请等待完成后再发送。")
	}
	promptText := reviewApplyPromptPrefix + reviewText
	sourceMessageID := xutil.FirstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(session.SourceMessageID))
	s.ensureReviewSessionParentSelection(surface, session)
	s.releaseFeishuRoomReviewReservations(surface)
	s.clearPendingReviewStart(surface)
	surface.ReviewSession = nil
	events := []eventcontract.Event{
		{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:     "review_apply_requested",
				Title:    "正在继续修改",
				Text:     "已退出审阅，正在把审阅意见带回原会话继续修改。",
				ThemeKey: "system",
			},
		},
	}
	return append(events, s.enqueueQueueItemWithTarget(
		surface,
		sourceMessageID,
		promptText,
		nil,
		[]agentproto.Input{{
			Type: agentproto.InputText,
			Text: promptText,
		}},
		parentThreadID,
		cwd,
		surface.RouteMode,
		surface.PromptOverride,
		agentproto.PromptExecutionModeResumeExisting,
		"",
		agentproto.SurfaceBindingPolicyKeepSurfaceSelection,
		"",
		true,
	)...)
}

func reviewSessionActionCardExpired(surface *state.SurfaceConsoleRecord, session *state.ReviewSessionRecord, action control.Action) []eventcontract.Event {
	if surface == nil || session == nil || !action.IsCardAction() {
		return nil
	}
	currentMessageID := strings.TrimSpace(session.ActionMessageID)
	if currentMessageID == "" || currentMessageID == strings.TrimSpace(action.MessageID) {
		return nil
	}
	return notice(surface, "review_action_card_expired", "这张审阅结果卡已经失效，请在最新的审阅结果卡上继续操作。")
}
