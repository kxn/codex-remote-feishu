package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) useThread(surface *state.SurfaceConsoleRecord, threadID string, allowCrossWorkspace bool) []eventcontract.Event {
	threadID = strings.TrimSpace(threadID)
	target := s.resolveThreadTargetWithScope(surface, threadID, allowCrossWorkspace)
	return s.executeResolvedThreadTarget(surface, threadID, target)
}

func (s *Service) executeResolvedThreadTarget(surface *state.SurfaceConsoleRecord, threadID string, target resolvedThreadTarget) []eventcontract.Event {
	switch target.Mode {
	case threadAttachCurrentVisible:
		return s.useAttachedVisibleThread(surface, threadID)
	case threadAttachFreeVisible, threadAttachReuseHeadless:
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
		return s.attachSurfaceToKnownThread(surface, target.Instance, target.View, attachSurfaceToKnownThreadDefault)
	case threadAttachCreateHeadless:
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
		return s.startHeadlessForResolvedThread(surface, target.View)
	default:
		code := firstNonEmpty(target.NoticeCode, "thread_not_found")
		text := firstNonEmpty(target.NoticeText, "目标会话不存在或当前不可见。")
		return notice(surface, code, text)
	}
}

func (s *Service) useAttachedVisibleThread(surface *state.SurfaceConsoleRecord, threadID string) []eventcontract.Event {
	return s.useAttachedVisibleThreadMode(surface, threadID, s.surfaceThreadPickRouteMode(surface))
}

func (s *Service) useAttachedVisibleThreadMode(surface *state.SurfaceConsoleRecord, threadID string, routeMode state.RouteMode) []eventcontract.Event {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if (surface.RouteMode != routeMode || surface.SelectedThreadID != threadID) && s.surfaceHasRouteMutationRequestState(surface) {
		if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
			return blocked
		}
	}
	events := []eventcontract.Event{}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadRouteExit(surface); blocked != nil {
			return blocked
		}
		events = append(events, s.discardDrafts(surface)...)
	} else if blocked := s.blockThreadSwitch(surface); blocked != nil {
		return blocked
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return append(events, notice(surface, "thread_not_found", "目标会话不存在或当前不可见。")...)
	}
	if !threadBelongsToInstanceWorkspace(inst, thread) {
		fallback := s.resolveThreadTargetFromView(surface, s.mergedThreadView(surface, threadID))
		if fallback.Mode == threadAttachCurrentVisible {
			return append(events, notice(surface, "thread_not_found", "目标会话不存在或当前不可见。")...)
		}
		return append(events, s.executeResolvedThreadTarget(surface, threadID, fallback)...)
	}
	if owner := s.threadClaimSurface(threadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		switch s.threadKickStatus(inst, owner, threadID) {
		case threadKickIdle:
			return append(events, s.presentKickThreadPrompt(surface, inst, threadID, owner)...)
		case threadKickQueued:
			return append(events, notice(surface, "thread_busy_queued", "目标会话当前还有排队任务，暂时不能强踢。请等待对方队列清空，或切换到其他会话。")...)
		case threadKickRunning:
			return append(events, notice(surface, "thread_busy_running", "目标会话当前正在执行，暂时不能强踢。请等待执行完成，或切换到其他会话。")...)
		default:
			return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
		}
	}
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	if prevThreadID != threadID || prevRouteMode != routeMode {
		clearAutoContinueRuntime(surface)
	}
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimThread(surface, inst, threadID) {
		surface.RouteMode = state.RouteModeUnbound
		s.clearPreparedNewThread(surface)
		return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
	}
	events = append(events, s.maybeSealPlanProposalForRouteChange(surface, "当前工作目标已切换到其他会话，之前的提案计划已失效。")...)
	events = append(events, s.discardStagedInputsForRouteChange(surface, prevThreadID, prevRouteMode, threadID, routeMode)...)
	surface.SelectedThreadID = threadID
	s.clearPreparedNewThread(surface)
	surface.RouteMode = routeMode
	title := threadID
	thread = s.ensureThread(inst, threadID)
	s.touchThread(thread)
	title = displayThreadTitle(inst, thread, threadID)
	events = append(events, s.threadSelectionEvents(surface, threadID, string(surface.RouteMode), title)...)
	events = append(events, s.replayThreadUpdate(surface, inst, threadID)...)
	if len(events) != 0 {
		return events
	}
	return notice(surface, "selection_unchanged", fmt.Sprintf("当前输入目标保持为：%s", title))
}

func (s *Service) attachSurfaceToKnownThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, view *mergedThreadView, mode attachSurfaceToKnownThreadMode) []eventcontract.Event {
	if surface == nil || inst == nil || view == nil || strings.TrimSpace(view.ThreadID) == "" {
		return nil
	}
	instanceBackend := state.EffectiveInstanceBackend(inst)
	viewBackend := agentproto.NormalizeBackend(view.Backend)
	if viewBackend != "" && instanceBackend != viewBackend {
		switch mode {
		case attachSurfaceToKnownThreadHeadlessRestore:
			return []eventcontract.Event{{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           headlessRestoreFailureNotice("thread_not_found"),
			}}
		case attachSurfaceToKnownThreadSurfaceResume:
			return []eventcontract.Event{{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           surfaceResumeFailureNotice("thread_not_found"),
			}}
		default:
			return notice(surface, "thread_backend_mismatch", "目标会话当前不可恢复，请重新选择可用会话。")
		}
	}
	workspaceKey := mergedThreadWorkspaceClaimKey(view)
	if s.surfaceUsesWorkspaceClaims(surface) && workspaceKey == "" {
		return notice(surface, "workspace_key_missing", "当前无法确定目标会话所属的 workspace，暂时不能在 headless 模式接管。请切到 `/mode vscode` 后再试。")
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return attachSurfaceToKnownThreadWorkspaceBusyNotice(surface, mode)
	}
	if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return attachSurfaceToKnownThreadInstanceBusyNotice(surface, inst, mode)
	}
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	events := []eventcontract.Event{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearAutoContinueRuntime(surface)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
		surface.PendingHeadless = nil
		surface.ActiveQueueItemID = ""
		surface.DispatchMode = state.DispatchModeNormal
		surface.Abandoning = false
		delete(s.pausedUntil, surface.SurfaceSessionID)
		delete(s.abandoningUntil, surface.SurfaceSessionID)
	}
	if !s.claimWorkspace(surface, workspaceKey) {
		if s.surfaceUsesWorkspaceClaims(surface) {
			return append(events, attachSurfaceToKnownThreadWorkspaceBusyNotice(surface, mode)...)
		}
		return append(events, notice(surface, "workspace_key_missing", "当前无法确定目标会话所属的 workspace，暂时不能在 headless 模式接管。请切到 `/mode vscode` 后再试。")...)
	}

	if !s.claimInstance(surface, inst.InstanceID) {
		s.releaseSurfaceWorkspaceClaim(surface)
		return append(events, attachSurfaceToKnownThreadInstanceBusyNotice(surface, inst, mode)...)
	}
	s.surfaceCurrentWorkspaceKey(surface)
	surface.Backend = instanceBackend
	surface.AttachedInstanceID = inst.InstanceID
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	clearSurfaceRequests(surface)
	s.clearPreparedNewThread(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)

	if isHeadlessInstance(inst) && strings.TrimSpace(threadCWD(view)) != "" {
		s.retargetManagedHeadlessInstance(inst, threadCWD(view))
	}

	thread := s.ensureThread(inst, view.ThreadID)
	if view.Thread != nil {
		if strings.TrimSpace(view.Thread.Name) != "" {
			thread.Name = strings.TrimSpace(view.Thread.Name)
		}
		if strings.TrimSpace(view.Thread.Preview) != "" {
			thread.Preview = strings.TrimSpace(view.Thread.Preview)
		}
		if strings.TrimSpace(view.Thread.CWD) != "" {
			thread.CWD = strings.TrimSpace(view.Thread.CWD)
		}
		if view.Thread.RuntimeStatus != nil {
			thread.RuntimeStatus = agentproto.CloneThreadRuntimeStatus(view.Thread.RuntimeStatus)
		} else if strings.TrimSpace(view.Thread.State) != "" {
			thread.State = strings.TrimSpace(view.Thread.State)
		}
		if strings.TrimSpace(view.Thread.ExplicitModel) != "" {
			thread.ExplicitModel = strings.TrimSpace(view.Thread.ExplicitModel)
		}
		if strings.TrimSpace(view.Thread.ExplicitReasoningEffort) != "" {
			thread.ExplicitReasoningEffort = strings.TrimSpace(view.Thread.ExplicitReasoningEffort)
		}
		thread.Loaded = thread.Loaded || view.Thread.Loaded
		thread.Archived = view.Thread.Archived
		thread.LastUsedAt = view.Thread.LastUsedAt
		thread.ListOrder = view.Thread.ListOrder
	}
	if mode == attachSurfaceToKnownThreadHeadlessRestore || mode == attachSurfaceToKnownThreadSurfaceResume {
		s.clearThreadReplay(inst, view.ThreadID)
	} else {
		s.adoptThreadReplay(inst, view.ThreadID)
	}
	s.touchThread(thread)
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimKnownThread(surface, inst, view.ThreadID) {
		events = append(events, s.finalizeDetachedSurface(surface)...)
		return append(events, attachSurfaceToKnownThreadThreadBusyNotice(surface, mode)...)
	}
	surface.SelectedThreadID = view.ThreadID
	surface.RouteMode = state.RouteModePinned

	title := displayThreadTitle(inst, thread, view.ThreadID)
	preview := threadPreview(thread)
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		surface.LastSelection = &state.SelectionAnnouncementRecord{
			ThreadID:  view.ThreadID,
			RouteMode: string(surface.RouteMode),
			Title:     title,
			Preview:   preview,
		}
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_restore_attached",
				Title: "会话已恢复",
				Text:  fmt.Sprintf("重连成功，已恢复到之前会话：%s", title),
			},
		})
	} else if mode == attachSurfaceToKnownThreadSurfaceResume {
		s.clearThreadReplay(inst, view.ThreadID)
		surface.LastSelection = &state.SelectionAnnouncementRecord{
			ThreadID:  view.ThreadID,
			RouteMode: string(surface.RouteMode),
			Title:     title,
			Preview:   preview,
		}
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "surface_resume_attached",
				Title: "会话已恢复",
				Text:  fmt.Sprintf("已恢复到之前会话：%s", title),
			},
		})
	} else {
		attachLead := s.attachedLeadText(surface, inst)
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code: "attached",
				Text: fmt.Sprintf("%s 当前输入目标：%s", attachLead, title),
			},
		})
		events = append(events, s.threadSelectionEvents(surface, view.ThreadID, string(surface.RouteMode), title)...)
		events = append(events, s.replayThreadUpdate(surface, inst, view.ThreadID)...)
	}
	events = append(events, s.maybeRequestThreadRefresh(surface, inst, view.ThreadID)...)
	return events
}

func (s *Service) startHeadlessForResolvedThread(surface *state.SurfaceConsoleRecord, view *mergedThreadView) []eventcontract.Event {
	return s.startHeadlessForResolvedThreadWithMode(surface, view, startHeadlessModeDefault)
}

func attachSurfaceToKnownThreadInstanceBusyNotice(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, mode attachSurfaceToKnownThreadMode) []eventcontract.Event {
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_busy"),
		}}
	}
	if mode == attachSurfaceToKnownThreadSurfaceResume {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           surfaceResumeFailureNotice("workspace_instance_busy"),
		}}
	}
	if surface != nil && state.IsHeadlessProductMode(state.NormalizeProductMode(surface.ProductMode)) {
		return notice(surface, "workspace_instance_busy", "目标工作区当前暂时不可接管，请稍后重试。")
	}
	return notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))
}

func attachSurfaceToKnownThreadThreadBusyNotice(surface *state.SurfaceConsoleRecord, mode attachSurfaceToKnownThreadMode) []eventcontract.Event {
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_busy"),
		}}
	}
	if mode == attachSurfaceToKnownThreadSurfaceResume {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           surfaceResumeFailureNotice("thread_busy"),
		}}
	}
	return notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")
}

func attachSurfaceToKnownThreadWorkspaceBusyNotice(surface *state.SurfaceConsoleRecord, mode attachSurfaceToKnownThreadMode) []eventcontract.Event {
	if mode == attachSurfaceToKnownThreadHeadlessRestore {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("workspace_busy"),
		}}
	}
	if mode == attachSurfaceToKnownThreadSurfaceResume {
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           surfaceResumeFailureNotice("workspace_busy"),
		}}
	}
	return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管。")
}

func (s *Service) startHeadlessForResolvedThreadWithMode(surface *state.SurfaceConsoleRecord, view *mergedThreadView, mode startHeadlessMode) []eventcontract.Event {
	if surface == nil || view == nil {
		return nil
	}
	cwd := strings.TrimSpace(threadCWD(view))
	if cwd == "" {
		if mode == startHeadlessModeHeadlessRestore {
			return []eventcontract.Event{{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           headlessRestoreFailureNotice("thread_cwd_missing"),
			}}
		}
		return notice(surface, "thread_cwd_missing", "目标会话缺少可恢复的工作目录，当前无法在后台恢复该会话。")
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, cwd); owner != nil {
		if mode == startHeadlessModeHeadlessRestore {
			return []eventcontract.Event{{
				Kind:             eventcontract.KindNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice:           headlessRestoreFailureNotice("workspace_busy"),
			}}
		}
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管。")
	}
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)
	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	threadTitle := displayThreadTitle(view.Inst, view.Thread, view.ThreadID)
	threadPreview := ""
	threadName := ""
	sourceInstanceID := ""
	if view.Thread != nil {
		threadPreview = strings.TrimSpace(view.Thread.Preview)
		threadName = strings.TrimSpace(view.Thread.Name)
	}
	if view.Inst != nil {
		sourceInstanceID = view.Inst.InstanceID
	}

	events := []eventcontract.Event{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearAutoContinueRuntime(surface)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
	}
	if !s.claimWorkspace(surface, cwd) {
		if s.surfaceUsesWorkspaceClaims(surface) {
			if mode == startHeadlessModeHeadlessRestore {
				return append(events, eventcontract.Event{
					Kind:             eventcontract.KindNotice,
					SurfaceSessionID: surface.SurfaceSessionID,
					Notice:           headlessRestoreFailureNotice("workspace_busy"),
				})
			}
			return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管。")...)
		}
		return append(events, notice(surface, "workspace_key_missing", "当前无法确定目标会话所属的 workspace，暂时不能在 headless 模式恢复。请切到 `/mode vscode` 后再试。")...)
	}
	targetBackend := agentproto.NormalizeBackend(view.Backend)
	if targetBackend == "" {
		targetBackend = s.surfaceBackend(surface)
	}
	if view.Inst != nil {
		instanceBackend := state.EffectiveInstanceBackend(view.Inst)
		if targetBackend == "" {
			targetBackend = instanceBackend
		}
		if targetBackend != instanceBackend {
			if mode == startHeadlessModeHeadlessRestore {
				return append(events, eventcontract.Event{
					Kind:             eventcontract.KindNotice,
					SurfaceSessionID: surface.SurfaceSessionID,
					Notice:           headlessRestoreFailureNotice("thread_not_found"),
				})
			}
			return append(events, notice(surface, "thread_backend_mismatch", "目标会话当前不可恢复，请重新选择可用会话。")...)
		}
	}
	surface.Backend = agentproto.NormalizeBackend(targetBackend)
	s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	launchContract := s.headlessLaunchContract(surface)
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:            instanceID,
		ThreadID:              view.ThreadID,
		ThreadTitle:           threadTitle,
		ThreadName:            threadName,
		ThreadPreview:         threadPreview,
		ThreadCWD:             cwd,
		Backend:               launchContract.Backend,
		CodexProviderID:       launchContract.CodexProviderID,
		ClaudeProfileID:       launchContract.ClaudeProfileID,
		ClaudeReasoningEffort: launchContract.ClaudeReasoningEffort,
		RequestedAt:           s.now(),
		ExpiresAt:             s.now().Add(s.config.HeadlessLaunchWait),
		Status:                state.HeadlessLaunchStarting,
		Purpose:               state.HeadlessLaunchPurposeThreadRestore,
		SourceInstanceID:      sourceInstanceID,
		AutoRestore:           mode == startHeadlessModeHeadlessRestore,
	}
	if mode == startHeadlessModeDefault {
		events = append(events, eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_starting",
				Title: "准备恢复会话",
				Text:  fmt.Sprintf("正在后台准备恢复会话：%s", threadTitle),
			},
		})
	}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindDaemonCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		DaemonCommand: func() *control.DaemonCommand {
			command := &control.DaemonCommand{
				Kind:             control.DaemonCommandStartHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
				ThreadID:         view.ThreadID,
				ThreadTitle:      threadTitle,
				ThreadCWD:        cwd,
				AutoRestore:      mode == startHeadlessModeHeadlessRestore,
			}
			s.applyHeadlessLaunchContract(command, launchContract)
			return command
		}(),
	})
	return events
}
