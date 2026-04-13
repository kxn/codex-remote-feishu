package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) presentThreadSelection(surface *state.SurfaceConsoleRecord, showAll bool) []control.UIEvent {
	mode := threadSelectionDisplayRecent
	if showAll {
		mode = threadSelectionDisplayAll
	}
	return s.presentThreadSelectionMode(surface, mode)
}

func (s *Service) presentAllThreadWorkspaces(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	return s.presentThreadSelectionMode(surface, threadSelectionDisplayAllExpanded)
}

func (s *Service) presentScopedThreadSelection(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	return s.presentThreadSelectionMode(surface, threadSelectionDisplayScopedAll)
}

func (s *Service) presentWorkspaceThreadSelection(surface *state.SurfaceConsoleRecord, workspaceKey string) []control.UIEvent {
	model, events := s.buildWorkspaceThreadSelectionModel(surface, workspaceKey)
	if len(events) != 0 {
		return events
	}
	if model == nil {
		return nil
	}
	return []control.UIEvent{s.selectionViewEvent(surface, control.FeishuSelectionView{
		PromptKind: control.SelectionPromptUseThread,
		Thread:     model,
	})}
}

func (s *Service) buildWorkspaceThreadSelectionModel(surface *state.SurfaceConsoleRecord, workspaceKey string) (*control.FeishuThreadSelectionView, []control.UIEvent) {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return nil, notice(surface, "workspace_not_found", "目标工作区不存在。请重新发送 /useall。")
	}
	views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
	filtered := make([]*mergedThreadView, 0, len(views))
	for _, view := range views {
		if mergedThreadWorkspaceClaimKey(view) != workspaceKey {
			continue
		}
		filtered = append(filtered, view)
	}
	if len(filtered) == 0 {
		return nil, notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。", workspaceKey))
	}
	model := &control.FeishuThreadSelectionView{
		Mode:        control.FeishuThreadSelectionNormalWorkspaceView,
		RecentLimit: workspaceSelectionRecentLimit,
		Workspace: &control.FeishuThreadSelectionWorkspaceContext{
			WorkspaceKey:   workspaceKey,
			WorkspaceLabel: workspaceSelectionLabel(workspaceKey),
		},
		Entries: make([]control.FeishuThreadSelectionEntry, 0, len(filtered)),
	}
	for _, view := range filtered {
		model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, true))
	}
	return model, nil
}

func (s *Service) presentThreadSelectionMode(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode) []control.UIEvent {
	model, events := s.buildThreadSelectionModel(surface, mode)
	if len(events) != 0 {
		return events
	}
	if model == nil {
		return nil
	}
	return []control.UIEvent{s.selectionViewEvent(surface, control.FeishuSelectionView{
		PromptKind: control.SelectionPromptUseThread,
		Thread:     model,
	})}
}

func (s *Service) buildThreadSelectionModel(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode) (*control.FeishuThreadSelectionView, []control.UIEvent) {
	if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return nil, notice(surface, "not_attached_vscode", "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。")
	}
	productMode := state.ProductModeNormal
	if surface != nil {
		productMode = s.normalizeSurfaceProductMode(surface)
	}
	model := &control.FeishuThreadSelectionView{RecentLimit: workspaceSelectionRecentLimit}
	switch productMode {
	case state.ProductModeVSCode:
		views := s.scopedMergedThreadViews(surface)
		if surface != nil {
			if inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]; inst != nil {
				model.CurrentInstance = &control.FeishuThreadSelectionInstanceContext{
					Label:  instanceSelectionLabel(inst),
					Status: s.vscodeInstanceSurfaceStatus(surface, inst),
				}
			}
		}
		switch mode {
		case threadSelectionDisplayScopedAll:
			model.Mode = control.FeishuThreadSelectionVSCodeScopedAll
		case threadSelectionDisplayAll, threadSelectionDisplayAllExpanded:
			model.Mode = control.FeishuThreadSelectionVSCodeAll
		default:
			model.Mode = control.FeishuThreadSelectionVSCodeRecent
		}
		for _, view := range views {
			model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, false))
		}
	default:
		attached := surface != nil && strings.TrimSpace(surface.AttachedInstanceID) != ""
		if !attached || mode == threadSelectionDisplayAll || mode == threadSelectionDisplayAllExpanded {
			if workspaceKey := s.surfaceCurrentWorkspaceKey(surface); workspaceKey != "" {
				model.CurrentWorkspace = &control.FeishuThreadSelectionWorkspaceContext{
					WorkspaceKey:   workspaceKey,
					WorkspaceLabel: workspaceSelectionLabel(workspaceKey),
					AgeText:        humanizeRelativeTime(s.now(), threadViewsLatestUsedAt(s.scopedMergedThreadViews(surface))),
				}
			}
			views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
			if mode == threadSelectionDisplayAllExpanded {
				model.Mode = control.FeishuThreadSelectionNormalGlobalAll
			} else {
				model.Mode = control.FeishuThreadSelectionNormalGlobalRecent
			}
			for _, view := range views {
				model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, true))
			}
		} else {
			views := s.scopedMergedThreadViews(surface)
			if mode == threadSelectionDisplayScopedAll {
				model.Mode = control.FeishuThreadSelectionNormalScopedAll
			} else {
				model.Mode = control.FeishuThreadSelectionNormalScopedRecent
			}
			for _, view := range views {
				model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, false))
			}
		}
	}
	if len(model.Entries) == 0 {
		if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) != "" {
			return nil, notice(surface, "no_visible_threads", "当前接管的 VS Code 实例还没有已知会话。请先在 VS Code 里实际操作一次会话，再重试。")
		}
		if workspaceKey := s.threadSelectionWorkspaceScope(surface); workspaceKey != "" {
			return nil, notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。你可以直接发送文本开启新会话（或 /new 先进入待命），发送 /useall 查看其他 workspace 的会话，或先 /list 切换工作区。", workspaceKey))
		}
		return nil, notice(surface, "no_visible_threads", "当前还没有可恢复会话。")
	}
	return model, nil
}

func (s *Service) threadSelectionViewEntry(surface *state.SurfaceConsoleRecord, view *mergedThreadView, allowCrossWorkspace bool) control.FeishuThreadSelectionEntry {
	status, disabled := s.threadSelectionStatus(surface, view, allowCrossWorkspace)
	workspaceKey := mergedThreadWorkspaceClaimKey(view)
	return control.FeishuThreadSelectionEntry{
		ThreadID:            view.ThreadID,
		Summary:             s.threadSelectionSummary(surface, view),
		WorkspaceKey:        workspaceKey,
		WorkspaceLabel:      workspaceSelectionLabel(workspaceKey),
		AgeText:             humanizeRelativeTime(s.now(), threadLastUsedAt(view)),
		Status:              status,
		VSCodeFocused:       view != nil && view.Inst != nil && strings.TrimSpace(view.Inst.ObservedFocusedThreadID) == view.ThreadID,
		Disabled:            disabled,
		AllowCrossWorkspace: allowCrossWorkspace,
		Current:             surface != nil && surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID),
	}
}

func (s *Service) threadSelectionSummary(surface *state.SurfaceConsoleRecord, view *mergedThreadView) string {
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return vscodeThreadSelectionButtonLabel(view.Thread, view.ThreadID)
	}
	return threadSelectionButtonLabel(view.Thread, view.ThreadID)
}

func vscodeThreadSelectionButtonLabel(thread *state.ThreadRecord, fallback string) string {
	source := threadDisplayBody(thread, 20)
	if source == "" {
		source = control.ShortenThreadID(fallback)
	}
	if source == "" {
		source = "未命名会话"
	}
	return source
}

func (s *Service) vscodeInstanceSurfaceStatus(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if surface == nil || inst == nil || strings.TrimSpace(surface.AttachedInstanceID) != inst.InstanceID {
		return ""
	}
	if strings.TrimSpace(surface.SelectedThreadID) != "" {
		if surface.RouteMode == state.RouteModeFollowLocal {
			return "当前跟随中"
		}
		return "已接管"
	}
	if instanceHasObservedFocus(inst) {
		return "当前焦点可跟随"
	}
	return "等待 VS Code 焦点"
}

func (s *Service) vscodeThreadSelectionContextText(surface *state.SurfaceConsoleRecord) string {
	if surface == nil {
		return ""
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return ""
	}
	label := instanceSelectionLabel(inst)
	status := s.vscodeInstanceSurfaceStatus(surface, inst)
	if status == "" {
		return label
	}
	return label + " · " + status
}

func (s *Service) TryAutoRestoreHeadless(surfaceID string, attempt HeadlessRestoreAttempt, allowMissingThreadFailure bool) ([]control.UIEvent, HeadlessRestoreResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
	view := s.headlessRestoreView(surface, attempt)
	if view == nil {
		if !allowMissingThreadFailure {
			return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusWaiting}
		}
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_not_found"),
		}}, HeadlessRestoreResult{Status: HeadlessRestoreStatusFailed, FailureCode: "thread_not_found"}
	}
	target := s.resolveHeadlessRestoreTargetFromView(surface, view)
	switch target.Mode {
	case threadAttachFreeVisible, threadAttachReuseHeadless:
		return s.attachSurfaceToKnownThread(surface, target.Instance, target.View, attachSurfaceToKnownThreadHeadlessRestore), HeadlessRestoreResult{Status: HeadlessRestoreStatusAttached}
	case threadAttachCreateHeadless:
		return s.startHeadlessForResolvedThreadWithMode(surface, target.View, startHeadlessModeHeadlessRestore), HeadlessRestoreResult{Status: HeadlessRestoreStatusStarting}
	case threadAttachUnavailable:
		if target.NoticeCode == "thread_not_found" && !allowMissingThreadFailure {
			return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusWaiting}
		}
		failureCode := firstNonEmpty(strings.TrimSpace(target.NoticeCode), "thread_not_found")
		return []control.UIEvent{{
			Kind:             control.UIEventNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice(failureCode),
		}}, HeadlessRestoreResult{Status: HeadlessRestoreStatusFailed, FailureCode: failureCode}
	default:
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
}

func (s *Service) TryAutoResumeNormalSurface(surfaceID string, attempt SurfaceResumeAttempt, allowMissingTargetFailure bool) ([]control.UIEvent, SurfaceResumeResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}

	failureCode := ""
	threadID := strings.TrimSpace(attempt.ThreadID)
	if threadID != "" {
		view := s.mergedThreadView(surface, threadID)
		if inst, code := s.resolveSurfaceResumeVisibleInstance(surface, view, strings.TrimSpace(attempt.InstanceID)); inst != nil {
			return s.attachSurfaceToKnownThread(surface, inst, view, attachSurfaceToKnownThreadSurfaceResume), SurfaceResumeResult{Status: SurfaceResumeStatusThreadAttached}
		} else if code != "" {
			failureCode = code
		}
		if !allowMissingTargetFailure {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
		}
	}

	workspaceKey := normalizeWorkspaceClaimKey(attempt.WorkspaceKey)
	if workspaceKey != "" {
		if owner := s.workspaceBusyOwnerForSurface(surface, workspaceKey); owner != nil {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "workspace_busy"}
		}
		if inst := s.resolveWorkspaceAttachInstance(surface, workspaceKey); inst != nil {
			return s.attachWorkspaceWithMode(surface, workspaceKey, attachWorkspaceModeSurfaceResume), SurfaceResumeResult{Status: SurfaceResumeStatusWorkspaceAttached}
		}
		if len(s.workspaceOnlineInstances(workspaceKey)) == 0 {
			if !allowMissingTargetFailure {
				return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
			}
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: firstNonEmpty(failureCode, "workspace_not_found")}
		}
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "workspace_instance_busy"}
	}

	if failureCode == "" {
		failureCode = "thread_not_found"
	}
	if !allowMissingTargetFailure && failureCode == "thread_not_found" {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
	}
	return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: failureCode}
}

func (s *Service) TryAutoResumeVSCodeSurface(surfaceID, instanceID string) ([]control.UIEvent, SurfaceResumeResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeVSCode {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}

	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	inst := s.root.Instances[instanceID]
	if inst == nil || !inst.Online || !isVSCodeInstance(inst) {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
	}
	if owner := s.instanceClaimSurface(instanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "instance_busy"}
	}
	return s.attachInstanceWithMode(surface, instanceID, attachInstanceModeSurfaceResume), SurfaceResumeResult{Status: SurfaceResumeStatusInstanceAttached}
}

func (s *Service) headlessRestoreView(surface *state.SurfaceConsoleRecord, attempt HeadlessRestoreAttempt) *mergedThreadView {
	threadID := strings.TrimSpace(attempt.ThreadID)
	if threadID == "" {
		return nil
	}
	view := s.mergedThreadView(surface, threadID)
	if view == nil {
		return s.syntheticHeadlessRestoreView(threadID, attempt.ThreadTitle, attempt.ThreadCWD)
	}
	cloned := *view
	thread := &state.ThreadRecord{ThreadID: threadID}
	if view.Thread != nil {
		copy := *view.Thread
		thread = &copy
	}
	if strings.TrimSpace(thread.Name) == "" {
		thread.Name = strings.TrimSpace(attempt.ThreadTitle)
	}
	if strings.TrimSpace(thread.CWD) == "" {
		thread.CWD = strings.TrimSpace(attempt.ThreadCWD)
	}
	cloned.Thread = thread
	return &cloned
}

func (s *Service) syntheticHeadlessRestoreView(threadID, threadTitle, threadCWD string) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	threadCWD = strings.TrimSpace(threadCWD)
	threadTitle = strings.TrimSpace(threadTitle)
	if threadID == "" || threadCWD == "" {
		return nil
	}
	view := &mergedThreadView{
		ThreadID: threadID,
		Thread: &state.ThreadRecord{
			ThreadID: threadID,
			Name:     threadTitle,
			CWD:      threadCWD,
			Loaded:   true,
		},
	}
	if owner := s.threadClaimSurface(threadID); owner != nil {
		view.BusyOwner = owner
	}
	return view
}

func headlessRestoreFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "workspace_busy":
		return &control.Notice{
			Code:  "headless_restore_workspace_busy",
			Title: "恢复失败",
			Text:  "之前的 workspace 当前被其他飞书会话占用，暂时无法恢复，请稍后重试或尝试其他会话。",
		}
	case "thread_busy":
		return &control.Notice{
			Code:  "headless_restore_thread_busy",
			Title: "恢复失败",
			Text:  "之前的会话当前被其他窗口占用，暂时无法恢复，请稍后重试或尝试其他会话。",
		}
	case "thread_cwd_missing":
		return &control.Notice{
			Code:  "headless_restore_thread_cwd_missing",
			Title: "恢复失败",
			Text:  "之前的会话缺少可恢复的工作目录，暂时无法自动恢复，请稍后重试或尝试其他会话。",
		}
	default:
		return &control.Notice{
			Code:  "headless_restore_thread_not_found",
			Title: "恢复失败",
			Text:  "暂时无法找到之前会话，请稍后重试或尝试其他会话。",
		}
	}
}

func surfaceResumeFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "workspace_busy":
		return &control.Notice{
			Code:  "surface_resume_workspace_busy",
			Title: "恢复失败",
			Text:  "之前的工作区当前被其他飞书会话接管，暂时无法恢复。请稍后重试，或发送 /list 重新选择工作区。",
		}
	case "workspace_instance_busy":
		return &control.Notice{
			Code:  "surface_resume_workspace_instance_busy",
			Title: "恢复失败",
			Text:  "之前的工作区当前暂时不可接管。请稍后重试，或发送 /list 重新选择工作区。",
		}
	case "thread_busy":
		return &control.Notice{
			Code:  "surface_resume_thread_busy",
			Title: "恢复失败",
			Text:  "之前的会话当前被其他飞书会话占用，暂时无法直接恢复。请稍后重试，或发送 /use 选择其他会话。",
		}
	default:
		return &control.Notice{
			Code:  "surface_resume_target_not_found",
			Title: "恢复失败",
			Text:  "暂时无法恢复到之前会话。请稍后重试，或发送 /list 重新选择工作区。",
		}
	}
}

func NoticeForSurfaceResumeFailure(code string) *control.Notice {
	return surfaceResumeFailureNotice(code)
}

func vscodeSurfaceResumeFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "instance_busy":
		return &control.Notice{
			Code:  "surface_resume_instance_busy",
			Title: "恢复失败",
			Text:  "之前的 VS Code 实例当前已被其他飞书会话接管，暂时无法恢复。请稍后重试，或发送 /list 重新选择实例。",
		}
	default:
		return &control.Notice{
			Code:  "surface_resume_instance_not_found",
			Title: "恢复失败",
			Text:  "暂时无法恢复到之前的 VS Code 实例。请稍后重试，或发送 /list 重新选择实例。",
		}
	}
}

func NoticeForVSCodeSurfaceResumeFailure(code string) *control.Notice {
	return vscodeSurfaceResumeFailureNotice(code)
}

func NoticeForVSCodeOpenPrompt(hadPreviousInstance bool) *control.Notice {
	if hadPreviousInstance {
		return &control.Notice{
			Code:  "surface_resume_open_vscode",
			Title: "请先打开 VS Code",
			Text:  "还没有找到之前的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。",
		}
	}
	return &control.Notice{
		Code:  "vscode_open_required",
		Title: "请先打开 VS Code",
		Text:  "当前还没有可用的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。",
	}
}
