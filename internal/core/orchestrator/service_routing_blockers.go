package orchestrator

import (
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) blockThreadSwitch(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.progress.surfaceHasPendingCompact(surface) {
		return notice(surface, "thread_switch_compacting", "当前正在压缩上下文，暂时不能切换会话。请等待完成、/stop，或先 /detach。")
	}
	if s.surfaceHasPendingSteer(surface) {
		return notice(surface, "thread_switch_steering", "当前正在把排队输入并入本轮执行，暂时不能切换会话。请稍候再试。")
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching:
				return notice(surface, "thread_switch_dispatching", "当前请求正在派发，暂时不能切换会话。")
			case state.QueueItemRunning:
				return notice(surface, "thread_switch_running", "当前请求正在执行，暂时不能切换会话。")
			}
		}
	}
	if len(surface.QueuedQueueItemIDs) != 0 {
		return notice(surface, "thread_switch_queued", "当前还有排队消息，暂时不能切换会话。请等待队列清空、/stop，或 /detach。")
	}
	return nil
}

func (s *Service) blockFreshThreadAttach(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
		return blocked
	}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadRouteExit(surface); blocked != nil {
			return blocked
		}
	} else if blocked := s.blockThreadSwitch(surface); blocked != nil {
		return blocked
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceNeedsDelayedDetach(surface, inst) {
		return notice(surface, "thread_attach_requires_detach", s.threadAttachRequiresDetachText(surface))
	}
	return nil
}

func (s *Service) surfaceHasRouteMutationRequestState(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	return surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil || s.activePathPicker(surface) != nil
}

func (s *Service) blockRouteMutationForRequestState(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.activePathPicker(surface) != nil {
		return notice(surface, "path_picker_active", "当前正在进行路径选择，请先在卡片里确认或取消；如需查看状态，可继续使用 /status。")
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", pendingRequestNoticeText(activePendingRequest(surface)))
	}
	return nil
}

func (s *Service) blockActionForActivePathPicker(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || s.activePathPicker(surface) == nil {
		return nil
	}
	switch action.Kind {
	case control.ActionStatus,
		control.ActionTextMessage,
		control.ActionImageMessage,
		control.ActionFileMessage,
		control.ActionReactionCreated,
		control.ActionMessageRecalled,
		control.ActionPathPickerEnter,
		control.ActionPathPickerUp,
		control.ActionPathPickerSelect,
		control.ActionPathPickerPage,
		control.ActionPathPickerConfirm,
		control.ActionPathPickerCancel:
		return nil
	default:
		return notice(surface, "path_picker_active", "当前正在进行路径选择，请先在卡片里确认或取消；如需查看状态，可继续使用 /status。")
	}
}

func (s *Service) blockActionForActiveTargetPicker(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil || !s.targetPickerHasBlockingProcessing(surface) {
		return nil
	}
	switch action.Kind {
	case control.ActionStatus,
		control.ActionReactionCreated,
		control.ActionMessageRecalled,
		control.ActionTargetPickerCancel:
		return nil
	default:
		return notice(surface, "target_picker_processing", s.targetPickerProcessingBlockedText(surface))
	}
}

func (s *Service) targetPickerProcessingBlockedText(surface *state.SurfaceConsoleRecord) string {
	record := s.activeTargetPicker(surface)
	if record != nil && record.PendingKind == targetPickerPendingWorktreeCreate {
		return "当前正在创建 Worktree 工作区，请等待完成或取消；如需查看状态，可继续使用 /status。"
	}
	return "当前正在导入 Git 工作区，请等待完成或取消；如需查看状态，可继续使用 /status。"
}

func (s *Service) blockNewThreadPreparation(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if s.progress.surfaceHasPendingCompact(surface) {
		return notice(surface, "new_thread_blocked_compacting", "当前正在压缩上下文，暂时不能新建会话。请等待完成、/stop，或先 /detach。")
	}
	if s.surfaceHasPendingSteer(surface) {
		return notice(surface, "new_thread_blocked_steering", "当前正在把排队输入并入本轮执行，暂时不能新建会话。请稍候再试。")
	}
	if review := s.activeReviewSession(surface); review != nil && strings.TrimSpace(review.ActiveTurnID) != "" {
		return notice(surface, "new_thread_blocked_review_running", "当前审阅请求正在执行，暂时不能新建会话。请等待完成，或先 /stop 结束当前审阅。")
	}
	if item := s.preparedNewThreadActiveItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching:
			return notice(surface, "new_thread_dispatching", "当前新会话的首条消息正在派发，暂时不能再次 /new。")
		case state.QueueItemRunning:
			return notice(surface, "new_thread_running", "当前新会话的首条消息正在执行，暂时不能再次 /new。")
		}
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching:
				return notice(surface, "new_thread_blocked_dispatching", "当前请求正在派发，暂时不能新建会话。")
			case state.QueueItemRunning:
				return notice(surface, "new_thread_blocked_running", "当前请求正在执行，暂时不能新建会话。")
			}
		}
	}
	return nil
}

func (s *Service) blockPreparedNewThreadRouteExit(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady {
		return nil
	}
	if item := s.preparedNewThreadActiveItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching:
			return notice(surface, "new_thread_switch_dispatching", "当前新会话的首条消息正在派发，暂时不能切换目标。请等待它落地，或直接 /detach。")
		case state.QueueItemRunning:
			return notice(surface, "new_thread_switch_running", "当前新会话的首条消息正在执行，暂时不能切换目标。请等待它完成，或直接 /detach。")
		}
	}
	return nil
}

func (s *Service) blockPreparedNewThreadReprepare(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady {
		return nil
	}
	if item := s.preparedNewThreadActiveItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching:
			return notice(surface, "new_thread_dispatching", "当前新会话的首条消息正在派发，暂时不能再次 /new。")
		case state.QueueItemRunning:
			return notice(surface, "new_thread_running", "当前新会话的首条消息正在执行，暂时不能再次 /new。")
		}
	}
	return nil
}

func (s *Service) preparedNewThreadActiveItem(surface *state.SurfaceConsoleRecord) *state.QueueItemRecord {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady || surface.ActiveQueueItemID == "" {
		return nil
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady {
		return nil
	}
	switch item.Status {
	case state.QueueItemDispatching, state.QueueItemRunning:
		return item
	default:
		return nil
	}
}

func (s *Service) preparedNewThreadQueuedItem(surface *state.SurfaceConsoleRecord) *state.QueueItemRecord {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady {
		return nil
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady || item.Status != state.QueueItemQueued {
			continue
		}
		return item
	}
	return nil
}

func (s *Service) preparedNewThreadHasPendingCreate(surface *state.SurfaceConsoleRecord) bool {
	return s.preparedNewThreadActiveItem(surface) != nil || s.preparedNewThreadQueuedItem(surface) != nil
}

func (s *Service) clearPreparedNewThread(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.PreparedThreadCWD = ""
	surface.PreparedFromThreadID = ""
	surface.PreparedAt = time.Time{}
}

func (s *Service) unboundInputBlocked(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	switch surface.RouteMode {
	case state.RouteModeFollowLocal:
		if surface.SelectedThreadID != "" && s.surfaceOwnsThread(surface, surface.SelectedThreadID) {
			return nil
		}
		return notice(surface, "follow_waiting", "当前已进入跟随模式，但还没有可接管的 VS Code 会话。请先在 VS Code 里实际操作一次会话，或通过 /use 选择当前实例已知会话。")
	case state.RouteModeNewThreadReady:
		if strings.TrimSpace(surface.PreparedThreadCWD) != "" {
			return nil
		}
		return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
	default:
		if surface.SelectedThreadID != "" && s.surfaceOwnsThread(surface, surface.SelectedThreadID) {
			return nil
		}
		if s.surfaceIsHeadless(surface) {
			return notice(surface, "thread_unbound", "当前还没有选择会话；请先 /use 选择一个会话，或直接发送文本开启新会话（也可 /new 先进入待命）。")
		}
		return notice(surface, "thread_unbound", "当前还没有选择会话，请先 /use 选择一个会话，或执行 /follow 进入跟随模式。")
	}
}

func (s *Service) autoPromptUseThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []eventcontract.Event {
	if surface == nil || inst == nil {
		return nil
	}
	if s.surfaceIsHeadless(surface) {
		if workspaceKey := normalizeWorkspaceClaimKey(s.surfaceCurrentWorkspaceKey(surface)); workspaceKey != "" {
			return s.openLockedWorkspaceTargetPicker(surface, workspaceKey, true)
		}
	}
	if len(visibleThreads(inst)) == 0 {
		return nil
	}
	return s.presentThreadSelection(surface, false)
}

func (s *Service) threadSelectionSubtitle(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, thread *state.ThreadRecord) string {
	subtitle := threadSelectionSubtitle(thread, thread.ThreadID)
	status := ""
	owner := s.threadClaimSurface(thread.ThreadID)
	switch {
	case surface != nil && s.surfaceOwnsThread(surface, thread.ThreadID):
		if surface.RouteMode == state.RouteModeFollowLocal {
			status = "当前跟随"
		} else {
			status = "当前会话"
		}
	case owner != nil:
		switch s.threadKickStatus(inst, owner, thread.ThreadID) {
		case threadKickIdle:
			status = "已被其他飞书会话占用，可强踢"
		case threadKickQueued:
			status = "已被其他飞书会话占用，对方队列未空"
		case threadKickRunning:
			status = "已被其他飞书会话占用，对方正在执行"
		}
	default:
		status = "可切换"
	}
	if status == "" {
		return subtitle
	}
	if subtitle == "" {
		return status
	}
	return subtitle + "\n" + status
}

func (s *Service) restoreStagedInputs(surface *state.SurfaceConsoleRecord, sourceMessageIDs []string) {
	if surface == nil || len(sourceMessageIDs) == 0 {
		return
	}
	allowed := map[string]bool{}
	for _, messageID := range sourceMessageIDs {
		if strings.TrimSpace(messageID) != "" {
			allowed[messageID] = true
		}
	}
	for _, image := range surface.StagedImages {
		if image == nil || image.State != state.ImageBound || !allowed[image.SourceMessageID] {
			continue
		}
		image.State = state.ImageStaged
	}
	for _, file := range surface.StagedFiles {
		if file == nil || file.State != state.FileBound || !allowed[file.SourceMessageID] {
			continue
		}
		file.State = state.FileStaged
	}
}

func (s *Service) surfaceNeedsDelayedDetach(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	if surface == nil {
		return false
	}
	if inst != nil && !inst.Online {
		return false
	}
	if s.progress.surfaceHasPendingCompact(surface) {
		return true
	}
	if s.surfaceHasPendingSteer(surface) {
		return true
	}
	if binding := s.remoteBindingForSurface(surface); binding != nil && !s.surfaceHasPreStartRemoteDispatch(surface) {
		return true
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemRunning:
				return true
			case state.QueueItemDispatching:
				return !s.surfaceHasPreStartRemoteDispatch(surface)
			}
		}
	}
	return inst != nil && inst.ActiveTurnID != "" && s.surfaceOwnsThread(surface, inst.ActiveThreadID)
}

func (s *Service) surfaceHasPreStartRemoteDispatch(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || surface.AttachedInstanceID == "" || surface.ActiveQueueItemID == "" {
		return false
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.Status != state.QueueItemDispatching {
		return false
	}
	binding := s.remoteBindingForSurface(surface)
	if binding == nil || binding.QueueItemID != "" && binding.QueueItemID != item.ID {
		return false
	}
	return strings.TrimSpace(binding.TurnID) == "" && binding.StartedAt.IsZero() && !binding.AnyOutputSeen
}
