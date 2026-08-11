package orchestrator

import (
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (s *Service) presentThreadSelection(surface *state.SurfaceConsoleRecord, showAll bool) []eventcontract.Event {
	mode := threadSelectionDisplayRecent
	if showAll {
		mode = threadSelectionDisplayAll
	}
	return s.presentThreadSelectionModeAtCursorWithAction(surface, control.Action{}, mode, 1, 0)
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
		Entries: make([]control.FeishuThreadSelectionEntry, 0, xutil.MaxInt(end-start, 0)),
	}
	for _, view := range filtered[start:end] {
		model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, true))
	}
	return model, nil
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
	if s.surfaceIsVSCode(surface) && strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return nil, notice(surface, "not_attached_vscode", "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。")
	}
	model := &control.FeishuThreadSelectionView{}
	if s.surfaceIsVSCode(surface) {
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
		model.Cursor = xutil.MaxInt(cursor, 0)
		model.PageSize = len(selectedViews)
		for _, view := range selectedViews {
			model.Entries = append(model.Entries, s.threadSelectionViewEntry(surface, view, false))
		}
	} else {
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
		if s.surfaceIsVSCode(surface) && strings.TrimSpace(surface.AttachedInstanceID) != "" {
			return nil, notice(surface, "no_visible_threads", "当前接管的 VS Code 实例还没有已知会话。请先在 VS Code 里实际操作一次会话，再重试。")
		}
		if workspaceKey := s.threadSelectionWorkspaceScope(surface); workspaceKey != "" {
			return nil, notice(surface, "no_visible_threads", fmt.Sprintf("当前工作区 %s 还没有可恢复会话。你可以直接发送文本开启新会话（或 /new 先进入待命），发送 /useall 查看其他 workspace 的会话，或先 /list 切换工作区。", workspaceKey))
		}
		return nil, notice(surface, "no_visible_threads", "当前还没有可恢复会话。")
	}
	return model, nil
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
	if s.surfaceIsVSCode(surface) && strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return vscodeThreadSelectionDropdownLabel(view)
	}
	return threadSelectionButtonLabel(view.Thread)
}

func vscodeThreadSelectionDropdownLabel(view *mergedThreadView) string {
	if view == nil {
		return ""
	}
	return displayThreadTitle(view.Inst, view.Thread)
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

func (s *Service) TryAutoResumeHeadlessSurface(surfaceID string, attempt SurfaceResumeAttempt, allowMissingTargetFailure bool) ([]eventcontract.Event, SurfaceResumeResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if !s.surfaceIsHeadless(surface) {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if surface.Abandoning || strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}

	failureCode := ""
	threadID := strings.TrimSpace(attempt.ThreadID)
	prepareNewThread := attempt.PrepareNewThread
	targetBackend := s.surfaceBackend(surface)
	if strings.TrimSpace(string(attempt.Backend)) != "" {
		targetBackend = state.NormalizeHeadlessBackend(attempt.Backend)
	}
	if threadID != "" {
		view := s.mergedThreadViewForBackend(surface, threadID, targetBackend, true)
		if inst, code := s.resolveSurfaceResumeVisibleInstance(surface, view, strings.TrimSpace(attempt.InstanceID), targetBackend); inst != nil {
			events := s.attachSurfaceToKnownThread(surface, inst, view, attachSurfaceToKnownThreadSurfaceResume)
			if failureCode := surfaceResumeAttachFailureCode(events); failureCode != "" {
				return events, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: failureCode}
			}
			return events, SurfaceResumeResult{Status: SurfaceResumeStatusThreadAttached}
		} else if code != "" {
			failureCode = code
		}
		if attempt.ResumeHeadless {
			events, result := s.tryAutoResumeManagedHeadlessTarget(surface, attempt, allowMissingTargetFailure)
			switch result.Status {
			case SurfaceResumeStatusThreadAttached, SurfaceResumeStatusStarting, SurfaceResumeStatusWaiting, SurfaceResumeStatusFailed:
				return events, result
			}
		}
		if !allowMissingTargetFailure {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
		}
	}

	workspaceKey := normalizeWorkspaceClaimKey(attempt.WorkspaceKey)
	if workspaceKey != "" {
		resolution := s.resolveWorkspaceContract(surface, workspaceKey, targetBackend)
		switch resolution.Mode {
		case contractResolutionAttachVisible, contractResolutionReuseManaged:
			options := attachWorkspaceOptions{ResumeNotice: !prepareNewThread, PrepareNewThread: prepareNewThread}
			return s.attachWorkspaceWithOptions(surface, workspaceKey, options), SurfaceResumeResult{Status: SurfaceResumeStatusWorkspaceAttached}
		case contractResolutionRestartManaged, contractResolutionCreateHeadless:
			if !allowMissingTargetFailure {
				return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
			}
			workspacePrepareNewThread := prepareNewThread || threadID != ""
			continuation := s.buildHeadlessWorkspaceContinuation(surface, workspaceKey, targetBackend, workspacePrepareNewThread)
			if attempt.ReserveRoomSlot {
				if ok, events := s.reserveFeishuRoomGroupOnDemandSlot(surface); !ok {
					return events, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "room_workspace_active"}
				}
			}
			events := s.executeResolvedWorkspaceContinuation(surface, continuation, resolution, attachWorkspaceOptions{
				ResumeNotice:     !prepareNewThread,
				PrepareNewThread: workspacePrepareNewThread,
			})
			if attempt.ReserveRoomSlot && surface.PendingHeadless == nil {
				s.releaseFeishuRoomActiveReservationByReason(surface, feishuRoomGroupOnDemandReservationReason)
			}
			return events, SurfaceResumeResult{Status: SurfaceResumeStatusStarting}
		case contractResolutionUnavailable:
			code := xutil.FirstNonEmpty(strings.TrimSpace(resolution.NoticeCode), "workspace_instance_busy")
			if code == "workspace_not_found" && !allowMissingTargetFailure {
				return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
			}
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: code}
		}
	}

	if failureCode == "" {
		failureCode = "thread_not_found"
	}
	if !allowMissingTargetFailure && failureCode == "thread_not_found" {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
	}
	return nil, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: failureCode}
}

func surfaceResumeAttachFailureCode(events []eventcontract.Event) string {
	for _, event := range events {
		if event.Notice == nil {
			continue
		}
		switch strings.TrimSpace(event.Notice.Code) {
		case "surface_resume_workspace_busy":
			return "workspace_busy"
		case "surface_resume_workspace_instance_busy":
			return "workspace_instance_busy"
		case "surface_resume_thread_busy":
			return "thread_busy"
		case "surface_resume_target_not_found":
			return "thread_not_found"
		}
	}
	return ""
}

func (s *Service) tryAutoResumeManagedHeadlessTarget(surface *state.SurfaceConsoleRecord, attempt SurfaceResumeAttempt, allowMissingTargetFailure bool) ([]eventcontract.Event, SurfaceResumeResult) {
	view := s.headlessRestoreView(surface, attempt)
	if view == nil {
		if !allowMissingTargetFailure {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
		}
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice("thread_not_found"),
		}}, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "thread_not_found"}
	}
	target := s.resolveHeadlessRestoreTargetFromView(surface, view)
	switch target.Mode {
	case threadAttachFreeVisible, threadAttachReuseHeadless:
		return s.attachSurfaceToKnownThread(surface, target.Instance, target.View, attachSurfaceToKnownThreadHeadlessRestore), SurfaceResumeResult{Status: SurfaceResumeStatusThreadAttached}
	case threadAttachCreateHeadless:
		if attempt.ReserveRoomSlot {
			if ok, events := s.reserveFeishuRoomGroupOnDemandSlot(surface); !ok {
				return events, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: "room_workspace_active"}
			}
		}
		events := s.startHeadlessForResolvedThreadWithMode(surface, target.View, startHeadlessModeHeadlessRestore)
		if attempt.ReserveRoomSlot && surface.PendingHeadless == nil {
			s.releaseFeishuRoomActiveReservationByReason(surface, feishuRoomGroupOnDemandReservationReason)
		}
		return events, SurfaceResumeResult{Status: SurfaceResumeStatusStarting}
	case threadAttachUnavailable:
		if target.NoticeCode == "thread_not_found" && !allowMissingTargetFailure {
			return nil, SurfaceResumeResult{Status: SurfaceResumeStatusWaiting}
		}
		failureCode := xutil.FirstNonEmpty(strings.TrimSpace(target.NoticeCode), "thread_not_found")
		return []eventcontract.Event{{
			Kind:             eventcontract.KindNotice,
			SurfaceSessionID: surface.SurfaceSessionID,
			Notice:           headlessRestoreFailureNotice(failureCode),
		}}, SurfaceResumeResult{Status: SurfaceResumeStatusFailed, FailureCode: failureCode}
	default:
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
}

func (s *Service) TryAutoResumeVSCodeSurface(surfaceID, instanceID string) ([]eventcontract.Event, SurfaceResumeResult) {
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if !s.surfaceIsVSCode(surface) {
		return nil, SurfaceResumeResult{Status: SurfaceResumeStatusSkipped}
	}
	if surface.Abandoning || strings.TrimSpace(surface.AttachedInstanceID) != "" || surface.PendingHeadless != nil {
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

func (s *Service) headlessRestoreView(surface *state.SurfaceConsoleRecord, attempt SurfaceResumeAttempt) *mergedThreadView {
	threadID := strings.TrimSpace(attempt.ThreadID)
	if threadID == "" {
		return nil
	}
	attemptWorkspaceKey := state.ResolveHeadlessResumeWorkspaceKey(attempt.WorkspaceKey, attempt.ThreadCWD)
	backend := agentproto.NormalizeBackend(attempt.Backend)
	if strings.TrimSpace(string(backend)) == "" && surface != nil {
		backend = s.surfaceBackend(surface)
	}
	view := s.mergedThreadViewForBackend(surface, threadID, backend, true)
	if view == nil {
		return s.syntheticHeadlessRestoreView(threadID, attempt.ThreadTitle, attemptWorkspaceKey, attempt.ThreadCWD, backend)
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
	if strings.TrimSpace(thread.WorkspaceKey) == "" {
		thread.WorkspaceKey = attemptWorkspaceKey
	}
	if strings.TrimSpace(thread.CWD) == "" {
		thread.CWD = strings.TrimSpace(xutil.FirstNonEmpty(attempt.ThreadCWD, attemptWorkspaceKey))
	}
	cloned.Thread = thread
	return &cloned
}

func (s *Service) syntheticHeadlessRestoreView(threadID, threadTitle, workspaceKey, threadCWD string, backend agentproto.Backend) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	threadCWD = strings.TrimSpace(threadCWD)
	threadTitle = strings.TrimSpace(threadTitle)
	backend = agentproto.NormalizeBackend(backend)
	if threadID == "" || (workspaceKey == "" && threadCWD == "") {
		return nil
	}
	view := &mergedThreadView{
		ThreadID: threadID,
		Backend:  backend,
		Thread: &state.ThreadRecord{
			ThreadID:     threadID,
			Name:         threadTitle,
			WorkspaceKey: xutil.FirstNonEmpty(workspaceKey, threadCWD),
			CWD:          xutil.FirstNonEmpty(threadCWD, workspaceKey),
			Loaded:       true,
		},
	}
	if owner := s.threadClaimSurface(threadID); owner != nil {
		view.BusyOwner = owner
	}
	return view
}

func headlessRestoreFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "profile_definition_incomplete":
		return &control.Notice{
			Code:  "profile_definition_incomplete",
			Title: "Codex Profile 不可用",
			Text:  "当前 Codex Profile 缺少必需的模型或推理配置，请先完成配置后再恢复会话。",
		}
	case "profile_secret_missing":
		return &control.Notice{
			Code:  "profile_secret_missing",
			Title: "Codex Profile 不可用",
			Text:  "当前 Codex Profile 缺少可用的 API Key，请先更新 Profile 后再恢复会话。",
		}
	case "oauth_missing":
		return &control.Notice{
			Code:  "oauth_missing",
			Title: "Codex 登录不可用",
			Text:  "本机 Codex 当前没有可用的 ChatGPT 登录，请先重新登录后再恢复会话。",
		}
	case "oauth_probe_unknown":
		return &control.Notice{
			Code:  "oauth_probe_unknown",
			Title: "Codex 登录状态未知",
			Text:  "暂时无法确认本机 Codex 的 ChatGPT 登录状态，请刷新状态后再试。",
		}
	case "oauth_deployment_unsupported":
		return &control.Notice{
			Code:  "oauth_deployment_unsupported",
			Title: "Codex 登录部署暂不支持",
			Text:  "当前 ChatGPT 登录使用了暂不支持的自定义部署，请改用本机默认或官方部署。",
		}
	case "codex_capability_unsupported":
		return &control.Notice{
			Code:  "codex_capability_unsupported",
			Title: "Codex 版本不兼容",
			Text:  "当前 Codex 版本不支持所需的 Profile 隔离能力，请先升级 Codex。",
		}
	case "codex_binary_unavailable":
		return &control.Notice{
			Code:  "codex_binary_unavailable",
			Title: "Codex 运行环境不可用",
			Text:  "找不到可用的 Codex 可执行文件，或 Codex 启动失败，请检查 Codex 安装与运行环境后重试。",
		}
	case "codex_probe_timeout":
		return &control.Notice{
			Code:  "codex_probe_timeout",
			Title: "Codex 探测超时",
			Text:  "Codex 能力探测超时，请稍后重试或重启服务。",
		}
	case "codex_probe_unavailable":
		return &control.Notice{
			Code:  "codex_probe_unavailable",
			Title: "Codex 探测暂不可用",
			Text:  "暂时无法完成 Codex 能力探测，请稍后重试。",
		}
	case "codex_probe_contract_mismatch":
		return &control.Notice{
			Code:  "codex_probe_contract_mismatch",
			Title: "Codex 协议契约不匹配",
			Text:  "Codex app-server 返回的协议契约与预期不一致，暂时无法确认兼容性。",
		}
	case "managed_model_catalog_missing":
		return &control.Notice{
			Code:  "managed_model_catalog_missing",
			Title: "Codex Profile 不可用",
			Text:  "当前运行目录无法准备 Codex 模型目录，请检查服务安装状态后再恢复会话。",
		}
	case "profile_revision_unavailable":
		return &control.Notice{
			Code:  "profile_revision_unavailable",
			Title: "Codex Profile 已变化",
			Text:  "这个任务引用的 Codex Profile 版本已经不可用，请重新选择 Profile。",
		}
	case "headless_restore_profile_unavailable":
		return &control.Notice{
			Code:  "headless_restore_profile_unavailable",
			Title: "恢复失败",
			Text:  "当前 Codex Profile 配置不可用，暂时无法恢复之前会话。请检查 Profile 设置后重试。",
		}
	case "headless_restore_claude_profile_unavailable":
		return &control.Notice{
			Code:  "headless_restore_claude_profile_unavailable",
			Title: "恢复失败",
			Text:  "当前 Claude 配置不可用，暂时无法恢复之前会话。请检查 Claude 设置后重试。",
		}
	case "headless_restore_runtime_unavailable":
		return &control.Notice{
			Code:  "headless_restore_runtime_unavailable",
			Title: "恢复失败",
			Text:  "当前恢复环境未准备好，暂时无法恢复之前会话。请检查本地配置后重试。",
		}
	case "headless_restore_workspace_missing":
		return &control.Notice{
			Code:  "headless_restore_workspace_missing",
			Title: "恢复失败",
			Text:  "之前会话的工作目录已经不存在，无法自动恢复。请发送 /list 重新选择工作区，或新建一个会话。",
		}
	case "workspace_busy", "headless_restore_workspace_busy":
		return genericHeadlessRestoreFailureNotice("headless_restore_workspace_busy")
	case "thread_busy", "headless_restore_thread_busy":
		return genericHeadlessRestoreFailureNotice("headless_restore_thread_busy")
	case "thread_cwd_missing", "headless_restore_thread_cwd_missing":
		return &control.Notice{
			Code:  "headless_restore_thread_cwd_missing",
			Title: "恢复失败",
			Text:  "之前的会话缺少可恢复的工作目录，暂时无法自动恢复，请稍后重试或尝试其他会话。",
		}
	case "thread_not_found", "headless_restore_thread_not_found":
		return &control.Notice{
			Code:  "headless_restore_thread_not_found",
			Title: "恢复失败",
			Text:  "暂时无法找到之前会话，请稍后重试或尝试其他会话。",
		}
	default:
		return &control.Notice{
			Code:  "headless_restore_start_failed",
			Title: "恢复失败",
			Text:  "之前的会话暂时无法恢复，请检查错误原因后重试或尝试其他会话。",
		}
	}
}

func NoticeForHeadlessRestoreFailure(code string) *control.Notice {
	return headlessRestoreFailureNotice(code)
}

func HeadlessRestoreLaunchFailureCode(err error) string {
	problem := agentproto.ErrorInfoFromError(err, agentproto.ErrorInfo{})
	switch strings.TrimSpace(problem.Code) {
	case "profile_definition_incomplete",
		"profile_secret_missing",
		"oauth_missing",
		"oauth_probe_unknown",
		"oauth_deployment_unsupported",
		"codex_capability_unsupported",
		"codex_binary_unavailable",
		"codex_probe_timeout",
		"codex_probe_unavailable",
		"codex_probe_contract_mismatch",
		"managed_model_catalog_missing",
		"profile_revision_unavailable":
		return strings.TrimSpace(problem.Code)
	case "codex_profile_prepare_failed":
		return "headless_restore_profile_unavailable"
	case "claude_profile_prepare_failed", "claude_settings_prepare_failed":
		return "headless_restore_claude_profile_unavailable"
	case "headless_binary_missing", "headless_backend_missing":
		return "headless_restore_runtime_unavailable"
	case "headless_workspace_missing":
		return "headless_restore_workspace_missing"
	default:
		return "headless_restore_start_failed"
	}
}

func surfaceResumeFailureNotice(code string) *control.Notice {
	switch strings.TrimSpace(code) {
	case "workspace_busy":
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_workspace_busy", "恢复失败", "暂时无法恢复到之前会话。请稍后重试，或发送 /list 重新选择工作区。")
		return &notice
	case "workspace_instance_busy":
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_workspace_instance_busy", "恢复失败", "暂时无法恢复到之前会话。请稍后重试，或发送 /list 重新选择工作区。")
		return &notice
	case "thread_busy":
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_thread_busy", "恢复失败", "暂时无法恢复到之前会话。请稍后重试，或发送 /use 选择其他会话。")
		return &notice
	default:
		notice := globalRuntimeNotice(control.NoticeDeliveryFamilySurfaceResume, "surface_resume_target_not_found", "恢复失败", "暂时无法恢复到之前会话。请稍后重试，或发送 /list 重新选择工作区。")
		return &notice
	}
}

func genericHeadlessRestoreFailureNotice(code string) *control.Notice {
	return &control.Notice{
		Code:  strings.TrimSpace(code),
		Title: "恢复失败",
		Text:  "之前的会话暂时无法恢复，请稍后重试或尝试其他会话。",
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
