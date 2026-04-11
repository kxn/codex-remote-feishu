package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) prepareNewThread(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return notice(surface, "new_thread_disabled_vscode", "当前处于 vscode 模式，`/new` 只在 normal 模式可用。请先 `/mode normal`，或继续通过 follow / `/use` 使用当前 VS Code 会话。")
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", pendingRequestNoticeText(activePendingRequest(surface)))
	}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadReprepare(surface); blocked != nil {
			return blocked
		}
		cwd := strings.TrimSpace(surface.PreparedThreadCWD)
		if cwd == "" {
			if fallbackCWD, fallbackThreadID, ok := s.prepareNewThreadBase(surface, inst); ok {
				surface.PreparedThreadCWD = fallbackCWD
				surface.PreparedFromThreadID = fallbackThreadID
				cwd = fallbackCWD
			}
		}
		if cwd == "" {
			return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
		}
		discarded := countPendingDrafts(surface)
		events := s.discardDrafts(surface)
		surface.PreparedAt = s.now()
		if discarded == 0 {
			return append(events, notice(surface, "already_new_thread_ready", "当前已经在新建会话待命状态。下一条文本会创建新会话。")...)
		}
		return append(events, notice(surface, "new_thread_ready_reset", fmt.Sprintf("已丢弃 %d 条未发送输入。下一条文本会创建新会话。", discarded))...)
	}
	cwd, threadID, ok := s.prepareNewThreadBase(surface, inst)
	if !ok {
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return notice(surface, "new_thread_cwd_missing", "当前工作区缺少可继承的工作目录，暂时无法新建会话。请先 /list 切换工作区，或稍后重试。")
		}
		return notice(surface, "new_thread_requires_bound_thread", "当前必须先绑定并接管一个会话，才能基于它的新建会话。请先 /use，或在 follow 模式下等到已跟随到会话。")
	}
	if blocked := s.blockNewThreadPreparation(surface); blocked != nil {
		return blocked
	}
	discarded := countPendingDrafts(surface)
	events := s.discardDrafts(surface)
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.PreparedThreadCWD = cwd
	surface.PreparedFromThreadID = threadID
	surface.PreparedAt = s.now()
	events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeNewThreadReady)...)
	events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeNewThreadReady), preparedNewThreadSelectionTitle(), "")...)
	text := "已清空当前远端上下文。下一条文本会创建新会话。"
	if discarded > 0 {
		text = fmt.Sprintf("已清空当前远端上下文，并丢弃 %d 条未发送输入。下一条文本会创建新会话。", discarded)
	}
	return append(events, notice(surface, "new_thread_ready", text)...)
}

func (s *Service) prepareNewThreadBase(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) (string, string, bool) {
	if surface == nil || inst == nil {
		return "", "", false
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
		workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
		if workspaceKey == "" {
			return "", "", false
		}
		threadID := strings.TrimSpace(surface.SelectedThreadID)
		if threadID == "" || !s.surfaceOwnsThread(surface, threadID) || !threadVisible(inst.Threads[threadID]) {
			return workspaceKey, "", true
		}
		return workspaceKey, threadID, true
	}

	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" || !s.surfaceOwnsThread(surface, threadID) {
		return "", "", false
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return "", "", false
	}
	cwd := strings.TrimSpace(thread.CWD)
	if cwd == "" {
		return "", "", false
	}
	return cwd, threadID, true
}

func preparedNewThreadSelectionTitle() string {
	return "新建会话（等待首条消息）"
}

func clearAutoContinueRuntime(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.AutoContinue = state.AutoContinueRuntimeRecord{}
}

func parseProductMode(value string) (state.ProductMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "normal":
		return state.ProductModeNormal, true
	case "vscode", "vs-code", "vs_code":
		return state.ProductModeVSCode, true
	default:
		return "", false
	}
}

func (s *Service) handleModeCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	current := s.normalizeSurfaceProductMode(surface)
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModeCommandView(surface))}
	}
	if len(parts) != 2 {
		return notice(surface, "surface_mode_usage", "用法：/mode 查看当前状态；/mode normal；/mode vscode。")
	}
	target, ok := parseProductMode(parts[1])
	if !ok {
		return notice(surface, "surface_mode_usage", "用法：/mode 查看当前状态；/mode normal；/mode vscode。")
	}
	if target == current {
		return notice(surface, "surface_mode_current", fmt.Sprintf("当前已处于 %s 模式。", target))
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceHasLiveRemoteWork(surface) || s.surfaceNeedsDelayedDetach(surface, inst) {
		return notice(surface, "surface_mode_busy", "当前仍有执行中的 turn、派发中的请求或排队消息，暂时不能切换模式。请等待完成、/stop，或先 /detach。")
	}

	events := s.discardDrafts(surface)
	pending := surface.PendingHeadless
	events = append(events, s.finalizeDetachedSurface(surface)...)
	if pending != nil {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       pending.InstanceID,
				ThreadID:         pending.ThreadID,
				ThreadTitle:      pending.ThreadTitle,
				ThreadCWD:        pending.ThreadCWD,
			},
		})
	}
	surface.ProductMode = target
	return append(events, notice(surface, "surface_mode_switched", fmt.Sprintf("已切换到 %s 模式。当前没有接管中的目标。", target))...)
}

func (s *Service) handleAutoContinueCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{s.commandViewEvent(surface, s.buildAutoContinueCommandView(surface))}
	}
	if len(parts) != 2 {
		return notice(surface, "auto_continue_usage", "用法：`/autowhip` 查看当前状态；`/autowhip on`；`/autowhip off`。")
	}

	switch strings.ToLower(parts[1]) {
	case "on", "enable", "enabled", "true":
		if surface.AutoContinue.Enabled {
			return notice(surface, "auto_continue_enabled", "当前飞书会话的 autowhip 已开启。")
		}
		clearAutoContinueRuntime(surface)
		surface.AutoContinue.Enabled = true
		return notice(surface, "auto_continue_enabled", "已开启当前飞书会话的 autowhip。daemon 重启后不会恢复之前的 autowhip 状态。")
	case "off", "disable", "disabled", "false":
		clearAutoContinueRuntime(surface)
		return notice(surface, "auto_continue_disabled", "已关闭当前飞书会话的 autowhip。")
	default:
		return notice(surface, "auto_continue_usage", "用法：`/autowhip` 查看当前状态；`/autowhip on`；`/autowhip off`。")
	}
}

func (s *Service) handleModelCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{s.commandViewEvent(surface, s.buildModelCommandView(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.Model = ""
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		return notice(surface, "surface_override_cleared", "已清除飞书临时模型覆盖。之后从飞书发送的消息将恢复使用底层真实配置。")
	}
	if len(parts) > 3 {
		return notice(surface, "surface_override_usage", "用法：`/model` 查看当前配置；`/model <模型>`；`/model <模型> <推理强度>`；`/model clear`。")
	}
	override := surface.PromptOverride
	override.Model = parts[1]
	if len(parts) == 3 {
		if !looksLikeReasoningEffort(parts[2]) {
			return notice(surface, "surface_override_usage", "推理强度建议使用 `low`、`medium`、`high` 或 `xhigh`。")
		}
		override.ReasoningEffort = strings.ToLower(parts[2])
	}
	surface.PromptOverride = override
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时模型覆盖。"))
}

func (s *Service) handleReasoningCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{s.commandViewEvent(surface, s.buildReasoningCommandView(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) == 2 && isClearCommand(parts[1]) {
		surface.PromptOverride.ReasoningEffort = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		return notice(surface, "surface_override_reasoning_cleared", "已清除飞书临时推理强度覆盖。")
	}
	if len(parts) != 2 || !looksLikeReasoningEffort(parts[1]) {
		return notice(surface, "surface_override_usage", "用法：`/reasoning` 查看当前配置；`/reasoning <推理强度>`；`/reasoning clear`。")
	}
	surface.PromptOverride.ReasoningEffort = strings.ToLower(parts[1])
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_override_updated", formatOverrideNotice(summary, "已更新飞书临时推理强度覆盖。"))
}

func (s *Service) handleAccessCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{s.commandViewEvent(surface, s.buildAccessCommandView(surface))}
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if len(parts) != 2 {
		return notice(surface, "surface_access_usage", "用法：`/access` 查看当前配置；`/access full`；`/access confirm`；`/access clear`。")
	}
	if isClearCommand(parts[1]) {
		surface.PromptOverride.AccessMode = ""
		surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
		summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
		return notice(surface, "surface_access_reset", formatOverrideNotice(summary, "已恢复飞书默认执行权限。"))
	}
	mode := agentproto.NormalizeAccessMode(parts[1])
	if mode == "" {
		return notice(surface, "surface_access_usage", "执行权限建议使用 `full` 或 `confirm`。")
	}
	surface.PromptOverride.AccessMode = mode
	surface.PromptOverride = compactPromptOverride(surface.PromptOverride)
	summary := s.resolveNextPromptSummary(inst, surface, "", "", state.ModelConfigRecord{})
	return notice(surface, "surface_access_updated", formatOverrideNotice(summary, "已更新飞书执行权限模式。"))
}

func (s *Service) handleText(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	text := strings.TrimSpace(action.Text)
	if text == "" && len(action.Inputs) == 0 {
		return nil
	}

	if surface.ActiveRequestCapture != nil {
		if text == "" {
			return notice(surface, "request_capture_waiting_text", "当前反馈模式只接受文本，请发送一条文字处理意见。")
		}
		return s.consumeCapturedRequestFeedback(surface, action, text)
	}
	if pending := activePendingRequest(surface); pending != nil {
		return notice(surface, "request_pending", pendingRequestNoticeText(pending))
	}
	if surface.ActiveCommandCapture != nil {
		if text == "" {
			return notice(surface, "command_capture_waiting_text", "当前输入模式只接受文本，请发送一条模型名，或重新打开 `/model` 卡片。")
		}
		return s.consumeCapturedCommandInput(surface, text)
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
		return notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；请等待它落地后再继续发送。")
	}

	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	inputs, stagedMessageIDs := s.consumeStagedInputs(surface)
	messageInputs := append([]agentproto.Input{}, action.Inputs...)
	if len(messageInputs) == 0 {
		messageInputs = []agentproto.Input{{Type: agentproto.InputText, Text: text}}
	}
	inputs = append(inputs, messageInputs...)
	if !createThread && threadID == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use 重新选择会话；normal 模式可 /new，如需跟随 VS Code 请先 /mode vscode 再 /follow。")
	}
	if createThread && strings.TrimSpace(cwd) == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
	}
	return s.enqueueQueueItem(surface, action.MessageID, action.Text, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)
}

func (s *Service) stageImage(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if surface.ActiveCommandCapture != nil {
		return notice(surface, "command_capture_waiting_text", "当前正在等待你发送一条模型名，请先发送文本，或重新打开 `/model` 卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", pendingRequestNoticeText(pending))
	}
	if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
		return notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；如需带图，请等它创建完成后再发送下一条。")
	}
	s.nextImageID++
	image := &state.StagedImageRecord{
		ImageID:          fmt.Sprintf("img-%d", s.nextImageID),
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  action.MessageID,
		LocalPath:        action.LocalPath,
		MIMEType:         action.MIMEType,
		State:            state.ImageStaged,
	}
	surface.StagedImages[image.ImageID] = image
	return []control.UIEvent{{
		Kind:             control.UIEventPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     image.ImageID,
			SourceMessageID: image.SourceMessageID,
			Status:          string(image.State),
			QueueOn:         true,
		},
	}}
}

func (s *Service) handleReactionCreated(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || !isThumbsUpReaction(action.ReactionType) {
		return nil
	}
	targetMessageID := strings.TrimSpace(action.TargetMessageID)
	if targetMessageID == "" {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil || inst.ActiveTurnID == "" || inst.ActiveThreadID == "" {
		return nil
	}
	for index, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued || item.SourceMessageID != targetMessageID {
			continue
		}
		if item.FrozenThreadID == "" || item.FrozenThreadID != inst.ActiveThreadID {
			return nil
		}
		item.Status = state.QueueItemSteering
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		s.pendingSteers[item.ID] = &pendingSteerBinding{
			InstanceID:       inst.InstanceID,
			SurfaceSessionID: surface.SurfaceSessionID,
			QueueItemID:      item.ID,
			SourceMessageID:  item.SourceMessageID,
			ThreadID:         inst.ActiveThreadID,
			TurnID:           inst.ActiveTurnID,
			QueueIndex:       index,
		}
		return []control.UIEvent{{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandTurnSteer,
				Origin: agentproto.Origin{
					Surface:   surface.SurfaceSessionID,
					UserID:    surface.ActorUserID,
					ChatID:    surface.ChatID,
					MessageID: item.SourceMessageID,
				},
				Target: agentproto.Target{
					ThreadID: inst.ActiveThreadID,
					TurnID:   inst.ActiveTurnID,
				},
				Prompt: agentproto.Prompt{
					Inputs: item.Inputs,
				},
			},
		}}
	}
	return nil
}

func (s *Service) handleMessageRecalled(surface *state.SurfaceConsoleRecord, targetMessageID string) []control.UIEvent {
	targetMessageID = strings.TrimSpace(targetMessageID)
	if surface == nil || targetMessageID == "" {
		return nil
	}
	if activeID := surface.ActiveQueueItemID; activeID != "" {
		if item := surface.QueueItems[activeID]; item != nil && queueItemHasSourceMessage(item, targetMessageID) {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return []control.UIEvent{{
					Kind:             control.UIEventNotice,
					SurfaceSessionID: surface.SurfaceSessionID,
					Notice: &control.Notice{
						Code:     "message_recall_too_late",
						Title:    "无法撤回排队",
						Text:     "这条输入已经开始执行，不能通过撤回取消。若要中断当前 turn，请发送 `/stop`。",
						ThemeKey: "system",
					},
				}}
			}
		}
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued || !queueItemHasSourceMessage(item, targetMessageID) {
			continue
		}
		item.Status = state.QueueItemDiscarded
		s.markImagesForMessages(surface, queueItemSourceMessageIDs(item), state.ImageDiscarded)
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		return s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
			QueueOff:    true,
			ThumbsDown:  true,
		}, queueItemSourceMessageIDs(item))
	}
	for _, image := range surface.StagedImages {
		if image.SourceMessageID == targetMessageID && image.State == state.ImageStaged {
			image.State = state.ImageCancelled
			return s.pendingInputEvents(surface, control.PendingInputState{
				QueueItemID: image.ImageID,
				Status:      string(image.State),
				QueueOff:    true,
				ThumbsDown:  true,
			}, []string{image.SourceMessageID})
		}
	}
	return nil
}

func isThumbsUpReaction(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return normalized == "thumbsup"
}

func (s *Service) stopSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	var events []control.UIEvent
	discarded := countPendingDrafts(surface)
	inst := s.root.Instances[surface.AttachedInstanceID]
	notice := control.Notice{
		Code:     "stop_no_active_turn",
		Title:    "没有正在运行的推理",
		Text:     "当前没有正在运行的推理。",
		ThemeKey: "system",
	}
	if inst != nil && inst.ActiveTurnID != "" {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandTurnInterrupt,
				Origin: agentproto.Origin{
					Surface: surface.SurfaceSessionID,
					UserID:  surface.ActorUserID,
					ChatID:  surface.ChatID,
				},
				Target: agentproto.Target{
					ThreadID: inst.ActiveThreadID,
					TurnID:   inst.ActiveTurnID,
				},
			},
		})
		notice = control.Notice{
			Code:     "stop_requested",
			Title:    "已发送停止请求",
			Text:     "已向当前运行中的 turn 发送停止请求。",
			ThemeKey: "system",
		}
	} else if surface.ActiveQueueItemID != "" {
		if inst != nil && !inst.Online {
			notice = s.stopOfflineNotice(surface)
		} else {
			notice = control.Notice{
				Code:     "stop_not_interruptible",
				Title:    "当前还不能停止",
				Text:     "当前请求正在派发，尚未进入可中断状态。",
				ThemeKey: "system",
			}
		}
	}

	events = append(events, s.discardDrafts(surface)...)
	clearSurfaceRequests(surface)
	if discarded > 0 {
		notice.Text += fmt.Sprintf(" 已清空 %d 条排队或暂存输入。", discarded)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	})
	return events
}

func (s *Service) cancelPendingHeadlessLaunch(surface *state.SurfaceConsoleRecord, notice *control.Notice) []control.UIEvent {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	pending := surface.PendingHeadless
	events := s.discardDrafts(surface)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandKillHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       pending.InstanceID,
			ThreadID:         pending.ThreadID,
			ThreadTitle:      pending.ThreadTitle,
			ThreadCWD:        pending.ThreadCWD,
		},
	})
	if notice != nil {
		events = append(events, control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	return events
}

func (s *Service) killHeadlessInstance(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.PendingHeadless != nil {
		return s.cancelPendingHeadlessLaunch(surface, &control.Notice{
			Code:  "headless_cancelled",
			Title: "取消恢复流程",
			Text:  "已取消当前恢复流程。",
		})
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "headless_not_found", "当前没有可结束的后台恢复流程。")
	}
	if !isHeadlessInstance(inst) {
		return notice(surface, "headless_kill_forbidden", "当前接管的是 VS Code 实例，不需要结束后台恢复。")
	}
	instanceID := inst.InstanceID
	threadID := surface.SelectedThreadID
	threadTitle := displayThreadTitle(inst, inst.Threads[threadID], threadID)
	threadCWD := ""
	if thread := inst.Threads[threadID]; thread != nil {
		threadCWD = thread.CWD
	}
	events := s.discardDrafts(surface)
	surface.PendingHeadless = nil
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events,
		control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
				ThreadID:         threadID,
				ThreadTitle:      threadTitle,
				ThreadCWD:        threadCWD,
			},
		},
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_kill_requested",
				Title: "结束后台恢复",
				Text:  "已请求结束当前后台恢复，并断开当前接管。",
			},
		},
	)
	return events
}

func (s *Service) detach(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.PendingHeadless != nil {
		return s.cancelPendingHeadlessLaunch(surface, &control.Notice{
			Code:  "detached",
			Title: "已取消恢复流程",
			Text:  fmt.Sprintf("已取消当前恢复流程。%s", s.detachedNoneText(surface)),
		})
	}
	if surface.AttachedInstanceID == "" {
		return notice(surface, "detached", s.detachedNoneText(surface))
	}
	events := s.discardDrafts(surface)
	clearSurfaceRequests(surface)
	surface.PendingHeadless = nil
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.DispatchMode = state.DispatchModeNormal
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceNeedsDelayedDetach(surface, inst) {
		surface.Abandoning = true
		s.abandoningUntil[surface.SurfaceSessionID] = s.now().Add(s.config.DetachAbandonWait)
		if binding := s.remoteBindingForSurface(surface); binding != nil && binding.TurnID != "" {
			events = append(events, control.UIEvent{
				Kind:             control.UIEventAgentCommand,
				SurfaceSessionID: surface.SurfaceSessionID,
				Command: &agentproto.Command{
					Kind: agentproto.CommandTurnInterrupt,
					Origin: agentproto.Origin{
						Surface: surface.SurfaceSessionID,
						UserID:  surface.ActorUserID,
						ChatID:  surface.ChatID,
					},
					Target: agentproto.Target{
						ThreadID: binding.ThreadID,
						TurnID:   binding.TurnID,
					},
				},
			})
		}
		return append(events, notice(surface, "detach_pending", s.detachPendingText(surface))...)
	}
	events = append(events, s.finalizeDetachedSurface(surface)...)
	return append(events, notice(surface, "detached", s.detachedText(surface))...)
}
