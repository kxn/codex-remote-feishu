package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type threadAttachMode string

const (
	threadAttachUnavailable    threadAttachMode = "unavailable"
	threadAttachCurrentVisible threadAttachMode = "current_visible"
	threadAttachFreeVisible    threadAttachMode = "free_visible"
	threadAttachReuseHeadless  threadAttachMode = "reuse_headless"
	threadAttachCreateHeadless threadAttachMode = "create_headless"
)

const persistedRecentThreadLimit = 200

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
	s.mergePersistedRecentThreads(viewsByID)

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
	return s.persistedThreadView(surface, threadID)
}

func (s *Service) mergePersistedRecentThreads(viewsByID map[string]*mergedThreadView) {
	if s == nil || s.persistedThreads == nil {
		return
	}
	threads, err := s.persistedThreads.RecentThreads(persistedRecentThreadLimit)
	if err != nil {
		return
	}
	for i := range threads {
		thread := threads[i]
		if strings.TrimSpace(thread.ThreadID) == "" || !threadVisible(&thread) {
			continue
		}
		view := viewsByID[thread.ThreadID]
		if view == nil {
			viewsByID[thread.ThreadID] = &mergedThreadView{
				ThreadID: thread.ThreadID,
				Inst:     syntheticPersistedThreadInstance(&thread),
				Thread:   cloneThreadRecord(&thread),
			}
			continue
		}
		view.Thread = mergeThreadMetadata(view.Thread, &thread)
	}
}

func (s *Service) persistedThreadView(surface *state.SurfaceConsoleRecord, threadID string) *mergedThreadView {
	if s == nil || s.persistedThreads == nil {
		return nil
	}
	thread, err := s.persistedThreads.ThreadByID(strings.TrimSpace(threadID))
	if err != nil || !threadVisible(thread) {
		return nil
	}
	view := &mergedThreadView{
		ThreadID: thread.ThreadID,
		Inst:     syntheticPersistedThreadInstance(thread),
		Thread:   cloneThreadRecord(thread),
	}
	if owner := s.threadClaimSurface(view.ThreadID); owner != nil && (surface == nil || owner.SurfaceSessionID != surface.SurfaceSessionID) {
		view.BusyOwner = owner
	}
	return view
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

func shouldPromoteMergedThreadMetadata(currentThread, nextThread *state.ThreadRecord) bool {
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
	return mergedThreadMetadataScore(nextThread) > mergedThreadMetadataScore(currentThread)
}

func mergeThreadMetadata(currentThread, nextThread *state.ThreadRecord) *state.ThreadRecord {
	if currentThread == nil {
		return cloneThreadRecord(nextThread)
	}
	if nextThread == nil {
		return cloneThreadRecord(currentThread)
	}
	primary := currentThread
	secondary := nextThread
	if shouldPromoteMergedThreadMetadata(currentThread, nextThread) {
		primary = nextThread
		secondary = currentThread
	}
	merged := cloneThreadRecord(primary)
	if merged == nil {
		return cloneThreadRecord(secondary)
	}
	if strings.TrimSpace(merged.Name) == "" {
		merged.Name = strings.TrimSpace(secondary.Name)
	}
	if strings.TrimSpace(merged.Preview) == "" {
		merged.Preview = strings.TrimSpace(secondary.Preview)
	}
	if strings.TrimSpace(merged.CWD) == "" {
		merged.CWD = strings.TrimSpace(secondary.CWD)
	}
	if strings.TrimSpace(merged.State) == "" {
		merged.State = strings.TrimSpace(secondary.State)
	}
	if strings.TrimSpace(merged.ExplicitModel) == "" {
		merged.ExplicitModel = strings.TrimSpace(secondary.ExplicitModel)
	}
	if strings.TrimSpace(merged.ExplicitReasoningEffort) == "" {
		merged.ExplicitReasoningEffort = strings.TrimSpace(secondary.ExplicitReasoningEffort)
	}
	if !merged.Loaded {
		merged.Loaded = secondary.Loaded
	}
	if merged.LastUsedAt.IsZero() {
		merged.LastUsedAt = secondary.LastUsedAt
	}
	if merged.ListOrder == 0 {
		merged.ListOrder = secondary.ListOrder
	}
	if merged.TrafficClass == "" {
		merged.TrafficClass = secondary.TrafficClass
	}
	if merged.UndeliveredReplay == nil && secondary.UndeliveredReplay != nil {
		replayCopy := *secondary.UndeliveredReplay
		merged.UndeliveredReplay = &replayCopy
	}
	merged.Archived = merged.Archived || secondary.Archived
	return merged
}

func cloneThreadRecord(thread *state.ThreadRecord) *state.ThreadRecord {
	if thread == nil {
		return nil
	}
	threadCopy := *thread
	if thread.UndeliveredReplay != nil {
		replayCopy := *thread.UndeliveredReplay
		threadCopy.UndeliveredReplay = &replayCopy
	}
	return &threadCopy
}

func syntheticPersistedThreadInstance(thread *state.ThreadRecord) *state.InstanceRecord {
	if thread == nil {
		return nil
	}
	cwd := state.NormalizeWorkspaceKey(thread.CWD)
	if cwd == "" {
		return nil
	}
	return &state.InstanceRecord{
		WorkspaceRoot: cwd,
		WorkspaceKey:  state.ResolveWorkspaceKey(cwd),
		ShortName:     state.WorkspaceShortName(cwd),
	}
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
	root = state.NormalizeWorkspaceKey(root)
	cwd = state.NormalizeWorkspaceKey(cwd)
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
	return s.resolveThreadTargetWithScope(surface, threadID, false)
}

func (s *Service) resolveThreadTargetWithScope(surface *state.SurfaceConsoleRecord, threadID string, allowCrossWorkspace bool) resolvedThreadTarget {
	if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return s.resolveAttachedVSCodeThreadTarget(surface, threadID)
	}
	view := s.mergedThreadView(surface, threadID)
	if view == nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "thread_not_found",
			NoticeText: "目标会话不存在或当前不可见。",
		}
	}
	if !allowCrossWorkspace && !s.threadViewSelectableInCurrentScope(surface, view) {
		return s.threadOutsideCurrentWorkspaceTarget(surface)
	}
	return s.resolveThreadTargetFromView(surface, view)
}

func (s *Service) resolveAttachedVSCodeThreadTarget(surface *state.SurfaceConsoleRecord, threadID string) resolvedThreadTarget {
	if surface == nil || strings.TrimSpace(surface.AttachedInstanceID) == "" {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "not_attached_vscode",
			NoticeText: "vscode 模式下请先 /list 选择一个 VS Code 实例，再使用 /use 或 /useall。",
		}
	}
	if view := s.currentInstanceThreadView(surface, threadID); view != nil {
		return resolvedThreadTarget{
			Mode:     threadAttachCurrentVisible,
			View:     view,
			Instance: view.Inst,
		}
	}
	if s.mergedThreadView(surface, threadID) != nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "thread_outside_instance",
			NoticeText: "vscode 模式下只能选择当前接管实例的已知会话；如需其他实例，请先 /list 切换。",
		}
	}
	return resolvedThreadTarget{
		Mode:       threadAttachUnavailable,
		NoticeCode: "thread_not_found",
		NoticeText: "目标会话不存在或当前实例尚未观测到它。",
	}
}

func (s *Service) resolveThreadTargetFromView(surface *state.SurfaceConsoleRecord, view *mergedThreadView) resolvedThreadTarget {
	if view == nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "thread_not_found",
			NoticeText: "目标会话不存在或当前不可见。",
		}
	}
	if owner := s.workspaceBusyOwnerForView(surface, view); owner != nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			View:       view,
			NoticeCode: "workspace_busy",
			NoticeText: "目标会话所在 workspace 当前已被其他飞书会话占用。",
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

func (s *Service) resolveHeadlessRestoreTargetFromView(surface *state.SurfaceConsoleRecord, view *mergedThreadView) resolvedThreadTarget {
	if view == nil {
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			NoticeCode: "thread_not_found",
			NoticeText: "目标会话不存在或当前不可见。",
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
	if view.FreeVisibleInst != nil && isHeadlessInstance(view.FreeVisibleInst) {
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

func (s *Service) resolveSurfaceResumeVisibleInstance(surface *state.SurfaceConsoleRecord, view *mergedThreadView, preferredInstanceID string) (*state.InstanceRecord, string) {
	if view == nil {
		return nil, "thread_not_found"
	}
	preferredInstanceID = strings.TrimSpace(preferredInstanceID)
	if preferredInstanceID != "" {
		if inst := s.root.Instances[preferredInstanceID]; inst != nil && inst.Online && threadVisible(inst.Threads[view.ThreadID]) {
			if owner := s.threadClaimSurface(view.ThreadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
				return nil, "thread_busy"
			}
			if owner := s.instanceClaimSurface(inst.InstanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
				return nil, "workspace_instance_busy"
			}
			return inst, ""
		}
	}
	if view.BusyOwner != nil {
		return nil, "thread_busy"
	}
	if view.FreeVisibleInst != nil {
		return view.FreeVisibleInst, ""
	}
	if view.AnyVisibleInst != nil {
		return nil, "workspace_instance_busy"
	}
	return nil, "thread_not_found"
}

func threadCWD(view *mergedThreadView) string {
	if view == nil || view.Thread == nil {
		return ""
	}
	return strings.TrimSpace(view.Thread.CWD)
}

func (s *Service) threadSelectionWorkspaceScope(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || s.normalizeSurfaceProductMode(surface) != state.ProductModeNormal {
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
	for _, thread := range visibleThreads(inst) {
		if thread == nil {
			continue
		}
		view := &mergedThreadView{
			ThreadID:       thread.ThreadID,
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
	if surface != nil &&
		s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode &&
		strings.TrimSpace(surface.AttachedInstanceID) != "" {
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
			return "当前跟随", false
		}
		return "当前会话", false
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
			s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal &&
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
