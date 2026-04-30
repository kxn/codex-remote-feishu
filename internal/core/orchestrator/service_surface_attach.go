package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) attachWorkspace(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return s.attachWorkspaceWithOptions(surface, workspaceKey, attachWorkspaceOptions{})
}

func (s *Service) attachWorkspaceWithOptions(surface *state.SurfaceConsoleRecord, workspaceKey string, options attachWorkspaceOptions) []eventcontract.Event {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_not_found", "目标工作区不存在。请重新发送 /list。")
	}
	currentWorkspace := s.surfaceCurrentWorkspaceKey(surface)
	if surface.AttachedInstanceID != "" && currentWorkspace == workspaceKey {
		return notice(surface, "workspace_already_attached", fmt.Sprintf("当前已接管工作区：%s。", workspaceKey))
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	if surface.AttachedInstanceID != "" && currentWorkspace != "" && currentWorkspace != workspaceKey {
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
	}
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	inst := s.resolveWorkspaceAttachInstance(surface, workspaceKey)
	if inst == nil {
		if len(s.workspaceOnlineInstances(workspaceKey)) == 0 {
			return notice(surface, "workspace_not_found", "目标工作区已失效，请重新发送 /list。")
		}
		return notice(surface, "workspace_instance_busy", "目标工作区当前暂时不可接管，请稍后重试。")
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
		s.releaseSurfaceThreadClaim(surface)
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
		return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")...)
	}
	if !s.claimInstance(surface, inst.InstanceID) {
		s.releaseSurfaceWorkspaceClaim(surface)
		return append(events, notice(surface, "workspace_instance_busy", "目标工作区当前暂时不可接管，请稍后重试。")...)
	}

	surface.Backend = state.EffectiveInstanceBackend(inst)
	surface.AttachedInstanceID = inst.InstanceID
	s.surfaceCurrentWorkspaceKey(surface)
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	clearSurfaceRequests(surface)
	s.clearPreparedNewThread(surface)
	s.releaseSurfaceThreadClaim(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(state.RouteModeUnbound),
		Title:     "未选择会话",
		Preview:   "",
	}
	s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)
	if options.PrepareNewThread {
		return s.prepareNewThread(surface)
	}

	noticeCode := "workspace_attached"
	noticeText := fmt.Sprintf("已接管工作区 %s。请继续 /use 选择一个会话，或直接发送文本开启新会话（也可 /new 先进入待命）。", workspaceKey)
	if currentWorkspace != "" && currentWorkspace != workspaceKey {
		noticeCode = "workspace_switched"
		noticeText = fmt.Sprintf("已切换到工作区 %s。请继续 /use 选择一个会话，或直接发送文本开启新会话（也可 /new 先进入待命）。", workspaceKey)
	}
	visibleThreadCount := len(workspaceVisibleThreads(inst, workspaceKey))
	if options.ResumeNotice {
		noticeCode = "surface_resume_workspace_attached"
		if visibleThreadCount == 0 {
			noticeText = fmt.Sprintf("之前的会话暂未恢复，已先回到工作区 %s。当前还没有可见会话；你可以直接发送文本开启新会话（或 /new 先进入待命），也可稍后发送 /use。", workspaceKey)
		} else {
			noticeText = fmt.Sprintf("之前的会话当前不可见，已先回到工作区 %s。请继续 /use 选择要恢复的会话，或直接发送文本开启新会话（也可 /new 先进入待命）。", workspaceKey)
		}
	} else if visibleThreadCount == 0 {
		noticeText = fmt.Sprintf("已接管工作区 %s。当前还没有可见会话；你可以直接发送文本开启新会话（或 /new 先进入待命），也可稍后发送 /use。", workspaceKey)
	}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: noticeCode,
			Text: noticeText,
		},
	})
	events = append(events, s.autoPromptUseThread(surface, inst)...)
	return events
}

func (s *Service) attachInstance(surface *state.SurfaceConsoleRecord, instanceID string) []eventcontract.Event {
	return s.attachInstanceWithMode(surface, instanceID, attachInstanceModeDefault)
}

func (s *Service) attachInstanceWithMode(surface *state.SurfaceConsoleRecord, instanceID string, mode attachInstanceMode) []eventcontract.Event {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return notice(surface, "instance_not_found", "实例不存在。")
	}
	s.normalizeSurfaceProductMode(surface)
	surfaceBackend := s.surfaceBackend(surface)
	instanceBackend := state.EffectiveInstanceBackend(inst)
	workspaceKey := instanceWorkspaceClaimKey(inst)
	switchingInstance := surface.AttachedInstanceID != "" && surface.AttachedInstanceID != instanceID
	if s.surfaceIsVSCode(surface) && (instanceBackend != agentproto.BackendCodex || !isVSCodeInstance(inst)) {
		return notice(surface, "mode_backend_mismatch", "当前处于 vscode 模式，只能接管 Codex VS Code 实例。请先选择 VS Code 实例，或切回 `/mode codex` / `/mode claude`。")
	}
	if s.surfaceIsHeadless(surface) && instanceBackend != surfaceBackend {
		return notice(surface, "mode_backend_mismatch", fmt.Sprintf("当前处于 %s 模式，不能直接接管 %s backend。请先 `/mode %s`。", s.surfaceModeAlias(surface), instanceBackend, instanceBackend))
	}
	if switchingInstance && !s.surfaceIsVSCode(surface) {
		return notice(surface, "attach_requires_detach", "当前会话已接管其他工作区，请先 /detach。")
	}
	if switchingInstance {
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
	}
	if surface.AttachedInstanceID == instanceID {
		if !s.surfaceIsVSCode(surface) && workspaceKey != "" {
			return notice(surface, "already_attached", fmt.Sprintf("当前已接管工作区：%s。", workspaceKey))
		}
		return notice(surface, "already_attached", fmt.Sprintf("当前已接管 %s。", inst.DisplayName))
	}
	if s.surfaceUsesWorkspaceClaims(surface) && workspaceKey == "" {
		return notice(surface, "workspace_key_missing", "当前无法确定目标对应的工作区，暂时不能在 headless 模式接管。请切到 `/mode vscode` 后再试。")
	}
	if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
		return notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")
	}
	if owner := s.instanceClaimSurface(instanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))
	}
	s.persistCurrentClaudeWorkspaceProfileSnapshot(surface)

	events := s.discardDrafts(surface)
	if surface.AttachedInstanceID != "" {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		clearAutoContinueRuntime(surface)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.releaseSurfaceThreadClaim(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
	}
	if !s.claimWorkspace(surface, workspaceKey) {
		if s.surfaceUsesWorkspaceClaims(surface) {
			return append(events, notice(surface, "workspace_busy", "目标 workspace 当前已被其他飞书会话接管，请等待对方 /detach。")...)
		}
		return append(events, notice(surface, "workspace_key_missing", "当前无法确定目标对应的工作区，暂时不能在 headless 模式接管。请切到 `/mode vscode` 后再试。")...)
	}
	if !s.claimInstance(surface, instanceID) {
		s.releaseSurfaceWorkspaceClaim(surface)
		return append(events, notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))...)
	}
	s.surfaceCurrentWorkspaceKey(surface)
	surface.Backend = instanceBackend
	surface.AttachedInstanceID = instanceID
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	s.restoreCurrentClaudeWorkspaceProfileSnapshot(surface)

	if s.surfaceIsVSCode(surface) {
		return append(events, s.attachVSCodeInstance(surface, inst, switchingInstance, mode)...)
	}

	initialThreadID := s.defaultAttachThread(inst)
	if initialThreadID != "" && s.claimThread(surface, inst, initialThreadID) {
		surface.SelectedThreadID = initialThreadID
		surface.RouteMode = state.RouteModePinned
	} else {
		surface.SelectedThreadID = ""
		surface.RouteMode = state.RouteModeUnbound
	}
	lastTitle := ""
	lastPreview := ""
	if surface.SelectedThreadID != "" {
		lastTitle = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
		lastPreview = threadPreview(inst.Threads[surface.SelectedThreadID])
	}
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  surface.SelectedThreadID,
		RouteMode: string(surface.RouteMode),
		Title:     lastTitle,
		Preview:   lastPreview,
	}

	title := "未选择会话"
	text := s.attachedLeadText(surface, inst)
	if surface.SelectedThreadID != "" {
		title = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
		text = fmt.Sprintf("%s 当前输入目标：%s", text, title)
	} else if initialThreadID != "" {
		text = fmt.Sprintf("%s 默认会话当前已被其他飞书会话占用，请先通过 /use 选择可用会话。", text)
	} else if len(visibleThreads(inst)) != 0 {
		text = fmt.Sprintf("%s 当前还没有选择会话，请先通过 /use 选择一个会话。", text)
	} else {
		if s.surfaceIsVSCode(surface) {
			text = fmt.Sprintf("%s 当前没有可用会话，请等待 VS Code 切到会话后再 /use，或直接 /detach。", text)
		} else {
			text = fmt.Sprintf("%s 当前工作区还没有可用会话；你可以稍后再 /use，或直接发送文本开启新会话（也可 /new 先进入待命）。", text)
		}
	}
	events = append(events, eventcontract.Event{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "attached",
			Text: text,
		},
	})
	if surface.SelectedThreadID != "" {
		events = append(events, s.replayThreadUpdate(surface, inst, surface.SelectedThreadID)...)
	}
	events = append(events, s.maybeRequestThreadRefresh(surface, inst, surface.SelectedThreadID)...)
	if surface.SelectedThreadID == "" {
		events = append(events, s.autoPromptUseThread(surface, inst)...)
	}
	return events
}

func (s *Service) attachVSCodeInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, switched bool, mode attachInstanceMode) []eventcontract.Event {
	if surface == nil || inst == nil {
		return nil
	}
	surface.SelectedThreadID = ""
	s.clearPreparedNewThread(surface)
	surface.RouteMode = state.RouteModeFollowLocal

	events := s.reevaluateFollowSurface(surface)
	if len(events) == 0 && surface.SelectedThreadID == "" {
		events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）")...)
	}

	verb := "已接管"
	if switched {
		verb = "已切换到"
	}
	noticeCode := "attached"
	text := fmt.Sprintf("%s %s。", verb, inst.DisplayName)
	if mode == attachInstanceModeSurfaceResume {
		noticeCode = "surface_resume_instance_attached"
		text = fmt.Sprintf("已恢复到 VS Code 实例 %s。", inst.DisplayName)
	}
	if surface.SelectedThreadID != "" {
		thread := s.ensureThread(inst, surface.SelectedThreadID)
		text = fmt.Sprintf("%s 当前跟随会话：%s", text, displayThreadTitle(inst, thread, surface.SelectedThreadID))
	} else if len(visibleThreads(inst)) != 0 {
		if mode == attachInstanceModeSurfaceResume {
			text = fmt.Sprintf("%s 当前还没有新的 VS Code 焦点；请先在 VS Code 里再说一句话，或发送 /use 选择当前实例已知会话。", text)
		} else {
			text = fmt.Sprintf("%s 已进入跟随模式；当前还没有可接管的 VS Code 焦点。请先在 VS Code 里实际操作一次会话，或发送 /use 选择当前实例已知会话。", text)
		}
	} else {
		if mode == attachInstanceModeSurfaceResume {
			text = fmt.Sprintf("%s 当前还没有观测到新的 VS Code 活动；请先在 VS Code 里再说一句话，或稍后重试。", text)
		} else {
			text = fmt.Sprintf("%s 已进入跟随模式；当前还没有观测到会话。请先在 VS Code 里实际操作一次会话，或稍后重试。", text)
		}
	}

	result := []eventcontract.Event{{
		Kind:             eventcontract.KindNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: noticeCode,
			Text: text,
		},
	}}
	result = append(result, events...)
	if surface.SelectedThreadID != "" {
		result = append(result, s.replayThreadUpdate(surface, inst, surface.SelectedThreadID)...)
	}
	result = append(result, s.maybeRequestThreadRefresh(surface, inst, surface.SelectedThreadID)...)
	return result
}

func (s *Service) attachHeadlessInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, pending *state.HeadlessLaunchRecord) []eventcontract.Event {
	if surface == nil || inst == nil || pending == nil {
		return nil
	}
	if pending.Purpose == state.HeadlessLaunchPurposeFreshWorkspace {
		surface.PendingHeadless = nil
		if pending.PrepareNewThread {
			return s.attachWorkspaceWithOptions(surface, pending.ThreadCWD, attachWorkspaceOptions{PrepareNewThread: true})
		}
		return s.attachWorkspace(surface, pending.ThreadCWD)
	}
	if strings.TrimSpace(pending.ThreadID) != "" {
		view := s.mergedThreadView(surface, pending.ThreadID)
		if view == nil {
			thread := s.ensureThread(inst, pending.ThreadID)
			if strings.TrimSpace(thread.Name) == "" {
				thread.Name = strings.TrimSpace(pending.ThreadName)
			}
			if strings.TrimSpace(thread.Preview) == "" {
				thread.Preview = strings.TrimSpace(pending.ThreadPreview)
			}
			if strings.TrimSpace(thread.CWD) == "" {
				thread.CWD = strings.TrimSpace(pending.ThreadCWD)
			}
			view = &mergedThreadView{
				ThreadID: pending.ThreadID,
				Backend:  state.EffectiveInstanceBackend(inst),
				Inst:     inst,
				Thread:   thread,
			}
		}
		mode := attachSurfaceToKnownThreadDefault
		if pending.AutoRestore {
			mode = attachSurfaceToKnownThreadHeadlessRestore
		}
		return s.attachSurfaceToKnownThread(surface, inst, view, mode)
	}
	surface.PendingHeadless = nil
	events := []eventcontract.Event{}
	if surface.AttachedInstanceID == pending.InstanceID {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
	events = append(events,
		eventcontract.Event{
			Kind:             eventcontract.KindDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandKillHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       pending.InstanceID,
			},
		},
		eventcontract.Event{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "legacy_headless_restore_cancelled",
				Title: "旧恢复流程已结束",
				Text:  "检测到旧版后台恢复残留，已自动结束。请改用 `/use` 或 `/useall` 选择要恢复的会话。",
			},
		},
	)
	return events
}
