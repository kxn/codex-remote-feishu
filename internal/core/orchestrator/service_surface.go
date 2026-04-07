package orchestrator

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"path/filepath"
	"sort"
	"strings"
)

func (s *Service) ensureSurface(action control.Action) *state.SurfaceConsoleRecord {
	surface := s.root.Surfaces[action.SurfaceSessionID]
	if surface != nil {
		if action.GatewayID != "" {
			surface.GatewayID = action.GatewayID
		}
		if action.ChatID != "" {
			surface.ChatID = action.ChatID
		}
		if action.ActorUserID != "" {
			surface.ActorUserID = action.ActorUserID
		}
		if surface.PendingRequests == nil {
			surface.PendingRequests = map[string]*state.RequestPromptRecord{}
		}
		return surface
	}

	surface = &state.SurfaceConsoleRecord{
		SurfaceSessionID: action.SurfaceSessionID,
		Platform:         "feishu",
		GatewayID:        action.GatewayID,
		ChatID:           action.ChatID,
		ActorUserID:      action.ActorUserID,
		RouteMode:        state.RouteModeUnbound,
		DispatchMode:     state.DispatchModeNormal,
		QueueItems:       map[string]*state.QueueItemRecord{},
		StagedImages:     map[string]*state.StagedImageRecord{},
		PendingRequests:  map[string]*state.RequestPromptRecord{},
	}
	s.root.Surfaces[action.SurfaceSessionID] = surface
	return surface
}

func (s *Service) pendingHeadlessActionBlocked(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	if surface == nil || surface.PendingHeadless == nil {
		return nil
	}
	switch action.Kind {
	case control.ActionStatus,
		control.ActionKillInstance,
		control.ActionResumeHeadless,
		control.ActionReactionCreated,
		control.ActionMessageRecalled:
		return nil
	default:
		return notice(surface, headlessPendingNoticeCode(surface.PendingHeadless), headlessPendingNoticeText(surface.PendingHeadless))
	}
}

func (s *Service) expirePendingHeadless(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || pending == nil {
		return nil
	}
	surface.PendingHeadless = nil
	events := []control.UIEvent{}
	if surface.AttachedInstanceID == pending.InstanceID {
		events = append(events, s.finalizeDetachedSurface(surface)...)
	}
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
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "headless_start_timeout",
			Title: "Headless 实例超时",
			Text:  "headless 实例启动超时，已自动取消，请重新发送 /newinstance。",
		},
	})
	return events
}

func (s *Service) ensureThread(inst *state.InstanceRecord, threadID string) *state.ThreadRecord {
	if inst.Threads == nil {
		inst.Threads = map[string]*state.ThreadRecord{}
	}
	thread := inst.Threads[threadID]
	if thread != nil {
		return thread
	}
	thread = &state.ThreadRecord{ThreadID: threadID}
	inst.Threads[threadID] = thread
	return thread
}

func (s *Service) presentInstanceSelection(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	instances := make([]*state.InstanceRecord, 0, len(s.root.Instances))
	for _, inst := range s.root.Instances {
		if inst.Online {
			instances = append(instances, inst)
		}
	}
	if len(instances) == 0 {
		return notice(surface, "no_online_instances", "当前没有在线实例。请先在 VS Code 中打开 Codex 会话。")
	}
	sort.Slice(instances, func(i, j int) bool {
		if instances[i].WorkspaceKey == instances[j].WorkspaceKey {
			return instances[i].InstanceID < instances[j].InstanceID
		}
		return instances[i].WorkspaceKey < instances[j].WorkspaceKey
	})

	options := make([]control.SelectionOption, 0, len(instances))
	for i, inst := range instances {
		label := inst.ShortName
		if label == "" {
			label = filepath.Base(inst.WorkspaceKey)
		}
		if label == "" {
			label = inst.InstanceID
		}
		subtitle := inst.WorkspaceKey
		buttonLabel := ""
		current := surface.AttachedInstanceID == inst.InstanceID
		disabled := false
		if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
			disabled = true
			buttonLabel = "已占用"
			if subtitle == "" {
				subtitle = "已被其他飞书会话接管"
			} else {
				subtitle += "\n已被其他飞书会话接管"
			}
		}
		options = append(options, control.SelectionOption{
			Index:       i + 1,
			OptionID:    inst.InstanceID,
			Label:       label,
			Subtitle:    subtitle,
			ButtonLabel: buttonLabel,
			IsCurrent:   current,
			Disabled:    disabled,
		})
	}
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:    control.SelectionPromptAttachInstance,
			Title:   "在线实例",
			Options: options,
		},
	}}
}

func (s *Service) startHeadlessInstance(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.AttachedInstanceID != "" {
		return notice(surface, "headless_requires_detach", "当前会话已接管实例，请先 /detach 再创建 headless 实例。")
	}
	if surface.PendingHeadless != nil {
		return notice(surface, "headless_already_starting", "当前会话已有 headless 实例创建中，请等待完成或执行 /killinstance 取消。")
	}
	s.nextHeadlessID++
	instanceID := fmt.Sprintf("inst-headless-%d-%d", s.now().UnixNano(), s.nextHeadlessID)
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:  instanceID,
		RequestedAt: s.now(),
		ExpiresAt:   s.now().Add(s.config.HeadlessLaunchWait),
		Status:      state.HeadlessLaunchStarting,
	}
	return []control.UIEvent{
		{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_starting",
				Title: "创建 Headless 实例",
				Text:  "正在创建 headless 实例，稍后会自动加载可恢复会话。",
			},
		},
		{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandStartHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
			},
		},
	}
}

func (s *Service) presentHeadlessResumeSelection(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []control.UIEvent {
	if surface == nil || inst == nil {
		return nil
	}
	threads := visibleThreads(inst)
	if len(threads) == 0 {
		return nil
	}
	limit := len(threads)
	hint := ""
	if limit > 5 {
		limit = 5
		hint = "只显示最近 5 个已知会话。"
	}
	threads = threads[:limit]
	options := make([]control.SelectionOption, 0, len(threads))
	for i, thread := range threads {
		label := displayThreadTitle(inst, thread, thread.ThreadID)
		subtitle := threadSelectionSubtitle(thread, thread.ThreadID)
		options = append(options, control.SelectionOption{
			Index:    i + 1,
			OptionID: thread.ThreadID,
			Label:    label,
			Subtitle: subtitle,
		})
	}
	return []control.UIEvent{
		{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_ready_select_thread",
				Title: "Headless 实例已就绪",
				Text:  "请选择一个要恢复的会话；选定后下一条消息会在该会话继续。",
			},
		},
		{
			Kind:             control.UIEventSelectionPrompt,
			SurfaceSessionID: surface.SurfaceSessionID,
			SelectionPrompt: &control.SelectionPrompt{
				Kind:    control.SelectionPromptNewInstance,
				Title:   "选择要恢复的会话",
				Hint:    hint,
				Options: options,
			},
		},
	}
}

func (s *Service) attachInstance(surface *state.SurfaceConsoleRecord, instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return notice(surface, "instance_not_found", "实例不存在。")
	}
	if surface.AttachedInstanceID != "" && surface.AttachedInstanceID != instanceID {
		return notice(surface, "attach_requires_detach", "当前会话已接管其他实例，请先 /detach。")
	}
	if surface.AttachedInstanceID == instanceID {
		return notice(surface, "already_attached", fmt.Sprintf("当前已接管 %s。", inst.DisplayName))
	}
	if owner := s.instanceClaimSurface(instanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))
	}

	events := s.discardDrafts(surface)
	clearSurfaceRequestCapture(surface)
	clearSurfaceRequests(surface)
	s.releaseSurfaceThreadClaim(surface)
	s.clearPreparedNewThread(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	if !s.claimInstance(surface, instanceID) {
		return append(events, notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))...)
	}
	surface.AttachedInstanceID = instanceID
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)

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

	title := "未绑定会话"
	text := fmt.Sprintf("已接管 %s。", inst.DisplayName)
	if surface.SelectedThreadID != "" {
		title = displayThreadTitle(inst, inst.Threads[surface.SelectedThreadID], surface.SelectedThreadID)
		text = fmt.Sprintf("%s 当前输入目标：%s", text, title)
	} else if initialThreadID != "" {
		text = fmt.Sprintf("%s 默认会话当前已被其他飞书会话占用，请先通过 /use 选择可用会话。", text)
	} else if len(visibleThreads(inst)) != 0 {
		text = fmt.Sprintf("%s 当前还没有绑定会话，请先通过 /use 选择一个会话。", text)
	} else {
		text = fmt.Sprintf("%s 当前没有可用会话，请等待 VS Code 切到会话后再 /use，或直接 /detach。", text)
	}
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
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

func (s *Service) attachHeadlessInstance(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || inst == nil || pending == nil {
		return nil
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
				Inst:     inst,
				Thread:   thread,
			}
		}
		return s.attachSurfaceToKnownThread(surface, inst, view)
	}
	events := s.discardDrafts(surface)
	clearSurfaceRequestCapture(surface)
	s.releaseSurfaceThreadClaim(surface)
	s.clearPreparedNewThread(surface)
	surface.PromptOverride = state.ModelConfigRecord{}
	if !s.claimInstance(surface, inst.InstanceID) {
		surface.PendingHeadless = nil
		return append(events, notice(surface, "instance_busy", "新创建的 headless 实例已被其他飞书会话接管。")...)
	}
	surface.AttachedInstanceID = inst.InstanceID
	surface.ActiveQueueItemID = ""
	surface.DispatchMode = state.DispatchModeNormal
	surface.Abandoning = false
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	surface.SelectedThreadID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.LastSelection = &state.SelectionAnnouncementRecord{
		ThreadID:  "",
		RouteMode: string(surface.RouteMode),
		Title:     "",
		Preview:   "",
	}
	pending.Status = state.HeadlessLaunchSelecting
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code:  "headless_attached",
			Title: "Headless 实例已就绪",
			Text:  "已创建并接管 headless 实例，正在加载可恢复会话列表。",
		},
	})
	events = append(events, control.UIEvent{
		Kind:             control.UIEventAgentCommand,
		SurfaceSessionID: surface.SurfaceSessionID,
		Command: &agentproto.Command{
			Kind: agentproto.CommandThreadsRefresh,
			Origin: agentproto.Origin{
				Surface: surface.SurfaceSessionID,
				UserID:  surface.ActorUserID,
				ChatID:  surface.ChatID,
			},
		},
	})
	return events
}

func (s *Service) handlePendingHeadlessThreadSnapshot(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		pending := surface.PendingHeadless
		if pending == nil || pending.InstanceID != instanceID || pending.Status != state.HeadlessLaunchSelecting {
			continue
		}
		if len(visibleThreads(inst)) == 0 {
			events = append(events, s.failPendingHeadlessSelection(surface, pending)...)
			continue
		}
		events = append(events, s.presentHeadlessResumeSelection(surface, inst)...)
	}
	return events
}

func (s *Service) failPendingHeadlessSelection(surface *state.SurfaceConsoleRecord, pending *state.HeadlessLaunchRecord) []control.UIEvent {
	if surface == nil || pending == nil {
		return nil
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
				InstanceID:       pending.InstanceID,
			},
		},
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "no_recoverable_threads",
				Title: "没有可恢复会话",
				Text:  "headless 实例已启动，但没有发现可恢复的会话，已自动结束该实例。",
			},
		},
	)
	return events
}

func (s *Service) resumeHeadlessThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	return s.completeHeadlessThreadSelection(surface, threadID)
}

func (s *Service) completeHeadlessThreadSelection(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	pending := surface.PendingHeadless
	inst := s.root.Instances[surface.AttachedInstanceID]
	if pending == nil || inst == nil || pending.InstanceID != inst.InstanceID || !isHeadlessInstance(inst) {
		return notice(surface, "selection_expired", "之前的 headless 会话选择已过期，请重新发送 /newinstance。")
	}
	threadID = strings.TrimSpace(threadID)
	thread := inst.Threads[threadID]
	if !threadVisible(thread) || strings.TrimSpace(thread.CWD) == "" {
		return notice(surface, "headless_selection_invalid", "这个会话缺少可恢复的工作目录，无法恢复。")
	}
	thread = s.ensureThread(inst, threadID)
	thread.Loaded = true
	s.touchThread(thread)
	s.retargetManagedHeadlessInstance(inst, thread.CWD)
	surface.PendingHeadless = nil
	return s.useThread(surface, threadID)
}

func (s *Service) presentThreadSelection(surface *state.SurfaceConsoleRecord, showAll bool) []control.UIEvent {
	threads := s.mergedThreadViews(surface)
	if len(threads) == 0 {
		return notice(surface, "no_visible_threads", "当前还没有可恢复会话。")
	}
	limit := len(threads)
	title := "全部会话"
	hint := ""
	if !showAll {
		title = "最近会话"
		if limit > 5 {
			limit = 5
			hint = "发送 `/useall` 查看全部会话。"
		}
	}
	threads = threads[:limit]
	options := make([]control.SelectionOption, 0, len(threads))
	for i, view := range threads {
		buttonStatus, buttonLabel, disabled := s.mergedThreadStatus(surface, view)
		_ = buttonStatus
		options = append(options, control.SelectionOption{
			Index:       i + 1,
			OptionID:    view.ThreadID,
			Label:       displayThreadTitle(view.Inst, view.Thread, view.ThreadID),
			Subtitle:    s.mergedThreadSubtitle(surface, view),
			ButtonLabel: buttonLabel,
			IsCurrent:   surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID),
			Disabled:    disabled,
		})
	}
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:    control.SelectionPromptUseThread,
			Title:   title,
			Hint:    hint,
			Options: options,
		},
	}}
}

func (s *Service) useThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	threadID = strings.TrimSpace(threadID)
	target := s.resolveThreadTarget(surface, threadID)
	switch target.Mode {
	case threadAttachCurrentVisible:
		return s.useAttachedVisibleThread(surface, threadID)
	case threadAttachFreeVisible, threadAttachReuseHeadless:
		if blocked := s.blockFreshThreadAttach(surface); blocked != nil {
			return blocked
		}
		return s.attachSurfaceToKnownThread(surface, target.Instance, target.View)
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

func (s *Service) useAttachedVisibleThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	events := []control.UIEvent{}
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
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimThread(surface, inst, threadID) {
		surface.RouteMode = state.RouteModeUnbound
		s.clearPreparedNewThread(surface)
		return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
	}
	events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, threadID, state.RouteModePinned)...)
	surface.SelectedThreadID = threadID
	s.clearPreparedNewThread(surface)
	surface.RouteMode = state.RouteModePinned
	title := threadID
	preview := ""
	thread = s.ensureThread(inst, threadID)
	s.touchThread(thread)
	title = displayThreadTitle(inst, thread, threadID)
	preview = threadPreview(thread)
	events = append(events, s.threadSelectionEvents(surface, threadID, string(surface.RouteMode), title, preview)...)
	events = append(events, s.replayThreadUpdate(surface, inst, threadID)...)
	if len(events) != 0 {
		return events
	}
	return notice(surface, "selection_unchanged", fmt.Sprintf("当前输入目标保持为：%s", title))
}

func (s *Service) attachSurfaceToKnownThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, view *mergedThreadView) []control.UIEvent {
	if surface == nil || inst == nil || view == nil || strings.TrimSpace(view.ThreadID) == "" {
		return nil
	}
	if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))
	}

	events := []control.UIEvent{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
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

	if !s.claimInstance(surface, inst.InstanceID) {
		return append(events, notice(surface, "instance_busy", fmt.Sprintf("%s 当前已被其他飞书会话接管，请等待对方 /detach。", inst.DisplayName))...)
	}
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
		if strings.TrimSpace(view.Thread.State) != "" {
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
	s.touchThread(thread)
	s.releaseSurfaceThreadClaim(surface)
	if !s.claimKnownThread(surface, inst, view.ThreadID) {
		events = append(events, s.finalizeDetachedSurface(surface)...)
		return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
	}
	surface.SelectedThreadID = view.ThreadID
	surface.RouteMode = state.RouteModePinned

	title := displayThreadTitle(inst, thread, view.ThreadID)
	preview := threadPreview(thread)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "attached",
			Text: fmt.Sprintf("已接管 %s。当前输入目标：%s", inst.DisplayName, title),
		},
	})
	events = append(events, s.threadSelectionEvents(surface, view.ThreadID, string(surface.RouteMode), title, preview)...)
	events = append(events, s.replayThreadUpdate(surface, inst, view.ThreadID)...)
	events = append(events, s.maybeRequestThreadRefresh(surface, inst, view.ThreadID)...)
	return events
}

func (s *Service) startHeadlessForResolvedThread(surface *state.SurfaceConsoleRecord, view *mergedThreadView) []control.UIEvent {
	if surface == nil || view == nil {
		return nil
	}
	cwd := strings.TrimSpace(threadCWD(view))
	if cwd == "" {
		return notice(surface, "thread_cwd_missing", "目标会话缺少可恢复的工作目录，当前无法启动 headless 接管。")
	}
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

	events := []control.UIEvent{}
	if surface.AttachedInstanceID != "" {
		events = append(events, s.discardDrafts(surface)...)
		events = append(events, s.finalizeDetachedSurface(surface)...)
	} else {
		events = append(events, s.discardDrafts(surface)...)
		clearSurfaceRequestCapture(surface)
		clearSurfaceRequests(surface)
		s.clearPreparedNewThread(surface)
		surface.PromptOverride = state.ModelConfigRecord{}
	}
	surface.PendingHeadless = &state.HeadlessLaunchRecord{
		InstanceID:       instanceID,
		ThreadID:         view.ThreadID,
		ThreadTitle:      threadTitle,
		ThreadName:       threadName,
		ThreadPreview:    threadPreview,
		ThreadCWD:        cwd,
		RequestedAt:      s.now(),
		ExpiresAt:        s.now().Add(s.config.HeadlessLaunchWait),
		Status:           state.HeadlessLaunchStarting,
		SourceInstanceID: sourceInstanceID,
	}
	events = append(events,
		control.UIEvent{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice: &control.Notice{
				Code:  "headless_starting",
				Title: "创建 Headless 实例",
				Text:  fmt.Sprintf("正在创建 headless 实例并准备接管会话：%s", threadTitle),
			},
		},
		control.UIEvent{
			Kind:             control.UIEventDaemonCommand,
			SurfaceSessionID: surface.SurfaceSessionID,
			DaemonCommand: &control.DaemonCommand{
				Kind:             control.DaemonCommandStartHeadless,
				SurfaceSessionID: surface.SurfaceSessionID,
				InstanceID:       instanceID,
				ThreadID:         view.ThreadID,
				ThreadTitle:      threadTitle,
				ThreadCWD:        cwd,
			},
		},
	)
	return events
}

func (s *Service) prepareNewThread(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", "当前有待确认请求。请先点击卡片上的“允许一次”、“拒绝”或“告诉 Codex 怎么改”。")
	}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadReprepare(surface); blocked != nil {
			return blocked
		}
		if strings.TrimSpace(surface.PreparedThreadCWD) == "" {
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
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID == "" || !s.surfaceOwnsThread(surface, threadID) {
		return notice(surface, "new_thread_requires_bound_thread", "当前必须先绑定并接管一个会话，才能基于它的新建会话。请先 /use，或在 follow 模式下等到已跟随到会话。")
	}
	thread := inst.Threads[threadID]
	if !threadVisible(thread) {
		return notice(surface, "thread_not_found", "当前绑定的会话不存在或当前不可见。")
	}
	cwd := strings.TrimSpace(thread.CWD)
	if cwd == "" {
		return notice(surface, "new_thread_cwd_missing", "当前会话缺少可继承的工作目录，无法新建会话。")
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

func preparedNewThreadSelectionTitle() string {
	return "新建会话（等待首条消息）"
}

func (s *Service) handleModelCommand(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{{
			Kind:             control.UIEventSnapshot,
			SurfaceSessionID: surface.SurfaceSessionID,
			Snapshot:         s.buildSnapshot(surface),
		}}
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
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{{
			Kind:             control.UIEventSnapshot,
			SurfaceSessionID: surface.SurfaceSessionID,
			Snapshot:         s.buildSnapshot(surface),
		}}
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
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	parts := strings.Fields(strings.TrimSpace(action.Text))
	if len(parts) <= 1 {
		return []control.UIEvent{{
			Kind:             control.UIEventSnapshot,
			SurfaceSessionID: surface.SurfaceSessionID,
			Snapshot:         s.buildSnapshot(surface),
		}}
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
		return notice(surface, "request_pending", "当前有待确认请求。请先点击卡片上的“允许一次”、“拒绝”或“告诉 Codex 怎么改”。")
	}

	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
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
		return notice(surface, "thread_not_ready", "当前还没有可发送的目标会话。请先 /use，或执行 /follow 进入跟随模式。")
	}
	if createThread && strings.TrimSpace(cwd) == "" {
		s.restoreStagedInputs(surface, stagedMessageIDs)
		return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
	}
	return s.enqueueQueueItem(surface, action.MessageID, stagedMessageIDs, inputs, threadID, cwd, routeMode, surface.PromptOverride, false)
}

func (s *Service) stageImage(surface *state.SurfaceConsoleRecord, action control.Action) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", "当前还没有接管任何实例。")
	}
	if blocked := s.unboundInputBlocked(surface); blocked != nil {
		return blocked
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", "当前有待确认请求。请先处理确认卡片，再发送图片。")
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
		notice = control.Notice{
			Code:     "stop_not_interruptible",
			Title:    "当前还不能停止",
			Text:     "当前请求正在派发，尚未进入可中断状态。",
			ThemeKey: "system",
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

func (s *Service) killHeadlessInstance(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.PendingHeadless != nil {
		pending := surface.PendingHeadless
		events := s.discardDrafts(surface)
		if surface.AttachedInstanceID == pending.InstanceID {
			events = append(events, s.finalizeDetachedSurface(surface)...)
		}
		surface.PendingHeadless = nil
		return append(events,
			control.UIEvent{
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
			},
			control.UIEvent{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code:  "headless_cancelled",
					Title: "取消 Headless 实例",
					Text:  "已取消当前 headless 实例创建流程。",
				},
			},
		)
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "headless_not_found", "当前没有可结束的 headless 实例。")
	}
	if !isHeadlessInstance(inst) {
		return notice(surface, "headless_kill_forbidden", "当前接管的是 VS Code 实例，不能使用 /killinstance。")
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
				Title: "结束 Headless 实例",
				Text:  "已请求结束当前 headless 实例，并断开当前接管。",
			},
		},
	)
	return events
}

func (s *Service) detach(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface.PendingHeadless != nil {
		return notice(surface, "headless_pending", "当前 headless 创建流程尚未完成；如需取消，请执行 /killinstance。")
	}
	if surface.AttachedInstanceID == "" {
		return notice(surface, "detached", "当前没有接管中的实例。")
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
		return append(events, notice(surface, "detach_pending", "已放弃当前实例接管；未发送的队列和图片已清空，正在等待当前 turn 收尾。")...)
	}
	events = append(events, s.finalizeDetachedSurface(surface)...)
	return append(events, notice(surface, "detached", "已断开当前实例接管。")...)
}
