package orchestrator

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) threadSelectionWorkspaceScope(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || !s.surfaceIsHeadless(surface) {
		return ""
	}
	if strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return ""
	}
	return s.surfaceCurrentWorkspaceKey(surface)
}

func (s *Service) threadViewSelectableInCurrentScope(surface *state.SurfaceConsoleRecord, view *mergedThreadView) bool {
	workspaceKey := s.threadSelectionWorkspaceScope(surface)
	if workspaceKey == "" {
		return true
	}
	return mergedThreadWorkspaceClaimKey(view) == workspaceKey
}

func (s *Service) currentInstanceThreadViews(surface *state.SurfaceConsoleRecord) []*mergedThreadView {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return nil
	}
	owner := s.instanceClaimSurface(inst.InstanceID)
	views := make([]*mergedThreadView, 0, len(inst.Threads))
	for _, thread := range ordinaryVisibleThreads(inst) {
		if thread == nil {
			continue
		}
		view := &mergedThreadView{
			ThreadID:       thread.ThreadID,
			Backend:        state.EffectiveInstanceBackend(inst),
			Inst:           inst,
			Thread:         thread,
			CurrentVisible: true,
		}
		if inst.Online {
			view.AnyVisibleInst = inst
			if owner == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
				view.FreeVisibleInst = inst
			}
		}
		if busyOwner := s.threadClaimSurface(thread.ThreadID); busyOwner != nil && busyOwner.SurfaceSessionID != surface.SurfaceSessionID {
			view.BusyOwner = busyOwner
		}
		views = append(views, view)
	}
	sortMergedThreadViews(views)
	return views
}

func (s *Service) currentInstanceThreadView(surface *state.SurfaceConsoleRecord, threadID string) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	for _, view := range s.currentInstanceThreadViews(surface) {
		if view != nil && view.ThreadID == threadID {
			return view
		}
	}
	return nil
}

func (s *Service) scopedMergedThreadViews(surface *state.SurfaceConsoleRecord) []*mergedThreadView {
	if s.surfaceIsVSCode(surface) && strings.TrimSpace(surface.AttachedInstanceID) != "" {
		return s.currentInstanceThreadViews(surface)
	}
	views := s.mergedThreadViews(surface)
	workspaceKey := s.threadSelectionWorkspaceScope(surface)
	if workspaceKey == "" {
		return views
	}
	scoped := make([]*mergedThreadView, 0, len(views))
	for _, view := range views {
		if s.threadViewSelectableInCurrentScope(surface, view) {
			scoped = append(scoped, view)
		}
	}
	return scoped
}

func (s *Service) threadOutsideCurrentWorkspaceTarget(surface *state.SurfaceConsoleRecord) resolvedThreadTarget {
	workspaceKey := s.threadSelectionWorkspaceScope(surface)
	text := "当前已接管其他工作区；请先发送 /list 切换工作区，再选择这个会话。"
	if workspaceKey != "" {
		text = "当前已接管工作区 " + workspaceKey + "。请先发送 /list 切换工作区，再选择这个会话。"
	}
	return resolvedThreadTarget{
		Mode:       threadAttachUnavailable,
		NoticeCode: "thread_outside_workspace",
		NoticeText: text,
	}
}

func (s *Service) threadSelectionStatus(surface *state.SurfaceConsoleRecord, view *mergedThreadView, allowCrossWorkspace bool) (string, bool) {
	if view == nil {
		return "", true
	}
	if surface != nil && surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID) {
		if surface.RouteMode == state.RouteModeFollowLocal {
			return "当前跟随中", false
		}
		return "已接管", false
	}
	if owner := s.workspaceBusyOwnerForView(surface, view); owner != nil {
		return "所在 workspace 已被其他飞书会话接管", true
	}
	if view.BusyOwner != nil {
		return "已被其他飞书会话接管", true
	}
	target := s.resolveThreadTargetWithScope(surface, view.ThreadID, allowCrossWorkspace)
	switch target.Mode {
	case threadAttachCurrentVisible:
		return "可接管", false
	case threadAttachFreeVisible:
		if surface != nil &&
			s.surfaceIsHeadless(surface) &&
			strings.TrimSpace(surface.AttachedInstanceID) != "" &&
			target.Instance != nil &&
			isVSCodeInstance(target.Instance) {
			return "可接管，但被 VS Code 占用中，不建议", false
		}
		return "可接管", false
	case threadAttachReuseHeadless:
		return "可接管，将复用后台恢复", false
	case threadAttachCreateHeadless:
		return "可接管，将启动后台恢复", false
	default:
		if target.NoticeText != "" {
			return target.NoticeText, true
		}
		return "当前不可接管", true
	}
}

func (s *Service) threadSelectionOptionSubtitle(surface *state.SurfaceConsoleRecord, view *mergedThreadView, includeWorkspaceLine, allowCrossWorkspace bool) string {
	if view == nil {
		return ""
	}
	lines := []string{}
	if includeWorkspaceLine {
		if workspaceKey := mergedThreadWorkspaceClaimKey(view); workspaceKey != "" {
			lines = append(lines, workspaceKey)
		} else if fallback := threadSelectionSubtitle(view.Thread, view.ThreadID); fallback != "" {
			lines = append(lines, fallback)
		}
	}
	if status, _ := s.threadSelectionStatus(surface, view, allowCrossWorkspace); status != "" {
		lines = append(lines, status)
	}
	return strings.Join(lines, "\n")
}
