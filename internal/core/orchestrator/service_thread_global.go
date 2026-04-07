package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type threadAttachMode string

const (
	threadAttachUnavailable   threadAttachMode = "unavailable"
	threadAttachCurrentVisible threadAttachMode = "current_visible"
	threadAttachFreeVisible    threadAttachMode = "free_visible"
	threadAttachReuseHeadless  threadAttachMode = "reuse_headless"
	threadAttachCreateHeadless threadAttachMode = "create_headless"
)

type mergedThreadView struct {
	ThreadID string
	Inst     *state.InstanceRecord
	Thread   *state.ThreadRecord

	CurrentVisible  bool
	FreeVisibleInst *state.InstanceRecord
	AnyVisibleInst  *state.InstanceRecord
	BusyOwner       *state.SurfaceConsoleRecord
}

type resolvedThreadTarget struct {
	Mode       threadAttachMode
	View       *mergedThreadView
	Instance   *state.InstanceRecord
	NoticeCode string
	NoticeText string
}

func (s *Service) mergedThreadViews(surface *state.SurfaceConsoleRecord) []*mergedThreadView {
	viewsByID := map[string]*mergedThreadView{}
	instances := s.Instances()
	currentInstanceID := ""
	if surface != nil {
		currentInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		owner := s.instanceClaimSurface(inst.InstanceID)
		for _, thread := range visibleThreads(inst) {
			if thread == nil {
				continue
			}
			view := viewsByID[thread.ThreadID]
			if view == nil {
				view = &mergedThreadView{ThreadID: thread.ThreadID}
				viewsByID[thread.ThreadID] = view
			}
			if shouldPromoteMergedThread(view.Inst, view.Thread, inst, thread) {
				view.Inst = inst
				view.Thread = thread
			}
			if inst.InstanceID == currentInstanceID {
				view.CurrentVisible = true
			}
			if inst.Online && view.AnyVisibleInst == nil {
				view.AnyVisibleInst = inst
			}
			if inst.Online && view.FreeVisibleInst == nil && (owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID)) {
				view.FreeVisibleInst = inst
			}
		}
	}

	views := make([]*mergedThreadView, 0, len(viewsByID))
	for _, view := range viewsByID {
		if view == nil {
			continue
		}
		if owner := s.threadClaimSurface(view.ThreadID); owner != nil && (surface == nil || owner.SurfaceSessionID != surface.SurfaceSessionID) {
			view.BusyOwner = owner
		}
		views = append(views, view)
	}
	sortMergedThreadViews(views)
	return views
}

func (s *Service) mergedThreadView(surface *state.SurfaceConsoleRecord, threadID string) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	for _, view := range s.mergedThreadViews(surface) {
		if view != nil && view.ThreadID == threadID {
			return view
		}
	}
	return nil
}

func shouldPromoteMergedThread(currentInst *state.InstanceRecord, currentThread *state.ThreadRecord, nextInst *state.InstanceRecord, nextThread *state.ThreadRecord) bool {
	if nextThread == nil {
		return false
	}
	if currentThread == nil {
		return true
	}
	switch {
	case nextThread.LastUsedAt.After(currentThread.LastUsedAt):
		return true
	case currentThread.LastUsedAt.After(nextThread.LastUsedAt):
		return false
	}
	currentScore := mergedThreadMetadataScore(currentThread)
	nextScore := mergedThreadMetadataScore(nextThread)
	switch {
	case nextScore > currentScore:
		return true
	case currentScore > nextScore:
		return false
	}
	switch {
	case nextInst != nil && nextInst.Online && (currentInst == nil || !currentInst.Online):
		return true
	case currentInst != nil && currentInst.Online && (nextInst == nil || !nextInst.Online):
		return false
	}
	if currentInst == nil {
		return true
	}
	if nextInst == nil {
		return false
	}
	return nextInst.InstanceID < currentInst.InstanceID
}

func mergedThreadMetadataScore(thread *state.ThreadRecord) int {
	if thread == nil {
		return 0
	}
	score := 0
	if strings.TrimSpace(thread.Name) != "" {
		score++
	}
	if strings.TrimSpace(thread.Preview) != "" {
		score++
	}
	if strings.TrimSpace(thread.CWD) != "" {
		score++
	}
	if thread.Loaded {
		score++
	}
	return score
}

func sortMergedThreadViews(views []*mergedThreadView) {
	sort.SliceStable(views, func(i, j int) bool {
		left := views[i]
		right := views[j]
		var leftTime, rightTime time.Time
		var leftOrder, rightOrder int
		if left != nil && left.Thread != nil {
			leftTime = left.Thread.LastUsedAt
			leftOrder = left.Thread.ListOrder
		}
		if right != nil && right.Thread != nil {
			rightTime = right.Thread.LastUsedAt
			rightOrder = right.Thread.ListOrder
		}
		switch {
		case left == nil:
			return false
		case right == nil:
			return true
		case !leftTime.Equal(rightTime):
			return leftTime.After(rightTime)
		case leftOrder == 0 && rightOrder != 0:
			return false
		case leftOrder != 0 && rightOrder == 0:
			return true
		case leftOrder != rightOrder:
			return leftOrder < rightOrder
		default:
			return left.ThreadID < right.ThreadID
		}
	})
}

func (s *Service) reusableManagedHeadless(surface *state.SurfaceConsoleRecord, cwd string) *state.InstanceRecord {
	var candidates []*state.InstanceRecord
	for _, inst := range s.Instances() {
		if inst == nil || !inst.Online || !isHeadlessInstance(inst) {
			continue
		}
		if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && (surface == nil || owner.SurfaceSessionID != surface.SurfaceSessionID) {
			continue
		}
		candidates = append(candidates, inst)
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := reusableHeadlessScore(surface, candidates[i], cwd)
		right := reusableHeadlessScore(surface, candidates[j], cwd)
		if left != right {
			return left > right
		}
		return candidates[i].InstanceID < candidates[j].InstanceID
	})
	return candidates[0]
}

func reusableHeadlessScore(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, cwd string) int {
	if inst == nil {
		return 0
	}
	score := workspaceAffinityScore(inst.WorkspaceRoot, cwd)
	if surface != nil && inst.InstanceID == strings.TrimSpace(surface.AttachedInstanceID) {
		score += 4
	}
	if inst.ActiveTurnID == "" {
		score += 2
	}
	return score
}

func workspaceAffinityScore(root, cwd string) int {
	root = strings.TrimSpace(root)
	cwd = strings.TrimSpace(cwd)
	if root == "" || cwd == "" {
		return 0
	}
	switch {
	case root == cwd:
		return 3
	case strings.HasPrefix(cwd, root+"/"), strings.HasPrefix(root, cwd+"/"):
		return 2
	default:
		return 1
	}
}

func (s *Service) resolveThreadTarget(surface *state.SurfaceConsoleRecord, threadID string) resolvedThreadTarget {
	view := s.mergedThreadView(surface, threadID)
	if view == nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "thread_not_found",
			NoticeText: "目标会话不存在或当前不可见。",
		}
	}
	return s.resolveThreadTargetFromView(surface, view)
}

func (s *Service) resolveThreadTargetFromView(surface *state.SurfaceConsoleRecord, view *mergedThreadView) resolvedThreadTarget {
	if view == nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "thread_not_found",
			NoticeText: "目标会话不存在或当前不可见。",
		}
	}
	if view.CurrentVisible {
		return resolvedThreadTarget{
			Mode: threadAttachCurrentVisible,
			View: view,
		}
	}
	if view.BusyOwner != nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			View:       view,
			NoticeCode: "thread_busy",
			NoticeText: "目标会话当前已被其他飞书会话占用。",
		}
	}
	if view.FreeVisibleInst != nil {
		return resolvedThreadTarget{
			Mode:     threadAttachFreeVisible,
			View:     view,
			Instance: view.FreeVisibleInst,
		}
	}
	if headless := s.reusableManagedHeadless(surface, threadCWD(view)); headless != nil && strings.TrimSpace(threadCWD(view)) != "" {
		return resolvedThreadTarget{
			Mode:     threadAttachReuseHeadless,
			View:     view,
			Instance: headless,
		}
	}
	if strings.TrimSpace(threadCWD(view)) == "" {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			View:       view,
			NoticeCode: "thread_cwd_missing",
			NoticeText: "目标会话缺少可恢复的工作目录，当前无法直接接管。",
		}
	}
	return resolvedThreadTarget{
		Mode: threadAttachCreateHeadless,
		View: view,
	}
}

func threadCWD(view *mergedThreadView) string {
	if view == nil || view.Thread == nil {
		return ""
	}
	return strings.TrimSpace(view.Thread.CWD)
}

func (s *Service) mergedThreadStatus(surface *state.SurfaceConsoleRecord, view *mergedThreadView) (string, string, bool) {
	if view == nil {
		return "", "", true
	}
	if surface != nil && surface.SelectedThreadID == view.ThreadID && s.surfaceOwnsThread(surface, view.ThreadID) {
		if surface.RouteMode == state.RouteModeFollowLocal {
			return "当前跟随", "", false
		}
		return "当前会话", "", false
	}
	if view.BusyOwner != nil {
		return "已被其他飞书会话占用", "已占用", true
	}
	target := s.resolveThreadTargetFromView(surface, view)
	switch target.Mode {
	case threadAttachCurrentVisible:
		return "可切换", "切换", false
	case threadAttachFreeVisible:
		return "可接管已在线实例", "接管", false
	case threadAttachReuseHeadless:
		return "将复用空闲 headless", "恢复", false
	case threadAttachCreateHeadless:
		return "将启动新的 headless", "启动", false
	default:
		if target.NoticeText != "" {
			return target.NoticeText, "不可用", true
		}
		return "当前不可接管", "不可用", true
	}
}

func (s *Service) mergedThreadSubtitle(surface *state.SurfaceConsoleRecord, view *mergedThreadView) string {
	if view == nil {
		return ""
	}
	subtitle := threadSelectionSubtitle(view.Thread, view.ThreadID)
	status, _, _ := s.mergedThreadStatus(surface, view)
	if status == "" {
		return subtitle
	}
	if subtitle == "" {
		return status
	}
	return subtitle + "\n" + status
}
