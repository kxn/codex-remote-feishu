package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) prepareNewThread(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.prepareNewThreadWithOverlayCleanup(surface, surfaceOverlayRouteCleanupOptions{})
}

func (s *Service) prepareNewThreadPreservingTargetPicker(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.prepareNewThreadWithOverlayCleanup(surface, surfaceOverlayRouteCleanupOptions{PreserveTargetPicker: true})
}

func (s *Service) prepareNewThreadWithOverlayCleanup(surface *state.SurfaceConsoleRecord, cleanup surfaceOverlayRouteCleanupOptions) []eventcontract.Event {
	if !s.surfaceIsHeadless(surface) {
		return notice(surface, "new_thread_disabled_vscode", "当前处于 vscode 模式，`/new` 只在 headless 模式可用。请先 `/mode codex`、`/mode claude` 或 `/mode opencode`，或继续通过 follow / `/use` 使用当前 VS Code 会话。")
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
				if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
					AttachedInstanceID:   inst.InstanceID,
					RouteMode:            state.RouteModeNewThreadReady,
					PreparedThreadCWD:    fallbackCWD,
					PreparedFromThreadID: fallbackThreadID,
				}) {
					return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
				}
				cwd = fallbackCWD
			}
		}
		if cwd == "" {
			return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
		}
		discarded := countPendingDrafts(surface)
		events := s.maybeSealPlanProposalForRouteChange(surface, "当前工作目标已切换到新会话待命状态，之前的提案计划已失效。")
		clearIdleReviewSession(surface)
		clearAutoContinueRuntime(surface)
		events = append(events, s.discardDrafts(surface)...)
		s.refreshPreparedNewThreadRouteCore(surface)
		if discarded == 0 {
			return append(events, notice(surface, "already_new_thread_ready", "当前已经在新建会话待命状态。下一条文本会创建新会话。")...)
		}
		return append(events, notice(surface, "new_thread_ready_reset", fmt.Sprintf("已丢弃 %d 条未发送输入。下一条文本会创建新会话。", discarded))...)
	}
	cwd, threadID, ok := s.prepareNewThreadBase(surface, inst)
	if !ok {
		if s.surfaceIsHeadless(surface) {
			return notice(surface, "new_thread_cwd_missing", "当前工作区缺少可继承的工作目录，暂时无法新建会话。请先 /list 切换工作区，或稍后重试。")
		}
		return notice(surface, "new_thread_requires_bound_thread", "当前必须先绑定并接管一个会话，才能基于它的新建会话。请先 /use，或在 follow 模式下等到已跟随到会话。")
	}
	if blocked := s.blockNewThreadPreparation(surface); blocked != nil {
		return blocked
	}
	discarded := countPendingDrafts(surface)
	events := s.maybeSealPlanProposalForRouteChange(surface, "当前工作目标已切换到新会话待命状态，之前的提案计划已失效。")
	clearIdleReviewSession(surface)
	clearAutoContinueRuntime(surface)
	events = append(events, s.discardDrafts(surface)...)
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID:   inst.InstanceID,
		RouteMode:            state.RouteModeNewThreadReady,
		PreparedThreadCWD:    cwd,
		PreparedFromThreadID: threadID,
	}) {
		return append(events, notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")...)
	}
	events = append(events, s.cleanupContextBoundSurfaceOverlays(surface, "当前工作目标已变化", cleanup)...)
	events = append(events, s.discardStagedInputsForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeNewThreadReady)...)
	events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeNewThreadReady), preparedNewThreadSelectionTitle())...)
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
	if s.surfaceIsHeadless(surface) {
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

func (s *Service) maybePrepareImplicitNewThreadFromUnboundText(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, text string) []eventcontract.Event {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return s.maybePrepareImplicitNewThreadFromUnbound(surface, inst)
}

func (s *Service) maybePrepareImplicitNewThreadFromUnboundImage(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []eventcontract.Event {
	return s.maybePrepareImplicitNewThreadFromUnbound(surface, inst)
}

func (s *Service) maybePrepareImplicitNewThreadFromUnboundFile(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []eventcontract.Event {
	return s.maybePrepareImplicitNewThreadFromUnbound(surface, inst)
}

func (s *Service) maybePrepareImplicitNewThreadFromUnbound(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []eventcontract.Event {
	if surface == nil || inst == nil || !s.surfaceIsHeadless(surface) {
		return nil
	}
	if surface.RouteMode != state.RouteModeUnbound || strings.TrimSpace(surface.SelectedThreadID) != "" {
		return nil
	}
	if strings.TrimSpace(surface.PreparedThreadCWD) != "" {
		if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
			AttachedInstanceID:   inst.InstanceID,
			RouteMode:            state.RouteModeNewThreadReady,
			PreparedThreadCWD:    surface.PreparedThreadCWD,
			PreparedFromThreadID: surface.PreparedFromThreadID,
		}) {
			return notice(surface, "new_thread_cwd_missing", "当前工作区缺少可继承的工作目录，暂时无法新建会话。请先 /list 切换工作区，或稍后重试。")
		}
		return nil
	}
	cwd, threadID, ok := s.prepareNewThreadBase(surface, inst)
	if !ok {
		return notice(surface, "new_thread_cwd_missing", "当前工作区缺少可继承的工作目录，暂时无法新建会话。请先 /list 切换工作区，或稍后重试。")
	}
	if blocked := s.blockNewThreadPreparation(surface); blocked != nil {
		return blocked
	}
	if !s.transitionSurfaceRouteCore(surface, inst, surfaceRouteCoreState{
		AttachedInstanceID:   inst.InstanceID,
		RouteMode:            state.RouteModeNewThreadReady,
		PreparedThreadCWD:    cwd,
		PreparedFromThreadID: threadID,
	}) {
		return notice(surface, "new_thread_cwd_missing", "当前工作区缺少可继承的工作目录，暂时无法新建会话。请先 /list 切换工作区，或稍后重试。")
	}
	return nil
}

func preparedNewThreadSelectionTitle() string {
	return "新建会话（等待首条消息）"
}

func (s *Service) handleText(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	text := strings.TrimSpace(action.Text)
	if text == "" && len(action.Inputs) == 0 {
		return nil
	}
	if blocked := s.blockFeishuRoomNoWorkspaceDataPlane(surface, action); blocked != nil {
		return blocked
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
	if events, handled := s.handleReviewSessionText(surface, action, text); handled {
		return events
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		if s.surfaceIsHeadless(surface) && surfaceFeishuRoomID(surface) != "" {
			if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
				return blocked
			}
			workspaceKey := s.surfaceCurrentWorkspaceKey(surface)
			if workspaceKey == "" {
				// Save message for replay after the user selects a workspace
				// and thread from the picker.
				s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
				return s.openTargetPickerForAction(surface, action, "", nil, action.MessageID, false)
			}
			targetBackend := s.surfaceBackend(surface)
			continuation := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, targetBackend, false)
			resolution := s.resolveWorkspaceContract(surface, workspaceKey, targetBackend)
			events := s.executeResolvedWorkspaceContinuation(surface, continuation, resolution, attachWorkspaceOptions{SuppressAutoUsePrompt: true})
			inst = s.root.Instances[surface.AttachedInstanceID]
			if inst == nil {
				// Instance is starting — save message for replay when it
				// connects and the surface attaches.
				s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
				return events
			}
			return append(events, s.handleText(surface, action)...)
		}
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if surface.ContractRefreshPending && !s.surfaceInstanceCompatibleForAttach(surface, inst) {
		if surface.PendingHeadless != nil || s.surfaceHasLiveRemoteWork(surface) {
			return notice(surface, "contract_refresh_pending", "机器人配置刚发生变化，当前会话仍在处理中，稍后会自动切换到新配置。")
		}
		surface.ContractRefreshPending = false
		events := s.reconcileHeadlessSurfaceContract(surface)
		if len(events) == 0 {
			return notice(surface, "contract_refresh_unavailable", "当前会话暂时无法自动切换到新配置，请稍后重试，或 /detach 后重新连接。")
		}
		s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
		return events
	}
	detour, detourProblem := s.resolveDetourDirective(surface, inst, text)
	if detourProblem != "" {
		return notice(surface, "detour_invalid", detourProblem)
	}
	text = detour.CleanText
	sanitizedAction := action
	sanitizedAction.Text = text
	if detour.Triggered {
		sanitizedAction.Inputs = stripDetourInputs(action.Inputs)
	}
	if !detour.Triggered {
		if autoSteer := s.maybeAutoSteerReply(surface, sanitizedAction); autoSteer != nil {
			return append(s.maybeSealPlanProposalForInput(surface), autoSteer...)
		}
	}
	if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
		return blocked
	}
	if !detour.Triggered {
		if blocked := s.maybePrepareImplicitNewThreadFromUnboundText(surface, inst, text); blocked != nil {
			// Save the message for replay after the user resolves the
			// blocking condition (e.g., selects a thread from the picker).
			s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
			return blocked
		}
		if blocked := s.unboundInputBlocked(surface); blocked != nil {
			s.storePendingTextInput(surface, text, action.Inputs, action.MessageID, action.ActorUserID, action.MessageID, nil)
			return blocked
		}
		if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
			return notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；请等待它落地后再继续发送。")
		}
	}

	threadID, cwd, routeMode, createThread := freezeRoute(inst, surface)
	if detour.Triggered {
		threadID = ""
		cwd, routeMode = freezeDetourRoute(inst, surface)
		createThread = false
	}
	inputs, stagedMessageIDs, filePrompt := s.consumeStagedInputs(surface, action.ActorUserID)
	if filePrompt != "" {
		inputs = append(inputs, agentproto.Input{Type: agentproto.InputText, Text: filePrompt})
	}
	messageInputs := append([]agentproto.Input{}, sanitizedAction.Inputs...)
	if len(messageInputs) == 0 {
		if text != "" {
			messageInputs = []agentproto.Input{{Type: agentproto.InputText, Text: text}}
		} else if detour.Triggered && len(inputs) == 0 {
			s.restoreStagedInputs(surface, stagedMessageIDs)
			return notice(surface, "detour_empty_prompt", detourEmptyPromptText)
		}
	}
	inputs = append(inputs, messageInputs...)
	if !detour.Triggered && !createThread && threadID == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use 重新选择会话；headless 模式可直接发送文本开启新会话（也可 /new 先进入待命），如需跟随 VS Code 请先 /mode vscode 再 /follow。")
	}
	if strings.TrimSpace(cwd) == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		if detour.Triggered {
			return notice(surface, "detour_cwd_missing", "当前无法获取临时会话的工作目录，请先重新选择工作区或会话。")
		}
		if createThread {
			return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
		}
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use 重新选择会话；headless 模式可直接发送文本开启新会话（也可 /new 先进入待命），如需跟随 VS Code 请先 /mode vscode 再 /follow。")
	}
	events := s.maybeSealPlanProposalForInput(surface)
	if detour.Triggered {
		return append(events, s.enqueueQueueItemWithTarget(
			surface,
			action.MessageID,
			text,
			stagedMessageIDs,
			inputs,
			threadID,
			cwd,
			routeMode,
			surface.PromptOverride,
			detour.ExecutionMode,
			detour.SourceThreadID,
			detour.SurfaceBindingPolicy,
			"",
			false,
		)...)
	}
	// Clear any saved pending input — the user sent a new message that is
	// being processed, so the old pending is stale.
	s.clearPendingTextInput(surface)
	return append(events, s.enqueueQueueItem(surface, action.MessageID, action.Text, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)...)
}

func (s *Service) stageImage(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if blocked := s.blockNonTextReviewSessionInput(surface); blocked != nil {
		return blocked
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	events := []eventcontract.Event{}
	if inst == nil {
		prepared, preparedInst, handled := s.prepareFeishuRoomWorkspaceForDetachedInput(surface)
		if !handled {
			return notice(surface, "not_attached", s.notAttachedText(surface))
		}
		events = append(events, prepared...)
		inst = preparedInst
		if inst == nil && surface.PendingHeadless == nil {
			return events
		}
	}
	if inst != nil {
		if autoSteer := s.maybeAutoSteerReply(surface, action); autoSteer != nil {
			return append(s.maybeSealPlanProposalForInput(surface), autoSteer...)
		}
		if blocked := s.maybePrepareImplicitNewThreadFromUnboundImage(surface, inst); blocked != nil {
			return blocked
		}
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return append(events, blocked...)
	}
	if surface.ActiveRequestCapture != nil {
		return append(events, notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")...)
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return append(events, notice(surface, "request_pending", pendingRequestNoticeText(pending))...)
	}
	if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
		return append(events, notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；如需带图，请等它创建完成后再发送下一条。")...)
	}
	s.nextImageID++
	image := &state.StagedImageRecord{
		ImageID:          fmt.Sprintf("img-%d", s.nextImageID),
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  action.MessageID,
		ActorUserID:      action.ActorUserID,
		LocalPath:        action.LocalPath,
		MIMEType:         action.MIMEType,
		State:            state.ImageStaged,
	}
	surface.StagedImages[image.ImageID] = image
	events = append(events, s.maybeSealPlanProposalForInput(surface)...)
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     image.ImageID,
			SourceMessageID: image.SourceMessageID,
			Status:          string(image.State),
			QueueOn:         true,
		},
	})
	return events
}

func (s *Service) stageFile(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if blocked := s.blockNonTextReviewSessionInput(surface); blocked != nil {
		return blocked
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	events := []eventcontract.Event{}
	if inst == nil {
		prepared, preparedInst, handled := s.prepareFeishuRoomWorkspaceForDetachedInput(surface)
		if !handled {
			return notice(surface, "not_attached", s.notAttachedText(surface))
		}
		events = append(events, prepared...)
		inst = preparedInst
		if inst == nil && surface.PendingHeadless == nil {
			return events
		}
	}
	if inst != nil {
		if blocked := s.maybePrepareImplicitNewThreadFromUnboundFile(surface, inst); blocked != nil {
			return blocked
		}
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return append(events, blocked...)
	}
	if surface.ActiveRequestCapture != nil {
		return append(events, notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")...)
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return append(events, notice(surface, "request_pending", pendingRequestNoticeText(pending))...)
	}
	if surface.RouteMode == state.RouteModeNewThreadReady && s.preparedNewThreadHasPendingCreate(surface) {
		return append(events, notice(surface, "new_thread_first_input_pending", "当前新会话的首条消息已经在排队或发送中；如需带文件，请等它创建完成后再发送下一条。")...)
	}
	if surface.StagedFiles == nil {
		surface.StagedFiles = map[string]*state.StagedFileRecord{}
	}
	s.nextFileID++
	file := &state.StagedFileRecord{
		FileID:           fmt.Sprintf("file-%d", s.nextFileID),
		SurfaceSessionID: surface.SurfaceSessionID,
		SourceMessageID:  action.MessageID,
		ActorUserID:      action.ActorUserID,
		LocalPath:        action.LocalPath,
		FileName:         action.FileName,
		State:            state.FileStaged,
	}
	surface.StagedFiles[file.FileID] = file
	events = append(events, s.maybeSealPlanProposalForInput(surface)...)
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindPendingInput,
		SurfaceSessionID: surface.SurfaceSessionID,
		PendingInput: &control.PendingInputState{
			QueueItemID:     file.FileID,
			SourceMessageID: file.SourceMessageID,
			Status:          string(file.State),
			QueueOn:         true,
		},
	})
	return events
}

func (s *Service) prepareFeishuRoomWorkspaceForDetachedInput(surface *state.SurfaceConsoleRecord) ([]eventcontract.Event, *state.InstanceRecord, bool) {
	if surface == nil || !s.surfaceIsHeadless(surface) || surfaceFeishuRoomID(surface) == "" {
		return nil, nil, false
	}
	if blocked := s.blockFeishuRoomActiveDispatch(surface); blocked != nil {
		return blocked, nil, true
	}
	room := s.ensureFeishuRoomContextForSurface(surface)
	workspaceKey := ""
	if room != nil {
		workspaceKey = normalizeWorkspaceClaimKey(room.WorkspaceKey)
	}
	if workspaceKey == "" {
		return roomWorkspaceRequiredNotice(surface), nil, true
	}
	targetBackend := s.surfaceBackend(surface)
	continuation := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, targetBackend, false)
	resolution := s.resolveWorkspaceContract(surface, workspaceKey, targetBackend)
	events := s.executeResolvedWorkspaceContinuation(surface, continuation, resolution, attachWorkspaceOptions{SuppressAutoUsePrompt: true})
	return events, s.root.Instances[surface.AttachedInstanceID], true
}

func (s *Service) handleReactionCreated(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	if surface == nil {
		return nil
	}
	if isApplauseReaction(action.ReactionType) {
		return s.handleApplauseReaction(surface, action)
	}
	if !isThumbsUpReaction(action.ReactionType) {
		return nil
	}
	targetMessageID := strings.TrimSpace(action.TargetMessageID)
	if targetMessageID == "" {
		return nil
	}
	inst, activeThreadID, activeTurnID, ok := s.activeSteerTarget(surface)
	if !ok {
		return nil
	}
	for _, candidate := range s.steerCandidates(surface, activeThreadID) {
		if !queueItemHasSourceMessage(candidate.Item, targetMessageID) {
			continue
		}
		if state.EffectiveInstanceBackend(inst) == agentproto.BackendOpenCode {
			return notice(surface, "opencode_steer_not_supported", opencodeSteerUnsupportedNoticeText)
		}
		return s.dispatchSteerCandidates(surface, inst, activeThreadID, activeTurnID, []steerCandidate{candidate}, targetMessageID, "")
	}
	return nil
}

func (s *Service) handleApplauseReaction(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	targetMessageID := strings.TrimSpace(action.TargetMessageID)
	if targetMessageID == "" {
		return nil
	}
	inst, activeThreadID, activeTurnID, ok := s.activeSteerTarget(surface)
	if !ok || surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil {
		return nil
	}
	if state.EffectiveInstanceBackend(inst) != agentproto.BackendCodex || !inst.CapabilitiesDeclared || !state.EffectiveInstanceCapabilities(inst).ThreadShellCommand {
		return nil
	}
	for queueIndex, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued || strings.TrimSpace(item.SourceMessageID) != targetMessageID {
			continue
		}
		if queuedItemExecutionThreadID(item) != activeThreadID {
			return nil
		}
		payload, ok, err := queuedItemShellPayload(item)
		if err != nil || !ok {
			return nil
		}
		item.Status = state.QueueItemShellCommanding
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		s.bindPendingShell(item.ID, &pendingShellBinding{
			InstanceID:       inst.InstanceID,
			SurfaceSessionID: surface.SurfaceSessionID,
			QueueItemID:      item.ID,
			SourceMessageID:  targetMessageID,
			ThreadID:         activeThreadID,
			TurnID:           activeTurnID,
			QueueIndex:       queueIndex,
		})
		events := s.pendingInputEvents(surface, control.PendingInputState{
			QueueItemID: item.ID,
			Status:      string(item.Status),
		}, []string{targetMessageID})
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandThreadShellCommand,
				Origin: agentproto.Origin{
					Surface:   surface.SurfaceSessionID,
					UserID:    surface.ActorUserID,
					ChatID:    surface.ChatID,
					MessageID: targetMessageID,
				},
				Target:       agentproto.Target{ThreadID: activeThreadID, TurnID: activeTurnID},
				ShellCommand: agentproto.ShellCommand{Payload: payload},
			},
		})
		return events
	}
	return nil
}

func (s *Service) handleSteerAllCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	ownerCardMessageID := ""
	if commandCardOwnsInlineResult(action) {
		ownerCardMessageID = strings.TrimSpace(action.MessageID)
	}
	inst, activeThreadID, activeTurnID, ok := s.activeSteerTarget(surface)
	if !ok {
		if ownerCardMessageID != "" {
			return []eventcontract.Event{steerAllNoopOwnerCardEvent(surface.SurfaceSessionID, ownerCardMessageID)}
		}
		return notice(surface, "steer_all_noop", "当前没有可并入本轮执行的排队消息。")
	}
	candidates := s.steerCandidates(surface, activeThreadID)
	if len(candidates) == 0 {
		if ownerCardMessageID != "" {
			return []eventcontract.Event{steerAllNoopOwnerCardEvent(surface.SurfaceSessionID, ownerCardMessageID)}
		}
		return notice(surface, "steer_all_noop", "当前没有可并入本轮执行的排队消息。")
	}
	var events []eventcontract.Event
	if ownerCardMessageID != "" {
		events = []eventcontract.Event{steerAllRequestedOwnerCardEvent(surface.SurfaceSessionID, ownerCardMessageID, len(candidates))}
	} else {
		events = notice(surface, "steer_all_requested", fmt.Sprintf("正在把 %d 条排队输入并入当前执行。", len(candidates)))
	}
	return append(events, s.dispatchSteerCandidates(surface, inst, activeThreadID, activeTurnID, candidates, "", ownerCardMessageID)...)
}

type steerCandidate struct {
	QueueIndex int
	Item       *state.QueueItemRecord
}

func (s *Service) activeSteerTarget(surface *state.SurfaceConsoleRecord) (*state.InstanceRecord, string, string, bool) {
	if surface == nil {
		return nil, "", "", false
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return nil, "", "", false
	}
	threadID := strings.TrimSpace(inst.ActiveThreadID)
	turnID := strings.TrimSpace(inst.ActiveTurnID)
	if threadID == "" || turnID == "" {
		return nil, "", "", false
	}
	if s.progress.isCompactTurn(inst.InstanceID, threadID, turnID) {
		return nil, "", "", false
	}
	return inst, threadID, turnID, true
}

func (s *Service) steerCandidates(surface *state.SurfaceConsoleRecord, activeThreadID string) []steerCandidate {
	if surface == nil || strings.TrimSpace(activeThreadID) == "" {
		return nil
	}
	candidates := make([]steerCandidate, 0, len(surface.QueuedQueueItemIDs))
	for index, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued {
			continue
		}
		if queuedItemExecutionThreadID(item) == "" || queuedItemExecutionThreadID(item) != activeThreadID {
			continue
		}
		candidates = append(candidates, steerCandidate{
			QueueIndex: index,
			Item:       item,
		})
	}
	return candidates
}

func (s *Service) dispatchSteerCandidates(
	surface *state.SurfaceConsoleRecord,
	inst *state.InstanceRecord,
	activeThreadID string,
	activeTurnID string,
	candidates []steerCandidate,
	explicitSourceMessageID string,
	ownerCardMessageID string,
) []eventcontract.Event {
	if surface == nil || inst == nil || strings.TrimSpace(activeThreadID) == "" || strings.TrimSpace(activeTurnID) == "" || len(candidates) == 0 {
		return nil
	}
	queueItemIDs := make([]string, 0, len(candidates))
	queueIndices := make(map[string]int, len(candidates))
	inputs := make([]agentproto.Input, 0, len(candidates))
	sourceMessageID := strings.TrimSpace(explicitSourceMessageID)
	for _, candidate := range candidates {
		item := candidate.Item
		if item == nil || strings.TrimSpace(item.ID) == "" || item.Status != state.QueueItemQueued {
			continue
		}
		item.Status = state.QueueItemSteering
		surface.QueuedQueueItemIDs = removeString(surface.QueuedQueueItemIDs, item.ID)
		queueItemIDs = append(queueItemIDs, item.ID)
		queueIndices[item.ID] = candidate.QueueIndex
		inputs = append(inputs, queueItemSteerInputs(item)...)
		if sourceMessageID == "" {
			sourceMessageID = strings.TrimSpace(item.SourceMessageID)
		}
	}
	if len(queueItemIDs) == 0 || len(inputs) == 0 {
		return nil
	}
	primaryQueueItemID := queueItemIDs[0]
	s.bindPendingSteer(primaryQueueItemID, &pendingSteerBinding{
		InstanceID:         inst.InstanceID,
		SurfaceSessionID:   surface.SurfaceSessionID,
		QueueItemID:        primaryQueueItemID,
		QueueItemIDs:       queueItemIDs,
		QueueIndices:       queueIndices,
		SourceMessageID:    sourceMessageID,
		OwnerCardMessageID: strings.TrimSpace(ownerCardMessageID),
		ThreadID:           activeThreadID,
		TurnID:             activeTurnID,
		QueueIndex:         queueIndices[primaryQueueItemID],
	})
	return []eventcontract.Event{{
		Kind:             eventcontract.KindAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandTurnSteer,
			Origin: agentproto.Origin{
				Surface:   surface.SurfaceSessionID,
				UserID:    surface.ActorUserID,
				ChatID:    surface.ChatID,
				MessageID: sourceMessageID,
			},
			Target: agentproto.Target{
				ThreadID: activeThreadID,
				TurnID:   activeTurnID,
			},
			Prompt: agentproto.Prompt{
				Inputs: inputs,
			},
		},
	}}
}

func (s *Service) handleMessageRecalled(surface *state.SurfaceConsoleRecord, targetMessageID string) []eventcontract.Event {
	targetMessageID = strings.TrimSpace(targetMessageID)
	if surface == nil || targetMessageID == "" {
		return nil
	}
	if activeID := surface.ActiveQueueItemID; activeID != "" {
		if item := surface.QueueItems[activeID]; item != nil && queueItemHasSourceMessage(item, targetMessageID) {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return []eventcontract.Event{{
					Kind:             eventcontract.KindNotice,
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
		s.markFilesForMessages(surface, queueItemSourceMessageIDs(item), state.FileDiscarded)
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
	for _, file := range surface.StagedFiles {
		if file.SourceMessageID == targetMessageID && file.State == state.FileStaged {
			file.State = state.FileCancelled
			return s.pendingInputEvents(surface, control.PendingInputState{
				QueueItemID: file.FileID,
				Status:      string(file.State),
				QueueOff:    true,
				ThumbsDown:  true,
			}, []string{file.SourceMessageID})
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

func isApplauseReaction(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	return normalized == "applause"
}

func (s *Service) stopSurface(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	var events []eventcontract.Event
	discarded := countPendingDrafts(surface)
	inst := s.root.Instances[surface.AttachedInstanceID]
	notice := control.Notice{
		Code:     "stop_no_active_turn",
		Title:    "没有正在运行的推理",
		Text:     "当前没有正在运行的推理。",
		ThemeKey: "system",
	}
	if surface.ReviewSession != nil && surface.ReviewSession.AwaitingFollowUpText && !reviewSessionTurnActive(surface, surface.ReviewSession) {
		surface.ReviewSession.AwaitingFollowUpText = false
		surface.ReviewSession.LastUpdatedAt = s.now()
		notice = control.Notice{
			Code:     "review_follow_up_cancelled",
			Title:    "已取消追问输入",
			Text:     "已取消本次审阅追问输入；当前审阅结果仍可继续处理。",
			ThemeKey: "system",
		}
	} else if inst != nil && !inst.Online && surface.ActiveQueueItemID != "" {
		notice = s.stopOfflineNotice(surface)
	} else if threadID, turnID, ok := s.interruptibleSurfaceTurn(surface); ok {
		if strings.TrimSpace(surface.AttachedInstanceID) != "" {
			s.markRemoteTurnInterruptRequested(surface.AttachedInstanceID, threadID, turnID)
		}
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindAgentCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			Command: &agentproto.Command{
				Kind: agentproto.CommandTurnInterrupt,
				Origin: agentproto.Origin{
					Surface: surface.SurfaceSessionID,
					UserID:  surface.ActorUserID,
					ChatID:  surface.ChatID,
				},
				Target: agentproto.Target{
					ThreadID: threadID,
					TurnID:   turnID,
				},
			},
		})
		notice = control.Notice{
			Code:     "stop_requested",
			Title:    "已发送停止请求",
			Text:     "已向当前运行中的 turn 发送停止请求。",
			ThemeKey: "system",
		}
	} else if episode := activeAutoContinueEpisode(surface); episode != nil && episode.State == state.AutoContinueEpisodeScheduled {
		events = append(events, s.cancelAutoContinueEpisode(surface)...)
		notice = control.Notice{
			Code:     "autocontinue_stopped",
			Title:    "已停止自动继续",
			Text:     "已停止等待中的自动继续。",
			ThemeKey: "system",
		}
	} else if surface.ActiveQueueItemID != "" {
		if s.progress.surfaceHasPendingCompact(surface) {
			notice = control.Notice{
				Code:     "stop_not_interruptible",
				Title:    "当前还不能停止",
				Text:     "当前上下文压缩请求正在派发，尚未进入可中断状态。",
				ThemeKey: "system",
			}
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
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice:           &notice,
	})
	return events
}

func (s *Service) cancelPendingHeadlessLaunch(surface *state.SurfaceConsoleRecord, notice *control.Notice) []eventcontract.Event {
	pending := s.pendingSurfaceHeadlessLaunch(surface, "")
	if pending == nil {
		return nil
	}
	s.releaseFeishuRoomActiveReservationByReason(surface, feishuRoomGroupOnDemandReservationReason)
	events := s.discardDrafts(surface)
	events = append(events, s.finalizeDetachedSurface(surface)...)
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: &control.DaemonCommand{
			Kind:             control.DaemonCommandKillHeadless,
			SurfaceSessionID: surface.SurfaceSessionID,
			InstanceID:       pending.InstanceID,
			ThreadID:         pending.ThreadID,
			ThreadTitle:      pending.ThreadTitle,
			WorkspaceKey:     pending.WorkspaceKey,
			ThreadCWD:        pending.ThreadCWD,
		},
	})
	if notice != nil {
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           notice,
		})
	}
	fallback := ""
	if notice != nil {
		fallback = notice.Text
	}
	return s.maybeFinalizePendingTargetPicker(surface, events, fallback)
}

func (s *Service) detach(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	if surface != nil {
		s.revokeGoalInterlockForSurface(surface.SurfaceSessionID)
	}
	if s.pendingSurfaceHeadlessLaunch(surface, "") != nil {
		return s.cancelPendingHeadlessLaunch(surface, &control.Notice{
			Code:  "detached",
			Title: "已取消恢复流程",
			Text:  fmt.Sprintf("已取消当前恢复流程。%s", s.detachedNoneText(surface)),
		})
	}
	if surface.AttachedInstanceID == "" {
		return notice(surface, "detached", s.detachedNoneText(surface))
	}
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
	events := s.discardDrafts(surface)
	clearSurfaceRequests(surface)
	s.consumeSurfacePendingHeadlessLaunch(surface, "")
	clearSurfacePromptRuntimeOverride(surface)
	s.restoreSurfaceDispatchNormal(surface)
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceHasPreStartRemoteDispatch(surface) {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			events = append(events, s.abortSurfaceActiveQueueItemForDetach(surface, item)...)
		}
		events = append(events, s.finalizeDetachedSurface(surface)...)
		return append(events, notice(surface, "detached", s.detachedText(surface))...)
	}
	if s.surfaceNeedsDelayedDetach(surface, inst) {
		s.setSurfaceDetachAbandoning(surface, s.now().Add(s.config.DetachAbandonWait))
		if binding := s.remoteBindingForSurface(surface); binding != nil && binding.TurnID != "" {
			events = append(events, eventcontract.Event{
				Kind:             eventcontract.KindAgentCommand,
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

func (s *Service) abortSurfaceActiveQueueItemForDetach(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord) []eventcontract.Event {
	if surface == nil || item == nil {
		return nil
	}
	item.Status = state.QueueItemFailed
	s.clearSurfaceActiveQueueItem(surface, item.ID)
	if binding := s.remoteBindingForSurface(surface); binding != nil {
		s.clearTurnArtifacts(binding.InstanceID, binding.ThreadID, binding.TurnID)
	}
	s.clearRemoteOwnership(surface)
	return s.pendingInputEvents(surface, control.PendingInputState{
		QueueItemID: item.ID,
		Status:      string(item.Status),
		TypingOff:   true,
	}, queueItemSourceMessageIDs(item))
}
