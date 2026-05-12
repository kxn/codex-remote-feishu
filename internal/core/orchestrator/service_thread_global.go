package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
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
	Backend  agentproto.Backend
	Inst     *state.InstanceRecord
	Thread   *state.ThreadRecord

	CurrentVisible            bool
	FreeVisibleInst           *state.InstanceRecord
	AnyVisibleInst            *state.InstanceRecord
	CompatibleFreeVisibleInst *state.InstanceRecord
	CompatibleAnyVisibleInst  *state.InstanceRecord
	BusyOwner                 *state.SurfaceConsoleRecord
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
	targetBackend, filterByBackend := s.normalModeThreadBackend(surface)
	if surface != nil {
		currentInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	for _, inst := range instances {
		if inst == nil {
			continue
		}
		if filterByBackend && state.EffectiveInstanceBackend(inst) != targetBackend {
			continue
		}
		owner := s.instanceClaimSurface(inst.InstanceID)
		for _, thread := range ordinaryVisibleThreads(inst) {
			if thread == nil {
				continue
			}
			if !threadBelongsToInstanceWorkspace(inst, thread) {
				continue
			}
			view := viewsByID[thread.ThreadID]
			if view == nil {
				view = &mergedThreadView{
					ThreadID: thread.ThreadID,
					Backend:  state.EffectiveInstanceBackend(inst),
				}
				viewsByID[thread.ThreadID] = view
			}
			if shouldPromoteMergedThread(view.Inst, view.Thread, inst, thread) {
				view.Inst = inst
				view.Thread = thread
				view.Backend = state.EffectiveInstanceBackend(inst)
			}
			if inst.InstanceID == currentInstanceID {
				view.CurrentVisible = true
			}
			if inst.Online && betterVisibleThreadInstance(surface, view.AnyVisibleInst, inst, thread) {
				view.AnyVisibleInst = inst
			}
			if inst.Online && (owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID)) &&
				betterVisibleThreadInstance(surface, view.FreeVisibleInst, inst, thread) {
				view.FreeVisibleInst = inst
			}
			compat := s.surfaceInstanceCompatibility(surface, inst)
			if compat.Compatible && inst.Online && betterVisibleThreadInstance(surface, view.CompatibleAnyVisibleInst, inst, thread) {
				view.CompatibleAnyVisibleInst = inst
			}
			if compat.Compatible && inst.Online &&
				(owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID)) &&
				betterVisibleThreadInstance(surface, view.CompatibleFreeVisibleInst, inst, thread) {
				view.CompatibleFreeVisibleInst = inst
			}
		}
	}
	s.mergePersistedRecentThreadsForBackend(viewsByID, targetBackend, filterByBackend)

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

func (s *Service) threadViewsVisibleInNormalList(surface *state.SurfaceConsoleRecord, views []*mergedThreadView) []*mergedThreadView {
	if len(views) == 0 {
		return nil
	}
	allowedWorkspaces := s.normalModeListWorkspaceSetWithViews(surface, views)
	if len(allowedWorkspaces) == 0 {
		return nil
	}
	filtered := make([]*mergedThreadView, 0, len(views))
	for _, view := range views {
		workspaceKey := mergedThreadWorkspaceClaimKey(view)
		if workspaceKey == "" {
			continue
		}
		if _, ok := allowedWorkspaces[workspaceKey]; !ok {
			continue
		}
		filtered = append(filtered, view)
	}
	return filtered
}

func (s *Service) normalModeListWorkspaceSet(surface *state.SurfaceConsoleRecord) map[string]struct{} {
	return s.normalModeListWorkspaceSetWithViews(surface, s.mergedThreadViews(surface))
}

func (s *Service) normalModeListWorkspaceSetWithViews(surface *state.SurfaceConsoleRecord, views []*mergedThreadView) map[string]struct{} {
	workspaces := map[string]struct{}{}
	targetBackend, filterByBackend := s.normalModeThreadBackend(surface)
	for _, inst := range s.root.Instances {
		if inst == nil || !inst.Online {
			continue
		}
		if filterByBackend && state.EffectiveInstanceBackend(inst) != targetBackend {
			continue
		}
		for _, workspaceKey := range instanceWorkspaceSelectionKeys(inst) {
			if workspaceKey == "" {
				continue
			}
			workspaces[workspaceKey] = struct{}{}
		}
	}
	if surface != nil {
		if currentWorkspace := s.surfaceCurrentWorkspaceKey(surface); currentWorkspace != "" {
			workspaces[currentWorkspace] = struct{}{}
		}
	}
	for _, view := range views {
		if workspaceKey := mergedThreadWorkspaceClaimKey(view); workspaceKey != "" {
			workspaces[workspaceKey] = struct{}{}
		}
	}
	return workspaces
}

func (s *Service) mergedThreadView(surface *state.SurfaceConsoleRecord, threadID string) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	if backend, filterByBackend := s.normalModeThreadBackend(surface); filterByBackend {
		return s.mergedThreadViewForBackend(surface, threadID, backend, true)
	}
	for _, view := range s.mergedThreadViews(surface) {
		if view != nil && view.ThreadID == threadID {
			return view
		}
	}
	return s.persistedThreadView(surface, threadID)
}

func (s *Service) mergedThreadViewForBackend(surface *state.SurfaceConsoleRecord, threadID string, backend agentproto.Backend, includePersisted bool) *mergedThreadView {
	threadID = strings.TrimSpace(threadID)
	backend = agentproto.NormalizeBackend(backend)
	if threadID == "" {
		return nil
	}
	view := &mergedThreadView{ThreadID: threadID}
	found := false
	currentInstanceID := ""
	if surface != nil {
		currentInstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	}
	for _, inst := range s.Instances() {
		if inst == nil || state.EffectiveInstanceBackend(inst) != backend {
			continue
		}
		thread := inst.Threads[threadID]
		if !ordinaryThreadVisible(thread) || !threadBelongsToInstanceWorkspace(inst, thread) {
			continue
		}
		found = true
		if shouldPromoteMergedThread(view.Inst, view.Thread, inst, thread) {
			view.Inst = inst
			view.Thread = thread
			view.Backend = state.EffectiveInstanceBackend(inst)
		}
		if inst.InstanceID == currentInstanceID {
			view.CurrentVisible = true
		}
		if inst.Online && betterVisibleThreadInstance(surface, view.AnyVisibleInst, inst, thread) {
			view.AnyVisibleInst = inst
		}
		owner := s.instanceClaimSurface(inst.InstanceID)
		if inst.Online && (owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID)) &&
			betterVisibleThreadInstance(surface, view.FreeVisibleInst, inst, thread) {
			view.FreeVisibleInst = inst
		}
		compat := s.surfaceInstanceCompatibility(surface, inst)
		if compat.Compatible && inst.Online && betterVisibleThreadInstance(surface, view.CompatibleAnyVisibleInst, inst, thread) {
			view.CompatibleAnyVisibleInst = inst
		}
		if compat.Compatible && inst.Online &&
			(owner == nil || (surface != nil && owner.SurfaceSessionID == surface.SurfaceSessionID)) &&
			betterVisibleThreadInstance(surface, view.CompatibleFreeVisibleInst, inst, thread) {
			view.CompatibleFreeVisibleInst = inst
		}
	}
	if includePersisted && s.catalog.persistedThreads != nil {
		thread, err := s.catalog.persistedThreadByIDForBackend(backend, threadID)
		if err == nil && ordinaryThreadVisible(thread) {
			found = true
			if view.Inst == nil {
				view.Inst = syntheticPersistedThreadInstance(thread, backend)
				view.Thread = cloneThreadRecord(thread)
				view.Backend = backend
			} else {
				view.Thread = mergeThreadMetadata(view.Thread, thread)
			}
		}
	}
	if !found {
		return nil
	}
	if owner := s.threadClaimSurface(view.ThreadID); owner != nil && (surface == nil || owner.SurfaceSessionID != surface.SurfaceSessionID) {
		view.BusyOwner = owner
	}
	return view
}

func (s *Service) persistedThreadView(surface *state.SurfaceConsoleRecord, threadID string) *mergedThreadView {
	if s == nil || s.catalog.persistedThreads == nil {
		return nil
	}
	thread, err := s.catalog.persistedThreadByIDForBackend(agentproto.BackendCodex, strings.TrimSpace(threadID))
	if err != nil || !ordinaryThreadVisible(thread) {
		return nil
	}
	view := &mergedThreadView{
		ThreadID: thread.ThreadID,
		Backend:  agentproto.BackendCodex,
		Inst:     syntheticPersistedThreadInstance(thread, agentproto.BackendCodex),
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
	if strings.TrimSpace(merged.FirstUserMessage) == "" {
		merged.FirstUserMessage = strings.TrimSpace(secondary.FirstUserMessage)
	}
	if strings.TrimSpace(merged.LastUserMessage) == "" {
		merged.LastUserMessage = strings.TrimSpace(secondary.LastUserMessage)
	}
	if strings.TrimSpace(merged.LastAssistantMessage) == "" {
		merged.LastAssistantMessage = strings.TrimSpace(secondary.LastAssistantMessage)
	}
	if strings.TrimSpace(merged.CWD) == "" {
		merged.CWD = strings.TrimSpace(secondary.CWD)
	}
	if strings.TrimSpace(merged.WorkspaceKey) == "" {
		merged.WorkspaceKey = strings.TrimSpace(secondary.WorkspaceKey)
	}
	if strings.TrimSpace(merged.ForkedFromID) == "" {
		merged.ForkedFromID = strings.TrimSpace(secondary.ForkedFromID)
	}
	if merged.Source == nil && secondary.Source != nil {
		merged.Source = agentproto.CloneThreadSourceRecord(secondary.Source)
	}
	if strings.TrimSpace(merged.ExplicitModel) == "" {
		merged.ExplicitModel = strings.TrimSpace(secondary.ExplicitModel)
	}
	if strings.TrimSpace(merged.ExplicitReasoningEffort) == "" {
		merged.ExplicitReasoningEffort = strings.TrimSpace(secondary.ExplicitReasoningEffort)
	}
	if merged.LastModelReroute == nil && secondary.LastModelReroute != nil {
		merged.LastModelReroute = agentproto.CloneTurnModelReroute(secondary.LastModelReroute)
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
	if merged.RuntimeStatus == nil && secondary.RuntimeStatus != nil {
		merged.RuntimeStatus = agentproto.CloneThreadRuntimeStatus(secondary.RuntimeStatus)
	}
	if merged.TokenUsage == nil && secondary.TokenUsage != nil {
		merged.TokenUsage = agentproto.CloneThreadTokenUsage(secondary.TokenUsage)
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
	threadCopy.RuntimeStatus = agentproto.CloneThreadRuntimeStatus(thread.RuntimeStatus)
	threadCopy.TokenUsage = agentproto.CloneThreadTokenUsage(thread.TokenUsage)
	threadCopy.LastModelReroute = agentproto.CloneTurnModelReroute(thread.LastModelReroute)
	threadCopy.Source = agentproto.CloneThreadSourceRecord(thread.Source)
	if thread.UndeliveredReplay != nil {
		replayCopy := *thread.UndeliveredReplay
		threadCopy.UndeliveredReplay = &replayCopy
	}
	return &threadCopy
}

func syntheticPersistedThreadInstance(thread *state.ThreadRecord, backend agentproto.Backend) *state.InstanceRecord {
	if thread == nil {
		return nil
	}
	workspaceKey := threadWorkspaceKeyFromRecord(thread)
	if workspaceKey == "" {
		return nil
	}
	return &state.InstanceRecord{
		WorkspaceRoot: workspaceKey,
		WorkspaceKey:  state.ResolveWorkspaceKey(workspaceKey),
		ShortName:     state.WorkspaceShortName(workspaceKey),
		Backend:       agentproto.NormalizeBackend(backend),
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
	if strings.TrimSpace(thread.FirstUserMessage) != "" {
		score++
	}
	if strings.TrimSpace(thread.LastUserMessage) != "" {
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

func betterVisibleThreadInstance(surface *state.SurfaceConsoleRecord, current, next *state.InstanceRecord, thread *state.ThreadRecord) bool {
	if next == nil {
		return false
	}
	if current == nil {
		return true
	}
	currentScore := visibleThreadInstanceScore(surface, current, thread)
	nextScore := visibleThreadInstanceScore(surface, next, thread)
	if nextScore != currentScore {
		return nextScore > currentScore
	}
	return next.InstanceID < current.InstanceID
}

func visibleThreadInstanceScore(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, thread *state.ThreadRecord) int {
	if inst == nil {
		return 0
	}
	score := 0
	root := state.NormalizeWorkspaceKey(inst.WorkspaceRoot)
	cwd := ""
	if thread != nil {
		cwd = state.NormalizeWorkspaceKey(thread.CWD)
	}
	switch {
	case root != "" && cwd != "" && cwd == root:
		score += 10000
	case root != "" && cwd != "" && strings.HasPrefix(cwd, root+"/"):
		score += 5000 + len(root)
	case root != "" && cwd != "":
		score += 1
	}
	if surface != nil && inst.InstanceID == strings.TrimSpace(surface.AttachedInstanceID) {
		score += 100
	}
	if inst.ActiveTurnID == "" {
		score += 1
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
	if s.surfaceIsVSCode(surface) {
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
	resolution := s.resolveThreadContract(surface, view, view.CurrentVisible && s.currentVisibleThreadEligible(surface, view.ThreadID), false)
	switch resolution.Mode {
	case contractResolutionCurrentVisible:
		return resolvedThreadTarget{
			Mode: threadAttachCurrentVisible,
			View: view,
		}
	case contractResolutionAttachVisible:
		return resolvedThreadTarget{
			Mode:     threadAttachFreeVisible,
			View:     view,
			Instance: resolution.Instance,
		}
	case contractResolutionReuseManaged:
		return resolvedThreadTarget{
			Mode:     threadAttachReuseHeadless,
			View:     view,
			Instance: resolution.Instance,
		}
	case contractResolutionRestartManaged, contractResolutionCreateHeadless:
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
	default:
		return resolvedThreadTarget{
			Mode:       threadAttachUnavailable,
			View:       view,
			NoticeCode: firstNonEmpty(strings.TrimSpace(resolution.NoticeCode), "thread_not_found"),
			NoticeText: firstNonEmpty(strings.TrimSpace(resolution.NoticeText), "目标会话不存在或当前不可见。"),
		}
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
	if view.CompatibleFreeVisibleInst != nil && isHeadlessInstance(view.CompatibleFreeVisibleInst) {
		return resolvedThreadTarget{
			Mode:     threadAttachFreeVisible,
			View:     view,
			Instance: view.CompatibleFreeVisibleInst,
		}
	}
	if headless := s.reusableManagedHeadlessForResolution(surface, threadCWD(view), view.Backend); headless != nil && strings.TrimSpace(threadCWD(view)) != "" {
		if s.surfaceInstanceCompatibleForAttach(surface, headless) {
			return resolvedThreadTarget{
				Mode:     threadAttachReuseHeadless,
				View:     view,
				Instance: headless,
			}
		}
		return resolvedThreadTarget{
			Mode: threadAttachCreateHeadless,
			View: view,
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

func (s *Service) resolveSurfaceResumeVisibleInstance(surface *state.SurfaceConsoleRecord, view *mergedThreadView, preferredInstanceID string, backend agentproto.Backend) (*state.InstanceRecord, string) {
	if view == nil {
		return nil, "thread_not_found"
	}
	backend = agentproto.NormalizeBackend(backend)
	preferredInstanceID = strings.TrimSpace(preferredInstanceID)
	if preferredInstanceID != "" {
		if inst := s.root.Instances[preferredInstanceID]; inst != nil && inst.Online &&
			state.EffectiveInstanceBackend(inst) == backend &&
			threadVisible(inst.Threads[view.ThreadID]) && threadBelongsToInstanceWorkspace(inst, inst.Threads[view.ThreadID]) &&
			s.surfaceInstanceCompatibleForAttach(surface, inst) {
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
	if view.CompatibleFreeVisibleInst != nil && state.EffectiveInstanceBackend(view.CompatibleFreeVisibleInst) == backend {
		return view.CompatibleFreeVisibleInst, ""
	}
	if view.AnyVisibleInst != nil && state.EffectiveInstanceBackend(view.AnyVisibleInst) == backend {
		return nil, "workspace_instance_busy"
	}
	return nil, "thread_not_found"
}

func threadWorkspaceKeyFromRecord(thread *state.ThreadRecord) string {
	if thread == nil {
		return ""
	}
	return normalizeWorkspaceClaimKey(firstNonEmpty(strings.TrimSpace(thread.WorkspaceKey), strings.TrimSpace(thread.CWD)))
}

func threadWorkspaceKey(view *mergedThreadView) string {
	if view == nil {
		return ""
	}
	if key := threadWorkspaceKeyFromRecord(view.Thread); key != "" {
		return key
	}
	return instanceWorkspaceClaimKey(view.Inst)
}

func threadCWD(view *mergedThreadView) string {
	if view == nil || view.Thread == nil {
		return ""
	}
	return strings.TrimSpace(firstNonEmpty(view.Thread.CWD, threadWorkspaceKey(view)))
}

func (s *Service) currentVisibleThreadEligible(surface *state.SurfaceConsoleRecord, threadID string) bool {
	if s == nil || surface == nil {
		return false
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	if inst == nil {
		return false
	}
	thread := inst.Threads[strings.TrimSpace(threadID)]
	return threadVisible(thread) && threadBelongsToInstanceWorkspace(inst, thread)
}
