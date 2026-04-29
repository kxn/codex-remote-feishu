package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) presentThreadSelection(surface *state.SurfaceConsoleRecord, showAll bool) []eventcontract.Event {
	mode := threadSelectionDisplayRecent
	if showAll {
		mode = threadSelectionDisplayAll
	}
	return s.presentThreadSelectionModeAtCursorWithAction(surface, control.Action{}, mode, 1, 0)
}

func (s *Service) presentAllThreadWorkspaces(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.presentThreadSelectionModeAtCursorWithAction(surface, control.Action{}, threadSelectionDisplayAllExpanded, 1, 0)
}

func (s *Service) presentScopedThreadSelection(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return s.presentThreadSelectionModeAtCursorWithAction(surface, control.Action{}, threadSelectionDisplayScopedAll, 1, 0)
}

func (s *Service) presentWorkspaceThreadSelection(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return s.presentWorkspaceThreadSelectionPageWithAction(surface, control.Action{}, workspaceKey, 1, 1)
}

func (s *Service) presentWorkspaceThreadSelectionPage(surface *state.SurfaceConsoleRecord, workspaceKey string, page, returnPage int) []eventcontract.Event {
	return s.presentWorkspaceThreadSelectionPageWithAction(surface, control.Action{}, workspaceKey, page, returnPage)
}

func (s *Service) presentWorkspaceThreadSelectionPageWithAction(surface *state.SurfaceConsoleRecord, action control.Action, workspaceKey string, page, returnPage int) []eventcontract.Event {
	model, events := s.buildWorkspaceThreadSelectionModel(surface, workspaceKey, page, returnPage)
	if len(events) != 0 {
		return events
	}
	if model == nil {
		return nil
	}
	familyID, variantID, backend := s.catalogProvenanceForAction(surface, action)
	return []eventcontract.Event{s.selectionViewEvent(surface, control.FeishuSelectionView{
		PromptKind:       control.SelectionPromptUseThread,
		CatalogFamilyID:  familyID,
		CatalogVariantID: variantID,
		CatalogBackend:   backend,
		Thread:           model,
	})}
}

const (
	threadSelectionPageSize          = 8
	threadWorkspaceGroupPageSize     = 3
	threadWorkspaceGroupPreviewSize  = 2
	vscodeRecentThreadSelectionLimit = 5
)

func (s *Service) buildWorkspaceThreadSelectionModel(surface *state.SurfaceConsoleRecord, workspaceKey string, page, returnPage int) (*control.FeishuThreadSelectionView, []eventcontract.Event) {
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
	page, totalPages := paginatePage(page, len(filtered), threadSelectionPageSize)
	start, end := pageBounds(page, threadSelectionPageSize, len(filtered))
	model := &control.FeishuThreadSelectionView{
		Mode:       control.FeishuThreadSelectionNormalWorkspaceView,
		Page:       page,
		PageSize:   threadSelectionPageSize,
		TotalPages: totalPages,
		ReturnPage: returnPage,
		Workspace: &control.FeishuThreadSelectionWorkspaceContext{
			WorkspaceKey:   workspaceKey,
			WorkspaceLabel: workspaceSelectionLabel(workspaceKey),
		},
		Entries: make([]control.FeishuThreadSelectionEntry, 0, maxInt(end-start, 0)),
	}
	for _, view := range filtered[start:end] {
		model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, true))
	}
	return model, nil
}

func (s *Service) presentThreadSelectionMode(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode, page int) []eventcontract.Event {
	return s.presentThreadSelectionModeAtCursorWithAction(surface, control.Action{}, mode, page, 0)
}

func (s *Service) presentThreadSelectionModeAtCursor(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode, page, cursor int) []eventcontract.Event {
	return s.presentThreadSelectionModeAtCursorWithAction(surface, control.Action{}, mode, page, cursor)
}

func (s *Service) presentThreadSelectionModeAtCursorWithAction(surface *state.SurfaceConsoleRecord, action control.Action, mode threadSelectionDisplayMode, page, cursor int) []eventcontract.Event {
	model, events := s.buildThreadSelectionModelAtCursor(surface, mode, page, cursor)
	if len(events) != 0 {
		return events
	}
	if model == nil {
		return nil
	}
	familyID, variantID, backend := s.catalogProvenanceForAction(surface, action)
	return []eventcontract.Event{s.selectionViewEvent(surface, control.FeishuSelectionView{
		PromptKind:       control.SelectionPromptUseThread,
		CatalogFamilyID:  familyID,
		CatalogVariantID: variantID,
		CatalogBackend:   backend,
		Thread:           model,
	})}
}

func (s *Service) buildThreadSelectionModel(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode, page int) (*control.FeishuThreadSelectionView, []eventcontract.Event) {
	return s.buildThreadSelectionModelAtCursor(surface, mode, page, 0)
}

func (s *Service) buildThreadSelectionModelAtCursor(surface *state.SurfaceConsoleRecord, mode threadSelectionDisplayMode, page, cursor int) (*control.FeishuThreadSelectionView, []eventcontract.Event) {
	if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode && strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return nil, notice(surface, "not_attached_vscode", "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。")
	}
	productMode := state.ProductModeNormal
	if surface != nil {
		productMode = s.normalizeSurfaceProductMode(surface)
	}
	model := &control.FeishuThreadSelectionView{}
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
		selectedViews := views
		model.Page = 1
		model.TotalPages = 1
		switch model.Mode {
		case control.FeishuThreadSelectionVSCodeRecent:
			model.RecentLimit = vscodeRecentThreadSelectionLimit
			if len(selectedViews) > vscodeRecentThreadSelectionLimit {
				selectedViews = selectedViews[:vscodeRecentThreadSelectionLimit]
			}
		default:
			model.RecentLimit = vscodeRecentThreadSelectionLimit
		}
		model.Cursor = maxInt(cursor, 0)
		model.PageSize = len(selectedViews)
		for _, view := range selectedViews {
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
			currentWorkspaceKey := ""
			currentThreadID := ""
			if model.CurrentWorkspace != nil {
				currentWorkspaceKey = strings.TrimSpace(model.CurrentWorkspace.WorkspaceKey)
			}
			if surface != nil {
				currentThreadID = strings.TrimSpace(surface.SelectedThreadID)
			}
			model.PageSize = threadWorkspaceGroupPageSize
			groupedViews := paginateThreadSelectionWorkspaceGroups(views, currentWorkspaceKey, currentThreadID, page, threadWorkspaceGroupPageSize, threadWorkspaceGroupPreviewSize)
			model.Page = groupedViews.Page
			model.TotalPages = groupedViews.TotalPages
			for _, view := range groupedViews.Entries {
				model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, true))
			}
		} else {
			views := s.scopedMergedThreadViews(surface)
			if mode == threadSelectionDisplayScopedAll {
				model.Mode = control.FeishuThreadSelectionNormalScopedAll
			} else {
				model.Mode = control.FeishuThreadSelectionNormalScopedRecent
			}
			page, totalPages := paginatePage(page, len(views), threadSelectionPageSize)
			start, end := pageBounds(page, threadSelectionPageSize, len(views))
			model.Page = page
			model.PageSize = threadSelectionPageSize
			model.TotalPages = totalPages
			for _, view := range views[start:end] {
				model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, false))
			}
		}
	}
	if len(model.Entries) == 0 {
		if model.Mode == control.FeishuThreadSelectionNormalGlobalAll || model.Mode == control.FeishuThreadSelectionNormalGlobalRecent {
			if model.CurrentWorkspace != nil {
				return model, nil
			}
		}
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

func (s *Service) handleThreadSelectionPage(surface *state.SurfaceConsoleRecord, viewMode string, cursor int) []eventcontract.Event {
	return s.handleThreadSelectionPageWithAction(surface, control.Action{}, viewMode, cursor)
}

func (s *Service) handleThreadSelectionPageWithAction(surface *state.SurfaceConsoleRecord, action control.Action, viewMode string, cursor int) []eventcontract.Event {
	mode, ok := threadSelectionDisplayModeFromViewMode(viewMode)
	if !ok {
		return notice(surface, "thread_selection_page_invalid", "当前会话列表已过期，请重新发送 /use 或 /useall。")
	}
	return s.presentThreadSelectionModeAtCursorWithAction(surface, action, mode, 1, cursor)
}

func threadSelectionDisplayModeFromViewMode(viewMode string) (threadSelectionDisplayMode, bool) {
	switch strings.TrimSpace(viewMode) {
	case string(control.FeishuThreadSelectionVSCodeRecent):
		return threadSelectionDisplayRecent, true
	case string(control.FeishuThreadSelectionVSCodeAll):
		return threadSelectionDisplayAll, true
	case string(control.FeishuThreadSelectionVSCodeScopedAll):
		return threadSelectionDisplayScopedAll, true
	default:
		return "", false
	}
}

type pagedThreadGroupResult struct {
	Page       int
	TotalPages int
	Entries    []*mergedThreadView
}

func paginateThreadSelectionWorkspaceGroups(views []*mergedThreadView, excludeWorkspaceKey, currentThreadID string, page, groupPageSize, previewLimit int) pagedThreadGroupResult {
	type workspaceGroup struct {
		key     string
		entries []*mergedThreadView
	}
	excludeWorkspaceKey = normalizeWorkspaceClaimKey(excludeWorkspaceKey)
	groups := make([]workspaceGroup, 0)
	groupIndex := map[string]int{}
	currentEntries := make([]*mergedThreadView, 0)
	for _, view := range views {
		if view == nil {
			continue
		}
		workspaceKey := normalizeWorkspaceClaimKey(mergedThreadWorkspaceClaimKey(view))
		if workspaceKey != "" && workspaceKey == excludeWorkspaceKey {
			if strings.TrimSpace(view.ThreadID) != "" && strings.TrimSpace(view.ThreadID) == strings.TrimSpace(currentThreadID) {
				currentEntries = append(currentEntries, view)
			}
			continue
		}
		if workspaceKey == "" {
			continue
		}
		index, ok := groupIndex[workspaceKey]
		if !ok {
			index = len(groups)
			groupIndex[workspaceKey] = index
			groups = append(groups, workspaceGroup{key: workspaceKey})
		}
		groups[index].entries = append(groups[index].entries, view)
	}
	page, totalPages := paginatePage(page, len(groups), groupPageSize)
	start, end := pageBounds(page, groupPageSize, len(groups))
	result := pagedThreadGroupResult{
		Page:       page,
		TotalPages: totalPages,
		Entries:    append([]*mergedThreadView(nil), currentEntries...),
	}
	for _, group := range groups[start:end] {
		limit := len(group.entries)
		if previewLimit > 0 && limit > previewLimit {
			limit = previewLimit
		}
		result.Entries = append(result.Entries, group.entries[:limit]...)
	}
	return result
}

func pageBounds(page, pageSize, total int) (int, int) {
	page, _ = paginatePage(page, total, pageSize)
	if total <= 0 {
		return 0, 0
	}
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func paginatePage(page, total, pageSize int) (int, int) {
	if pageSize <= 0 {
		pageSize = 1
	}
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page <= 0 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	return page, totalPages
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
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
		return vscodeThreadSelectionDropdownLabel(view)
	}
	return threadSelectionButtonLabel(view.Thread, view.ThreadID)
}

func vscodeThreadSelectionDropdownLabel(view *mergedThreadView) string {
	if view == nil {
		return ""
	}
	return displayThreadTitle(view.Inst, view.Thread, view.ThreadID)
}

func vscodeThreadSelectionButtonLabel(thread *state.ThreadRecord, fallback string) string {
	return threadTitle(nil, thread, fallback)
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

func (s *Service) TryAutoRestoreHeadless(surfaceID string, attempt HeadlessRestoreAttempt, allowMissingThreadFailure bool) ([]eventcontract.Event, HeadlessRestoreResult) {
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
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
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
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice(failureCode),
		}}, HeadlessRestoreResult{Status: HeadlessRestoreStatusFailed, FailureCode: failureCode}
	default:
		return nil, HeadlessRestoreResult{Status: HeadlessRestoreStatusSkipped}
	}
}

func (s *Service) TryAutoResumeNormalSurface(surfaceID string, attempt SurfaceResumeAttempt, allowMissingTargetFailure bool) ([]eventcontract.Event, SurfaceResumeResult) {
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
	prepareNewThread := attempt.PrepareNewThread
	targetBackend := s.surfaceBackend(surface)
	if strings.TrimSpace(string(attempt.Backend)) != "" {
		targetBackend = agentproto.NormalizeBackend(attempt.Backend)
	}
	if threadID != "" {
		view := s.mergedThreadViewForBackend(surface, threadID, targetBackend, targetBackend == agentproto.BackendCodex)
		if inst, code := s.resolveSurfaceResumeVisibleInstance(surface, view, strings.TrimSpace(attempt.InstanceID), targetBackend); inst != nil {
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
		if inst := s.resolveWorkspaceAttachInstanceForBackend(surface, workspaceKey, targetBackend); inst != nil {
			options := attachWorkspaceOptions{ResumeNotice: !prepareNewThread, PrepareNewThread: prepareNewThread}
			return s.attachWorkspaceWithOptions(surface, workspaceKey, options), SurfaceResumeResult{Status: SurfaceResumeStatusWorkspaceAttached}
		}
		if len(s.workspaceOnlineInstancesForSurfaceBackend(surface, workspaceKey, targetBackend)) == 0 {
			if !allowMissingTargetFailure {
				return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
			}
			return s.startFreshWorkspaceHeadlessWithOptions(surface, workspaceKey, prepareNewThread || threadID != ""), SurfaceResumeResult{Status: SurfaceResumeStatusStarting}
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

func (s *Service) TryAutoResumeVSCodeSurface(surfaceID, instanceID string) ([]eventcontract.Event, SurfaceResumeResult) {
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
	if inst == nil || !inst.Online || !isVSCodeInstance(inst) || state.EffectiveInstanceBackend(inst) != agentproto.BackendCodex {
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
	view := s.mergedThreadViewForBackend(surface, threadID, agentproto.BackendCodex, true)
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
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_workspace_busy", "恢复失败", "之前的工作区当前被其他飞书会话接管，暂时无法恢复。请稍后重试，或发送 /list 重新选择工作区。")
		return &notice
	case "workspace_instance_busy":
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_workspace_instance_busy", "恢复失败", "之前的工作区当前暂时不可接管。请稍后重试，或发送 /list 重新选择工作区。")
		return &notice
	case "thread_busy":
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_thread_busy", "恢复失败", "之前的会话当前被其他飞书会话占用，暂时无法直接恢复。请稍后重试，或发送 /use 选择其他会话。")
		return &notice
	default:
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_target_not_found", "恢复失败", "暂时无法恢复到之前会话。请稍后重试，或发送 /list 重新选择工作区。")
		return &notice
	}
}

func NoticeForSurfaceResumeFailure(code string) *control.Notice {
	return surfaceResumeFailureNotice(code)
}

func vscodeSurfaceResumeFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "instance_busy":
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilyVSCodeResume, "surface_resume_instance_busy", "恢复失败", "之前的 VS Code 实例当前已被其他飞书会话接管，暂时无法恢复。请稍后重试，或发送 /list 重新选择实例。")
		return &notice
	default:
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilyVSCodeResume, "surface_resume_instance_not_found", "恢复失败", "暂时无法恢复到之前的 VS Code 实例。请稍后重试，或发送 /list 重新选择实例。")
		return &notice
	}
}

func NoticeForVSCodeSurfaceResumeFailure(code string) *control.Notice {
	return vscodeSurfaceResumeFailureNotice(code)
}

func NoticeForVSCodeOpenPrompt(hadPreviousInstance bool) *control.Notice {
	if hadPreviousInstance {
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilyVSCodeOpenPrompt, "surface_resume_open_vscode", "请先打开 VS Code", "还没有找到之前的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。")
		return &notice
	}
	notice := globalRuntimeNotice(control.NoticeDeliveryFamilyVSCodeOpenPrompt, "vscode_open_required", "请先打开 VS Code", "当前还没有可用的 VS Code 实例。请先打开 VS Code 中的 Codex，然后再回来使用。")
	return &notice
}
