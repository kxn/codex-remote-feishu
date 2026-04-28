package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/gitmeta"
	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type reviewEntryContext struct {
	Ready           bool
	InstanceID      string
	ParentThreadID  string
	ThreadCWD       string
	SourceMessageID string
	FailureCode     string
	FailureText     string
}

type reviewStartState struct {
	Ready           bool
	ParentThreadID  string
	ThreadCWD       string
	SourceMessageID string
	Target          agentproto.ReviewTarget
	TargetLabel     string
	FailureCode     string
	FailureText     string
}

func failedReviewEntryContext(code, text string) reviewEntryContext {
	return reviewEntryContext{
		FailureCode: strings.TrimSpace(code),
		FailureText: strings.TrimSpace(text),
	}
}

func failedReviewStart(code, text string) reviewStartState {
	return reviewStartState{
		FailureCode: strings.TrimSpace(code),
		FailureText: strings.TrimSpace(text),
	}
}

func reviewStartFromEntry(entry reviewEntryContext, target agentproto.ReviewTarget, label string) reviewStartState {
	if !entry.Ready {
		return failedReviewStart(entry.FailureCode, entry.FailureText)
	}
	return reviewStartState{
		Ready:           true,
		ParentThreadID:  strings.TrimSpace(entry.ParentThreadID),
		ThreadCWD:       strings.TrimSpace(entry.ThreadCWD),
		SourceMessageID: strings.TrimSpace(entry.SourceMessageID),
		Target:          target.Normalized(),
		TargetLabel:     strings.TrimSpace(label),
	}
}

func (s *Service) CanOfferUncommittedReviewForFinalBlock(surfaceID string, block render.Block) bool {
	return s.resolveUncommittedReviewStartForBlock(surfaceID, block).Ready
}

func (s *Service) handleReviewCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	parsed, err := control.ParseFeishuReviewCommandText(action.Text)
	if err != nil {
		return notice(surface, "review_command_invalid", err.Error())
	}
	switch parsed.Mode {
	case control.ReviewCommandModeUncommitted:
		return s.startReview(surface, s.resolveUncommittedReviewStartFromCurrentContext(surface, action))
	case control.ReviewCommandModeCommitPicker:
		return s.openReviewCommitPicker(surface, action)
	case control.ReviewCommandModeCommitSHA:
		if action.IsCardAction() {
			if start, ok := s.resolveCommitReviewStartFromActivePicker(surface, action, parsed.CommitSHA); ok {
				return s.startReview(surface, start)
			}
		}
		if s.isReviewCommandFromFinalCard(surface, action) {
			return s.startReview(surface, s.resolveCommitReviewStartFromFinalCard(surface, action, parsed.CommitSHA))
		}
		return s.startReview(surface, s.resolveCommitReviewStartFromCurrentContext(surface, action, parsed.CommitSHA))
	case control.ReviewCommandModeCancel:
		return s.cancelReviewCommitPicker(surface, action)
	default:
		return notice(surface, "review_command_invalid", "当前暂不支持这个 review 命令。")
	}
}

func (s *Service) startReview(surface *state.SurfaceConsoleRecord, start reviewStartState) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if active := s.activeReviewSession(surface); active != nil {
		return notice(surface, "review_session_active", "当前已经在审阅中；请直接继续提问，或使用“放弃审阅”/“按审阅意见继续修改”退出。")
	}
	if !start.Ready {
		return notice(surface, start.FailureCode, start.FailureText)
	}
	s.clearReviewCommitPickerRuntime(surface)
	surface.ReviewSession = &state.ReviewSessionRecord{
		Phase:           state.ReviewSessionPhasePending,
		ParentThreadID:  strings.TrimSpace(start.ParentThreadID),
		ThreadCWD:       strings.TrimSpace(start.ThreadCWD),
		SourceMessageID: strings.TrimSpace(start.SourceMessageID),
		TargetLabel:     strings.TrimSpace(start.TargetLabel),
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
				Text:     reviewStartNoticeText(start),
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
					Target:   start.Target.Normalized(),
				},
			},
		},
	}
}

func reviewStartNoticeText(start reviewStartState) string {
	switch start.Target.Normalized().Kind {
	case agentproto.ReviewTargetKindCommit:
		if label := strings.TrimSpace(start.TargetLabel); label != "" {
			return fmt.Sprintf("正在为 %s 创建独立审阅会话。", label)
		}
	}
	return "正在为当前待提交内容创建独立审阅会话。"
}

func (s *Service) isReviewCommandFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action) bool {
	if surface == nil {
		return false
	}
	lifecycleID := ""
	if action.Inbound != nil {
		lifecycleID = action.Inbound.CardDaemonLifecycleID
	}
	return s.LookupFinalCardByMessageID(surface.SurfaceSessionID, action.MessageID, lifecycleID) != nil
}

func (s *Service) resolveReviewEntryFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action) reviewEntryContext {
	if surface == nil {
		return failedReviewEntryContext("", "")
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return failedReviewEntryContext("review_not_attached", s.notAttachedText(surface))
	}
	lifecycleID := ""
	if action.Inbound != nil {
		lifecycleID = action.Inbound.CardDaemonLifecycleID
	}
	record := s.LookupFinalCardByMessageID(surface.SurfaceSessionID, action.MessageID, lifecycleID)
	if record == nil {
		return failedReviewEntryContext("review_source_not_found", "当前结果卡片已经不可用，请重新获取最新结果后再进入审阅。")
	}
	if strings.TrimSpace(record.InstanceID) != strings.TrimSpace(surface.AttachedInstanceID) {
		return failedReviewEntryContext("review_source_instance_changed", "当前已经切换到其他实例，请重新获取这条结果对应的最新卡片后再进入审阅。")
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return failedReviewEntryContext("review_instance_missing", "当前实例已经不可用，请稍后重试。")
	}
	parentThreadID := strings.TrimSpace(record.ThreadID)
	if parentThreadID == "" {
		return failedReviewEntryContext("review_source_thread_missing", "当前结果缺少可审阅的线程上下文，请重新获取最新结果后再试。")
	}
	parentThread := inst.Threads[parentThreadID]
	if parentThread == nil {
		return failedReviewEntryContext("review_parent_thread_missing", "当前结果对应的会话已不可用，请重新获取最新结果后再试。")
	}
	return s.resolveReviewEntryFromThread(
		strings.TrimSpace(surface.AttachedInstanceID),
		parentThreadID,
		firstNonEmpty(strings.TrimSpace(parentThread.CWD), strings.TrimSpace(inst.WorkspaceRoot)),
		strings.TrimSpace(action.MessageID),
	)
}

func (s *Service) resolveReviewEntryFromCurrentContext(surface *state.SurfaceConsoleRecord, action control.Action) reviewEntryContext {
	if surface == nil {
		return failedReviewEntryContext("", "")
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return failedReviewEntryContext("review_not_attached", s.notAttachedText(surface))
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return failedReviewEntryContext("review_instance_missing", "当前实例已经不可用，请稍后重试。")
	}
	threadID := s.currentDetourSourceThread(surface, inst)
	if threadID == "" {
		return failedReviewEntryContext("review_requires_thread", "当前没有可审阅的会话，请先 /use 选择一个会话。")
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return failedReviewEntryContext("review_current_thread_missing", "当前会话已经不可用，请先 /use 重新选择一个会话。")
	}
	return s.resolveReviewEntryFromThread(
		strings.TrimSpace(surface.AttachedInstanceID),
		threadID,
		firstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(inst.WorkspaceRoot)),
		strings.TrimSpace(action.MessageID),
	)
}

func (s *Service) resolveReviewEntryForBlock(surfaceID string, block render.Block) reviewEntryContext {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return failedReviewEntryContext("", "")
	}
	instanceID := strings.TrimSpace(block.InstanceID)
	if instanceID == "" {
		instanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	if instanceID == "" {
		return failedReviewEntryContext("", "")
	}
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return failedReviewEntryContext("", "")
	}
	threadID := strings.TrimSpace(block.ThreadID)
	if threadID == "" {
		return failedReviewEntryContext("", "")
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return failedReviewEntryContext("", "")
	}
	return s.resolveReviewEntryFromThread(
		instanceID,
		threadID,
		firstNonEmpty(strings.TrimSpace(thread.CWD), strings.TrimSpace(inst.WorkspaceRoot)),
		"",
	)
}

func (s *Service) resolveReviewEntryFromThread(instanceID, parentThreadID, cwd, sourceMessageID string) reviewEntryContext {
	parentThreadID = strings.TrimSpace(parentThreadID)
	if parentThreadID == "" {
		return failedReviewEntryContext("review_source_thread_missing", "当前结果缺少可审阅的线程上下文，请重新获取最新结果后再试。")
	}
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return failedReviewEntryContext("review_parent_cwd_missing", "当前无法恢复原始会话的工作目录，请重新选择会话后再试。")
	}
	return reviewEntryContext{
		Ready:           true,
		InstanceID:      strings.TrimSpace(instanceID),
		ParentThreadID:  parentThreadID,
		ThreadCWD:       cwd,
		SourceMessageID: strings.TrimSpace(sourceMessageID),
	}
}

func (s *Service) resolveUncommittedReviewStartFromFinalCard(surface *state.SurfaceConsoleRecord, action control.Action) reviewStartState {
	return s.resolveUncommittedReviewStartFromEntry(s.resolveReviewEntryFromFinalCard(surface, action))
}

func (s *Service) resolveUncommittedReviewStartFromCurrentContext(surface *state.SurfaceConsoleRecord, action control.Action) reviewStartState {
	return s.resolveUncommittedReviewStartFromEntry(s.resolveReviewEntryFromCurrentContext(surface, action))
}

func (s *Service) resolveUncommittedReviewStartForBlock(surfaceID string, block render.Block) reviewStartState {
	return s.resolveUncommittedReviewStartFromEntry(s.resolveReviewEntryForBlock(surfaceID, block))
}

func (s *Service) resolveUncommittedReviewStartFromEntry(entry reviewEntryContext) reviewStartState {
	if !entry.Ready {
		return failedReviewStart(entry.FailureCode, entry.FailureText)
	}
	info, err := gitmeta.InspectWorkspace(entry.ThreadCWD, gitmeta.InspectOptions{IncludeStatus: true})
	if err != nil {
		return failedReviewStart("review_git_state_unavailable", "当前无法检查工作区的 Git 状态，请稍后重试。")
	}
	if !info.InRepo() {
		return failedReviewStart("review_not_in_repo", "当前会话工作目录不在 Git 仓库内，无法审阅待提交内容。")
	}
	if !info.Status.Dirty {
		return failedReviewStart("review_repo_clean", "当前 Git 工作区没有未提交内容，无法发起待提交内容审阅。")
	}
	return reviewStartFromEntry(entry, agentproto.ReviewTarget{
		Kind: agentproto.ReviewTargetKindUncommittedChanges,
	}, "未提交变更")
}
