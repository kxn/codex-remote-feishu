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
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return notice(surface, "workspace_not_found", "目标工作区不存在。请重新发送 /useall。")
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
		return notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。", workspaceKey))
	}
	options := make([]control.SelectionOption, 0, len(filtered))
	for i, view := range filtered {
		status, disabled := s.threadSelectionStatus(surface, view, true)
		summary := s.threadSelectionSummary(surface, view)
		options = append(options, control.SelectionOption{
			Index:               i + 1,
			OptionID:            view.ThreadID,
			Label:               summary,
			Subtitle:            s.threadSelectionOptionSubtitle(surface, view, false, true),
			ButtonLabel:         summary,
			GroupKey:            workspaceKey,
			GroupLabel:          workspaceSelectionLabel(workspaceKey),
			AgeText:             humanizeRelativeTime(s.now(), threadLastUsedAt(view)),
			MetaText:            s.threadSelectionMetaText(surface, view, status),
			IsCurrent:           surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID),
			Disabled:            disabled,
			AllowCrossWorkspace: true,
		})
	}
	options = append(options, control.SelectionOption{
		Index:       len(options) + 1,
		ButtonLabel: "全部会话",
		Subtitle:    "回到跨工作区会话列表",
		ActionKind:  "show_all_threads",
	})
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:    control.SelectionPromptUseThread,
			Layout:  "workspace_grouped_useall",
			Title:   workspaceSelectionLabel(workspaceKey) + " 全部会话",
			Options: options,
		},
	}}
}

func (s *Service) presentThreadSelectionMode(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode) []control.UIEvent {
	if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return notice(surface, "not_attached_vscode", "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。")
	}
	presentation := s.resolveThreadSelectionPresentation(surface, mode)
	if len(presentation.views) == 0 {
		if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) != "" {
			return notice(surface, "no_visible_threads", "当前接管的 VS Code 实例还没有已知会话。请先在 VS Code 里实际操作一次会话，再重试。")
		}
		if workspaceKey := s.threadSelectionWorkspaceScope(surface); workspaceKey != "" {
			return notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。你可以直接 /new、发送 /useall 查看其他 workspace 的会话，或先 /list 切换工作区。", workspaceKey))
		}
		return notice(surface, "no_visible_threads", "当前还没有可恢复会话。")
	}
	limit := presentation.limit
	if limit <= 0 || limit > len(presentation.views) {
		limit = len(presentation.views)
	}
	threads := presentation.views[:limit]
	options := make([]control.SelectionOption, 0, len(threads)+1)
	for i, view := range threads {
		status, disabled := s.threadSelectionStatus(surface, view, presentation.allowCrossWorkspace)
		summary := s.threadSelectionSummary(surface, view)
		workspaceKey := mergedThreadWorkspaceClaimKey(view)
		options = append(options, control.SelectionOption{
			Index:               i + 1,
			OptionID:            view.ThreadID,
			Label:               summary,
			Subtitle:            s.threadSelectionOptionSubtitle(surface, view, presentation.includeWorkspace, presentation.allowCrossWorkspace),
			ButtonLabel:         summary,
			GroupKey:            workspaceKey,
			GroupLabel:          workspaceSelectionLabel(workspaceKey),
			AgeText:             humanizeRelativeTime(s.now(), threadLastUsedAt(view)),
			MetaText:            s.threadSelectionMetaText(surface, view, status),
			IsCurrent:           surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID),
			Disabled:            disabled,
			AllowCrossWorkspace: presentation.allowCrossWorkspace,
		})
	}
	if presentation.showMoreButton {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: presentation.showMoreButtonText,
			Subtitle:    presentation.showMoreStatus,
			ActionKind:  presentation.showMoreActionKind,
		})
	}
	if strings.TrimSpace(presentation.returnActionKind) != "" {
		options = append(options, control.SelectionOption{
			Index:       len(options) + 1,
			ButtonLabel: presentation.returnButtonText,
			Subtitle:    presentation.returnStatus,
			ActionKind:  presentation.returnActionKind,
		})
	}
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:         control.SelectionPromptUseThread,
			Layout:       s.threadSelectionPromptLayout(surface, presentation),
			Title:        presentation.title,
			ContextTitle: s.threadSelectionContextTitle(surface, presentation),
			ContextText:  s.threadSelectionContextText(surface, presentation),
			ContextKey:   s.threadSelectionContextKey(surface, presentation),
			Options:      options,
		},
	}}
}

func (s *Service) threadSelectionPromptLayout(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return "vscode_instance_threads"
	}
	if presentation.title == "全部会话" && presentation.includeWorkspace {
		return "workspace_grouped_useall"
	}
	return ""
}

func (s *Service) threadSelectionContextTitle(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return ""
	}
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return "当前实例"
	}
	if !presentation.includeWorkspace || presentation.title != "全部会话" {
		return ""
	}
	if workspaceKey := s.surfaceCurrentWorkspaceKey(surface); workspaceKey != "" {
		return "当前工作区"
	}
	return ""
}

func (s *Service) threadSelectionContextText(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return s.vscodeThreadSelectionContextText(surface)
	}
	workspaceKey := s.threadSelectionContextKey(surface, presentation)
	if workspaceKey == "" {
		return ""
	}
	label := workspaceSelectionLabel(workspaceKey)
	if label == "" {
		label = workspaceKey
	}
	parts := []string{label}
	if latest := threadViewsLatestUsedAt(s.scopedMergedThreadViews(surface)); !latest.IsZero() {
		parts[0] += " · " + humanizeRelativeTime(s.now(), latest)
	}
	parts = append(parts, "同工作区内切换请直接用 /use")
	return strings.Join(parts, "\n")
}

func (s *Service) threadSelectionContextKey(surface *state.SurfaceConsoleRecord, presentation threadSelectionPresentation) string {
	if surface == nil || !presentation.includeWorkspace || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return ""
	}
	if s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
		return ""
	}
	if presentation.title != "全部会话" {
		return ""
	}
	return s.surfaceCurrentWorkspaceKey(surface)
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
		source = shortenThreadID(fallback)
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

func (s *Service) resolveThreadSelectionPresentation(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode) threadSelectionPresentation {
	productMode := state.ProductModeNormal
	if surface != nil {
		productMode = s.normalizeSurfaceProductMode(surface)
	}
	switch productMode {
	case state.ProductModeVSCode:
		views := s.scopedMergedThreadViews(surface)
		presentation := threadSelectionPresentation{
			title:            "最近会话",
			views:            views,
			limit:            min(5, len(views)),
			includeWorkspace: false,
		}
		switch mode {
		case threadSelectionDisplayAll:
			presentation.title = "当前实例全部会话"
			presentation.limit = len(views)
		case threadSelectionDisplayAllExpanded:
			presentation.title = "当前实例全部会话"
			presentation.limit = len(views)
		case threadSelectionDisplayScopedAll:
			presentation.title = "当前实例全部会话"
			presentation.limit = len(views)
			presentation.returnActionKind = "show_threads"
			presentation.returnButtonText = "最近会话"
			presentation.returnStatus = "回到当前实例最近 5 个会话"
		default:
			if len(views) > 5 {
				presentation.showMoreButton = true
				presentation.showMoreActionKind = "show_scoped_threads"
				presentation.showMoreButtonText = "当前实例全部会话"
				presentation.showMoreStatus = "展开当前实例内的全部会话"
			}
		}
		return presentation
	default:
		attached := surface != nil && strings.TrimSpace(surface.AttachedInstanceID) != ""
		if !attached || mode == threadSelectionDisplayAll || mode == threadSelectionDisplayAllExpanded {
			views := s.threadViewsVisibleInNormalList(surface, s.mergedThreadViews(surface))
			currentWorkspaceKey := ""
			if attached {
				currentWorkspaceKey = s.surfaceCurrentWorkspaceKey(surface)
			}
			totalGroups := countThreadWorkspaceGroups(views, currentWorkspaceKey)
			presentation := threadSelectionPresentation{
				title:               "全部会话",
				views:               views,
				limit:               len(views),
				includeWorkspace:    true,
				allowCrossWorkspace: true,
			}
			if mode != threadSelectionDisplayAllExpanded {
				filtered, visibleGroups := filterThreadViewsToRecentWorkspaceGroups(views, currentWorkspaceKey, workspaceSelectionRecentLimit)
				presentation.views = filtered
				presentation.limit = len(filtered)
				if totalGroups > visibleGroups {
					presentation.showMoreButton = true
					presentation.showMoreActionKind = "show_all_thread_workspaces"
					presentation.showMoreButtonText = "全部工作区"
					presentation.showMoreStatus = fmt.Sprintf("还有 %d 个工作区未显示", totalGroups-visibleGroups)
				}
				return presentation
			}
			if totalGroups > workspaceSelectionRecentLimit {
				presentation.returnActionKind = "show_recent_thread_workspaces"
				presentation.returnButtonText = "最近工作区"
				presentation.returnStatus = fmt.Sprintf("回到最近 %d 个工作区", workspaceSelectionRecentLimit)
			}
			return presentation
		}
		views := s.scopedMergedThreadViews(surface)
		presentation := threadSelectionPresentation{
			title:            "最近会话",
			views:            views,
			limit:            min(5, len(views)),
			includeWorkspace: false,
		}
		if mode == threadSelectionDisplayScopedAll {
			presentation.title = "当前工作区全部会话"
			presentation.limit = len(views)
			presentation.returnActionKind = "show_threads"
			presentation.returnButtonText = "最近会话"
			presentation.returnStatus = "回到当前工作区最近 5 个会话"
			return presentation
		}
		if len(views) > 5 {
			presentation.showMoreButton = true
			presentation.showMoreActionKind = "show_scoped_threads"
			presentation.showMoreButtonText = "当前工作区全部会话"
			presentation.showMoreStatus = "展开当前工作区内的全部会话"
		}
		return presentation
	}
}

func countThreadWorkspaceGroups(views []*mergedThreadView, excludeWorkspaceKey string) int {
	excludeWorkspaceKey = normalizeWorkspaceClaimKey(excludeWorkspaceKey)
	seen := map[string]struct{}{}
	for _, view := range views {
		workspaceKey := normalizeWorkspaceClaimKey(mergedThreadWorkspaceClaimKey(view))
		if workspaceKey == "" || workspaceKey == excludeWorkspaceKey {
			continue
		}
		seen[workspaceKey] = struct{}{}
	}
	return len(seen)
}

func filterThreadViewsToRecentWorkspaceGroups(views []*mergedThreadView, excludeWorkspaceKey string, limit int) ([]*mergedThreadView, int) {
	if len(views) == 0 {
		return nil, 0
	}
	excludeWorkspaceKey = normalizeWorkspaceClaimKey(excludeWorkspaceKey)
	seenGroups := map[string]struct{}{}
	visibleGroups := map[string]struct{}{}
	filtered := make([]*mergedThreadView, 0, len(views))
	for _, view := range views {
		workspaceKey := normalizeWorkspaceClaimKey(mergedThreadWorkspaceClaimKey(view))
		if workspaceKey == "" {
			continue
		}
		if workspaceKey == excludeWorkspaceKey {
			filtered = append(filtered, view)
			continue
		}
		if _, ok := seenGroups[workspaceKey]; !ok {
			seenGroups[workspaceKey] = struct{}{}
			if len(visibleGroups) < limit {
				visibleGroups[workspaceKey] = struct{}{}
			}
		}
		if _, ok := visibleGroups[workspaceKey]; ok {
			filtered = append(filtered, view)
		}
	}
	return filtered, len(visibleGroups)
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
