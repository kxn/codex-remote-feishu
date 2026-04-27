package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type uncommittedReviewStartState struct {
	Ready           bool
	ParentThreadID  string
	ThreadCWD       string
	SourceMessageID string
	FailureCode     string
	FailureText     string
}

func failedUncommittedReviewStart(code, text string) uncommittedReviewStartState {
	return uncommittedReviewStartState{
		FailureCode: strings.TrimSpace(code),
		FailureText: strings.TrimSpace(text),
	}
}

func (s *Service) CanOfferUncommittedReviewForFinalBlock(surfaceID string, block render.Block) bool {
	return s.resolveUncommittedReviewEntryForBlock(surfaceID, block).Ready
}

func (s *Service) handleReviewCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parsed, err := control.ParseFeishuReviewCommandText(action.Text)
	if err != nil {
		return notice(surface, "review_command_invalid", err.Error())
	}
	switch parsed.Target {
	case control.ReviewCommandTargetUncommitted:
		return s.startUncommittedReview(surface, s.resolveUncommittedReviewEntryFromCurrentContext(surface, action))
	default:
		return notice(surface, "review_command_invalid", "当前仅支持 `/review uncommitted`。")
	}
}

func (s *Service) startUncommittedReview(surface *state.SurfaceConsoleRecord, start uncommittedReviewStartState) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if active := s.activeReviewSession(surface); active != nil {
		return notice(surface, "review_session_active", "当前已经在审阅中；请直接继续提问，或使用“放弃审阅”/“按审阅意见继续修改”退出。")
	}
	if !start.Ready {
		return notice(surface, start.FailureCode, start.FailureText)
	}
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhasePending,
		ParentThreadID:  strings.TrimSpace(start.ParentThreadID),
		ThreadCWD:       strings.TrimSpace(start.ThreadCWD),
		SourceMessageID: strings.TrimSpace(start.SourceMessageID),
		StartedAt:       s.now(),
		LastUpdatedAt:   s.now(),
	}
	return []eventcontract.Event{
		{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:     "review_start_requested",
				Title:    "正在进入审阅",
				Text:     "正在为当前待提交内容创建独立审阅会话。",
				ThemeKey: "system",
			},
		},
		{
			Kind:             eventcontract.KindAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandReviewStart,
				Origin: agentproto.Origin{
					Surface:   surface.SurfaceSessionID,
					UserID:    surface.ActorUserID,
					ChatID:    surface.ChatID,
					MessageID: strings.TrimSpace(start.SourceMessageID),
				},
				Target: agentproto.Target{
					ThreadID: strings.TrimSpace(start.ParentThreadID),
				},
				Review: agentproto.ReviewRequest{
					Delivery: agentproto.ReviewDeliveryDetached,
					Target: agentproto.ReviewTarget{
						Kind: agentproto.ReviewTargetKindUncommittedChanges,
					},
				},
			},
		},
	}
}

func (s *Service) resolveUncommittedReviewEntryFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action) uncommittedReviewStartState {
	if surface == nil {
		return failedUncommittedReviewStart("", "")
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return failedUncommittedReviewStart("review_not_attached", s.notAttachedText(surface))
	}
	lifecycleID := ""
	if action.Inbound != nil {
		lifecycleID = action.Inbound.CardDaemonLifecycleID
	}
	record := s.LookupFinalCardByMessageID(surface.SurfaceSessionID, action.MessageID, lifecycleID)
	if record == nil {
		return failedUncommittedReviewStart("review_source_not_found", "当前结果卡片已经不可用，请重新获取最新结果后再进入审阅。")
	}
	if strings.TrimSpace(record.InstanceID) != strings.TrimSpace(surface.AttachedInstanceID) {
		return failedUncommittedReviewStart("review_source_instance_changed", "当前已经切换到其他实例，请重新获取这条结果对应的最新卡片后再进入审阅。")
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return failedUncommittedReviewStart("review_instance_missing", "当前实例已经不可用，请稍后重试。")
	}
	parentThreadID := strings.TrimSpace(record.ThreadID)
	if parentThreadID == "" {
		return failedUncommittedReviewStart("review_source_thread_missing", "当前结果缺少可审阅的线程上下文，请重新获取最新结果后再试。")
	}
	parentThread := inst.Threads[parentThreadID]
	if parentThread == nil {
		return failedUncommittedReviewStart("review_parent_thread_missing", "当前结果对应的会话已不可用，请重新获取最新结果后再试。")
	}
	return s.resolveUncommittedReviewEntryFromThread(
		parentThreadID,
		firstNonEmpty(strings.TrimSpace(parentThread.CWD), strings.TrimSpace(inst.WorkspaceRoot)),
		strings.TrimSpace(action.MessageID),
	)
}

func (s *Service) resolveUncommittedReviewEntryFromCurrentContext(surface *state.SurfaceConsoleRecord, action control.Action) uncommittedReviewStartState {
	if surface == nil {
		return failedUncommittedReviewStart("", "")
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return failedUncommittedReviewStart("review_not_attached", s.notAttachedText(surface))
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return failedUncommittedReviewStart("review_instance_missing", "当前实例已经不可用，请稍后重试。")
	}
	threadID := s.currentDetourSourceThread(surface, inst)
	if threadID == "" {
		return failedUncommittedReviewStart("review_requires_thread", "当前没有可审阅的会话，请先 /use 选择一个会话。")
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return failedUncommittedReviewStart("review_current_thread_missing", "当前会话已经不可用，请先 /use 重新选择一个会话。")
	}
	return s.resolveUncommittedReviewEntryFromThread(
		threadID,
		firstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(inst.WorkspaceRoot)),
		strings.TrimSpace(action.MessageID),
	)
}

func (s *Service) resolveUncommittedReviewEntryForBlock(surfaceID string, block render.Block) uncommittedReviewStartState {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return failedUncommittedReviewStart("", "")
	}
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		instanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	if instanceID == "" {
		return failedUncommittedReviewStart("", "")
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return failedUncommittedReviewStart("", "")
	}
	threadID := strings.TrimSpace(block.ThreadID)
	if threadID == "" {
		return failedUncommittedReviewStart("", "")
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return failedUncommittedReviewStart("", "")
	}
	return s.resolveUncommittedReviewEntryFromThread(
		threadID,
		firstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(inst.WorkspaceRoot)),
		"",
	)
}

func (s *Service) resolveUncommittedReviewEntryFromThread(parentThreadID, cwd, sourceMessageID string) uncommittedReviewStartState {
	parentThreadID = strings.TrimSpace(parentThreadID)
	if parentThreadID == "" {
		return failedUncommittedReviewStart("review_source_thread_missing", "当前结果缺少可审阅的线程上下文，请重新获取最新结果后再试。")
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return failedUncommittedReviewStart("review_parent_cwd_missing", "当前无法恢复原始会话的工作目录，请重新选择会话后再试。")
	}
	info, err := gitmeta.InspectWorkspace(cwd, gitmeta.InspectOptions{IncludeStatus: true})
	if err != nil {
		return failedUncommittedReviewStart("review_git_state_unavailable", "当前无法检查工作区的 Git 状态，请稍后重试。")
	}
	if !info.InRepo() {
		return failedUncommittedReviewStart("review_not_in_repo", "当前会话工作目录不在 Git 仓库内，无法审阅待提交内容。")
	}
	if !info.Status.Dirty {
		return failedUncommittedReviewStart("review_repo_clean", "当前 Git 工作区没有未提交内容，无法发起待提交内容审阅。")
	}
	return uncommittedReviewStartState{
		Ready:           true,
		ParentThreadID:  parentThreadID,
		ThreadCWD:       cwd,
		SourceMessageID: strings.TrimSpace(sourceMessageID),
	}
}
