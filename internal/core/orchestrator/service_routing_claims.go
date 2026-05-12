package orchestrator

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) defaultAttachThread(inst *state.InstanceRecord) string {
	if inst == nil {
		return ""
	}
	initialThreadID := inst.ObservedFocusedThreadID
	if initialThreadID == "" {
		initialThreadID = inst.ActiveThreadID
	}
	if !threadVisible(inst.Threads[initialThreadID]) {
		return ""
	}
	return initialThreadID
}

func (s *Service) surfaceThreadPickRouteMode(surface *state.SurfaceConsoleRecord) state.RouteMode {
	if s.surfaceIsVSCode(surface) {
		return state.RouteModeFollowLocal
	}
	return state.RouteModePinned
}

func normalizeWorkspaceClaimKey(value string) string {
	raw := strings.TrimSpace(value)
	value = state.ResolveWorkspaceKey(raw)
	if value == "" {
		return ""
	}
	if !shouldResolveWorkspacePathOnHost(runtime.GOOS, raw) {
		return value
	}
	if resolved, err := state.ResolveWorkspaceRootOnHost(raw); err == nil {
		if resolved = state.ResolveWorkspaceKey(resolved); resolved != "" {
			return resolved
		}
	}
	return value
}

func shouldResolveWorkspacePathOnHost(goos, raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(goos)) {
	case "windows":
		if isWindowsVolumePath(raw) || strings.HasPrefix(raw, `\\`) || strings.HasPrefix(raw, `//`) || strings.HasPrefix(raw, `\`) {
			return true
		}
	default:
		if strings.HasPrefix(raw, "/") {
			return true
		}
	}
	switch raw {
	case ".", "..":
		return true
	}
	return strings.HasPrefix(raw, "./") ||
		strings.HasPrefix(raw, "../") ||
		strings.HasPrefix(raw, `.\\`) ||
		strings.HasPrefix(raw, `..\\`)
}

func looksLikeWorkspacePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if filepath.IsAbs(value) {
		return true
	}
	if isWindowsVolumePath(value) {
		return true
	}
	return strings.Contains(value, "/") || strings.Contains(value, `\`)
}

func isWindowsVolumePath(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	drive := value[0]
	return (drive >= 'a' && drive <= 'z') || (drive >= 'A' && drive <= 'Z')
}

func instanceWorkspaceClaimKey(inst *state.InstanceRecord) string {
	if inst == nil {
		return ""
	}
	if key := normalizeWorkspaceClaimKey(inst.WorkspaceKey); key != "" {
		return key
	}
	return normalizeWorkspaceClaimKey(inst.WorkspaceRoot)
}

func mergedThreadWorkspaceClaimKey(view *mergedThreadView) string {
	if view == nil {
		return ""
	}
	if key := threadWorkspaceKey(view); key != "" {
		return key
	}
	return instanceWorkspaceClaimKey(view.Inst)
}

func (s *Service) surfaceUsesWorkspaceClaims(surface *state.SurfaceConsoleRecord) bool {
	return s.surfaceIsHeadless(surface)
}

func (s *Service) surfaceIsHeadless(surface *state.SurfaceConsoleRecord) bool {
	return state.IsHeadlessProductMode(s.normalizeSurfaceProductMode(surface))
}

func (s *Service) surfaceIsVSCode(surface *state.SurfaceConsoleRecord) bool {
	return state.IsVSCodeProductMode(s.normalizeSurfaceProductMode(surface))
}

func (s *Service) surfaceCurrentWorkspaceKey(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || !s.surfaceUsesWorkspaceClaims(surface) {
		return ""
	}
	if key := normalizeWorkspaceClaimKey(surface.ClaimedWorkspaceKey); key != "" {
		surface.ClaimedWorkspaceKey = key
		return key
	}
	if pending := surface.PendingHeadless; pending != nil {
		if key := normalizeWorkspaceClaimKey(firstNonEmpty(pending.WorkspaceKey, pending.ThreadCWD)); key != "" {
			surface.ClaimedWorkspaceKey = key
			return key
		}
	}
	if key := normalizeWorkspaceClaimKey(surface.PreparedThreadCWD); key != "" {
		surface.ClaimedWorkspaceKey = key
		return key
	}
	if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil {
		if key := instanceWorkspaceClaimKey(inst); key != "" {
			surface.ClaimedWorkspaceKey = key
			return key
		}
	}
	return ""
}

func (s *Service) surfaceAttachmentDisplayName(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		if key := s.surfaceCurrentWorkspaceKey(surface); key != "" {
			return key
		}
		if key := instanceWorkspaceClaimKey(inst); key != "" {
			return key
		}
		if inst != nil {
			for _, thread := range visibleThreads(inst) {
				if key := threadWorkspaceKeyFromRecord(thread); key != "" {
					return key
				}
			}
		}
		return ""
	}
	if inst == nil {
		return ""
	}
	return strings.TrimSpace(inst.DisplayName)
}

func (s *Service) attachedLeadText(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		if name := s.surfaceAttachmentDisplayName(surface, inst); name != "" {
			return fmt.Sprintf("已接管工作区 %s。", name)
		}
		return "已接管当前工作区。"
	}
	if name := s.surfaceAttachmentDisplayName(surface, inst); name != "" {
		return fmt.Sprintf("已接管 %s。", name)
	}
	return "已接管当前实例。"
}

func (s *Service) notAttachedText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "您没有接管任何工作区。请先 /list 选择工作区。"
	}
	return "当前还没有接管任何实例。"
}

func (s *Service) attachedTargetUnavailableText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "当前接管的工作区暂时不可用，请重新 /list 选择工作区后再发送消息。"
	}
	return "当前接管实例不可用，请重新接管后再发送消息。"
}

func (s *Service) detachedText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "已解除对当前工作区的接管。"
	}
	return "已解除对当前实例的接管。"
}

func (s *Service) detachedNoneText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "当前没有已接管的工作区。"
	}
	return "当前没有已接管的实例。"
}

func (s *Service) detachPendingText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "已放弃对当前工作区的接管；未发送的队列和图片已清空，正在等待当前 turn 收尾。"
	}
	return "已放弃对当前实例的接管；未发送的队列和图片已清空，正在等待当前 turn 收尾。"
}

func (s *Service) detachTimeoutText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "等待当前 turn 收尾超时，已强制解除对当前工作区的接管。"
	}
	return "等待当前 turn 收尾超时，已强制解除对当前实例的接管。"
}

func (s *Service) attachmentOfflineText(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		if name := s.surfaceAttachmentDisplayName(surface, inst); name != "" {
			return fmt.Sprintf("当前接管的工作区已离线：%s", name)
		}
		return "当前接管的工作区已离线。"
	}
	if name := s.surfaceAttachmentDisplayName(surface, inst); name != "" {
		return fmt.Sprintf("当前接管实例已离线：%s", name)
	}
	return "当前接管实例已离线。"
}

func (s *Service) attachmentTransportDegradedText(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		if name := s.surfaceAttachmentDisplayName(surface, inst); name != "" {
			return fmt.Sprintf("当前接管的工作区链路过载，正在等待恢复：%s。当前 turn 可能继续执行，但实时输出可能延迟或丢失；如需放弃请 /detach。", name)
		}
		return "当前接管的工作区链路过载，正在等待恢复。当前 turn 可能继续执行，但实时输出可能延迟或丢失；如需放弃请 /detach。"
	}
	if name := s.surfaceAttachmentDisplayName(surface, inst); name != "" {
		return fmt.Sprintf("当前接管实例链路过载，正在等待实例恢复：%s。当前 turn 可能继续执行，但实时输出可能延迟或丢失；如需放弃请 /detach。", name)
	}
	return "当前接管实例链路过载，正在等待实例恢复。当前 turn 可能继续执行，但实时输出可能延迟或丢失；如需放弃请 /detach。"
}

func (s *Service) threadAttachRequiresDetachText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "当前工作区仍有执行中的请求或收尾中的 turn，暂时不能切换到其他工作区的会话。请等待完成，或先 /detach。"
	}
	return "当前实例仍有执行中的请求或收尾中的 turn，暂时不能切换到其他实例上的会话。请等待完成，或先 /detach。"
}

func (s *Service) stopOfflineNotice(surface *state.SurfaceConsoleRecord) control.Notice {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return control.Notice{
			Code:     "stop_instance_offline",
			Title:    "工作区暂时离线",
			Text:     "当前工作区链路正在恢复，暂时无法发送停止请求。你可以等待恢复后再 `/stop`，或直接 `/detach` 放弃接管。",
			ThemeKey: "system",
		}
	}
	return control.Notice{
		Code:     "stop_instance_offline",
		Title:    "实例暂时离线",
		Text:     "当前实例链路正在恢复，暂时无法发送停止请求。你可以等待实例恢复后再 `/stop`，或直接 `/detach` 放弃接管。",
		ThemeKey: "system",
	}
}

func (s *Service) workspaceClaimSurface(workspaceKey string) *state.SurfaceConsoleRecord {
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return nil
	}
	if claim := s.workspaceClaims[workspaceKey]; claim != nil {
		surface := s.root.Surfaces[claim.SurfaceSessionID]
		if surface != nil && s.surfaceUsesWorkspaceClaims(surface) && s.surfaceCurrentWorkspaceKey(surface) == workspaceKey {
			return surface
		}
		delete(s.workspaceClaims, workspaceKey)
	}
	var owner *state.SurfaceConsoleRecord
	for _, surface := range s.root.Surfaces {
		if surface == nil || !s.surfaceUsesWorkspaceClaims(surface) {
			continue
		}
		if s.surfaceCurrentWorkspaceKey(surface) != workspaceKey {
			continue
		}
		if owner == nil ||
			surface.LastInboundAt.After(owner.LastInboundAt) ||
			(surface.LastInboundAt.Equal(owner.LastInboundAt) && surface.SurfaceSessionID < owner.SurfaceSessionID) {
			owner = surface
		}
	}
	if owner != nil {
		s.workspaceClaims[workspaceKey] = &workspaceClaimRecord{
			WorkspaceKey:     workspaceKey,
			SurfaceSessionID: owner.SurfaceSessionID,
		}
	}
	return owner
}

func (s *Service) claimWorkspace(surface *state.SurfaceConsoleRecord, workspaceKey string) bool {
	if surface == nil || !s.surfaceUsesWorkspaceClaims(surface) {
		return true
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return false
	}
	if owner := s.workspaceClaimSurface(workspaceKey); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return false
	}
	if current := s.surfaceCurrentWorkspaceKey(surface); current != "" && current != workspaceKey {
		s.releaseSurfaceWorkspaceClaim(surface)
	}
	surface.ClaimedWorkspaceKey = workspaceKey
	s.workspaceClaims[workspaceKey] = &workspaceClaimRecord{
		WorkspaceKey:     workspaceKey,
		SurfaceSessionID: surface.SurfaceSessionID,
	}
	return true
}

func (s *Service) releaseSurfaceWorkspaceClaim(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	workspaceKey := normalizeWorkspaceClaimKey(surface.ClaimedWorkspaceKey)
	if workspaceKey != "" {
		if claim := s.workspaceClaims[workspaceKey]; claim != nil && claim.SurfaceSessionID == surface.SurfaceSessionID {
			delete(s.workspaceClaims, workspaceKey)
		}
	}
	surface.ClaimedWorkspaceKey = ""
}

func (s *Service) workspaceBusyOwnerForSurface(surface *state.SurfaceConsoleRecord, workspaceKey string) *state.SurfaceConsoleRecord {
	if surface == nil || !s.surfaceUsesWorkspaceClaims(surface) {
		return nil
	}
	owner := s.workspaceClaimSurface(workspaceKey)
	if owner == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
		return nil
	}
	return owner
}

func (s *Service) workspaceBusyOwnerForView(surface *state.SurfaceConsoleRecord, view *mergedThreadView) *state.SurfaceConsoleRecord {
	return s.workspaceBusyOwnerForSurface(surface, mergedThreadWorkspaceClaimKey(view))
}

func (s *Service) instanceClaimSurface(instanceID string) *state.SurfaceConsoleRecord {
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	claim := s.instanceClaims[instanceID]
	if claim == nil {
		return nil
	}
	surface := s.root.Surfaces[claim.SurfaceSessionID]
	if surface == nil {
		delete(s.instanceClaims, instanceID)
		return nil
	}
	if surface.AttachedInstanceID != instanceID {
		delete(s.instanceClaims, instanceID)
		return nil
	}
	return surface
}

func (s *Service) claimInstance(surface *state.SurfaceConsoleRecord, instanceID string) bool {
	if surface == nil || strings.TrimSpace(instanceID) == "" {
		return false
	}
	if owner := s.instanceClaimSurface(instanceID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return false
	}
	s.instanceClaims[instanceID] = &instanceClaimRecord{
		InstanceID:       instanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
	}
	return true
}

func (s *Service) releaseSurfaceInstanceClaim(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	instanceID := strings.TrimSpace(surface.AttachedInstanceID)
	if instanceID == "" {
		return
	}
	if claim := s.instanceClaims[instanceID]; claim != nil && claim.SurfaceSessionID == surface.SurfaceSessionID {
		delete(s.instanceClaims, instanceID)
	}
}

func (s *Service) threadClaimSurface(threadID string) *state.SurfaceConsoleRecord {
	if strings.TrimSpace(threadID) == "" {
		return nil
	}
	claim := s.threadClaims[threadID]
	if claim == nil {
		return nil
	}
	surface := s.root.Surfaces[claim.SurfaceSessionID]
	if surface == nil {
		delete(s.threadClaims, threadID)
		return nil
	}
	if surface.AttachedInstanceID != claim.InstanceID || surface.SelectedThreadID != threadID {
		delete(s.threadClaims, threadID)
		return nil
	}
	return surface
}

func (s *Service) surfaceOwnsThread(surface *state.SurfaceConsoleRecord, threadID string) bool {
	if surface == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	claim := s.threadClaims[threadID]
	return claim != nil && claim.SurfaceSessionID == surface.SurfaceSessionID
}

func (s *Service) claimThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) bool {
	if surface == nil || inst == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	if !threadVisible(inst.Threads[threadID]) {
		return false
	}
	if owner := s.threadClaimSurface(threadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return false
	}
	s.threadClaims[threadID] = &threadClaimRecord{
		ThreadID:         threadID,
		InstanceID:       inst.InstanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
	}
	return true
}

func (s *Service) claimKnownThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) bool {
	if surface == nil || inst == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	if owner := s.threadClaimSurface(threadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return false
	}
	s.threadClaims[threadID] = &threadClaimRecord{
		ThreadID:         threadID,
		InstanceID:       inst.InstanceID,
		SurfaceSessionID: surface.SurfaceSessionID,
	}
	return true
}

func (s *Service) releaseSurfaceThreadClaim(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	if threadID != "" {
		if claim := s.threadClaims[threadID]; claim != nil && claim.SurfaceSessionID == surface.SurfaceSessionID {
			delete(s.threadClaims, threadID)
		}
	}
	surface.SelectedThreadID = ""
}

func (s *Service) surfaceHasLiveRemoteWork(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	if s.progress.surfaceHasPendingCompact(surface) {
		return true
	}
	if s.surfaceHasPendingSteer(surface) {
		return true
	}
	if episode := activeAutoContinueEpisode(surface); episode != nil {
		switch episode.State {
		case state.AutoContinueEpisodeScheduled, state.AutoContinueEpisodeRunning:
			return true
		}
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return true
			}
		}
	}
	if review := s.activeReviewSession(surface); review != nil && strings.TrimSpace(review.ActiveTurnID) != "" {
		return true
	}
	return len(surface.QueuedQueueItemIDs) != 0
}

func (s *Service) queueItemTargetsThread(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, threadID string) bool {
	if surface == nil || item == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	if executionThreadID := queuedItemExecutionThreadID(item); executionThreadID != "" {
		return executionThreadID == threadID
	}
	return surface.SelectedThreadID == threadID
}

func (s *Service) surfaceHasQueuedWorkOnThread(surface *state.SurfaceConsoleRecord, threadID string) bool {
	if surface == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.Status != state.QueueItemQueued {
			continue
		}
		if s.queueItemTargetsThread(surface, item, threadID) {
			return true
		}
	}
	return false
}

func (s *Service) threadKickStatus(inst *state.InstanceRecord, owner *state.SurfaceConsoleRecord, threadID string) threadKickStatus {
	if inst != nil && inst.ActiveTurnID != "" && inst.ActiveThreadID == threadID {
		return threadKickRunning
	}
	if inst != nil && threadRuntimeActive(inst.Threads[threadID]) {
		return threadKickRunning
	}
	if owner == nil {
		return threadKickIdle
	}
	if owner.ActiveQueueItemID != "" {
		if item := owner.QueueItems[owner.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				if s.queueItemTargetsThread(owner, item, threadID) {
					return threadKickRunning
				}
			}
		}
	}
	if s.surfaceHasQueuedWorkOnThread(owner, threadID) {
		return threadKickQueued
	}
	return threadKickIdle
}
