package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

const reviewApplyPromptPrefix = "请根据以下审阅意见继续修改：\n\n"

func (s *Service) startReviewFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	return s.startReview(surface, s.resolveUncommittedReviewStartFromFinalCard(surface, action))
}

func (s *Service) discardReviewSession(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || s.activeReviewSession(surface) == nil {
		return notice(surface, "review_session_inactive", "当前没有进行中的审阅会话。")
	}
	surface.ReviewSession = nil
	return []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:     "review_discarded",
			Title:    "已放弃审阅",
			Text:     "已退出当前审阅会话。",
			ThemeKey: "system",
		},
	}}
}

func (s *Service) applyReviewSessionResult(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	session := s.activeReviewSession(surface)
	if session == nil {
		return notice(surface, "review_session_inactive", "当前没有进行中的审阅会话。")
	}
	parentThreadID := strings.TrimSpace(session.ParentThreadID)
	reviewText := strings.TrimSpace(session.LastReviewText)
	if parentThreadID == "" {
		return notice(surface, "review_parent_thread_missing", "当前审阅会话缺少原始会话上下文，请重新进入审阅后再试。")
	}
	if reviewText == "" {
		return notice(surface, "review_result_not_ready", "当前审阅结果尚未就绪，请等本轮审阅完成后再继续修改。")
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	cwd := reviewSessionCWD(inst, session)
	if strings.TrimSpace(cwd) == "" {
		return notice(surface, "review_parent_cwd_missing", "当前无法恢复原始会话的工作目录，请重新选择会话后再继续修改。")
	}
	promptText := reviewApplyPromptPrefix + reviewText
	sourceMessageID := firstNonEmpty(strings.TrimSpace(action.MessageID), strings.TrimSpace(session.SourceMessageID))
	surface.ReviewSession = nil
	return []eventcontract.Event{
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
		{
			Kind:             eventcontract.KindAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandPromptSend,
				Origin: agentproto.Origin{
					Surface:   surface.SurfaceSessionID,
					UserID:    surface.ActorUserID,
					ChatID:    surface.ChatID,
					MessageID: sourceMessageID,
				},
				Target: agentproto.Target{
					ExecutionMode:        agentproto.PromptExecutionModeResumeExisting,
					ThreadID:             parentThreadID,
					CWD:                  cwd,
					SurfaceBindingPolicy: agentproto.SurfaceBindingPolicyKeepSurfaceSelection,
				},
				Prompt: agentproto.Prompt{
					Inputs: []agentproto.Input{{
						Type: agentproto.InputText,
						Text: promptText,
					}},
				},
				Overrides: agentproto.PromptOverrides{
					Model:           surface.PromptOverride.Model,
					ReasoningEffort: surface.PromptOverride.ReasoningEffort,
					AccessMode:      surface.PromptOverride.AccessMode,
					PlanMode:        string(state.NormalizePlanModeSetting(surface.PlanMode)),
				},
			},
		},
	}
}
