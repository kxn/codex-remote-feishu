package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type threadKickStatus string

const (
	threadKickIdle    threadKickStatus = "idle"
	threadKickQueued  threadKickStatus = "queued"
	threadKickRunning threadKickStatus = "running"
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
	if surface != nil && s.normalizeSurfaceProductMode(surface) == state.ProductModeVSCode {
		return state.RouteModeFollowLocal
	}
	return state.RouteModePinned
}

func normalizeWorkspaceClaimKey(value string) string {
	return state.ResolveWorkspaceKey(value)
}

func instanceWorkspaceClaimKey(inst *state.InstanceRecord) string {
	if inst == nil {
		return ""
	}
	return state.ResolveWorkspaceKey(inst.WorkspaceKey, inst.WorkspaceRoot)
}

func mergedThreadWorkspaceClaimKey(view *mergedThreadView) string {
	if view == nil {
		return ""
	}
	if key := normalizeWorkspaceClaimKey(threadCWD(view)); key != "" {
		return key
	}
	return instanceWorkspaceClaimKey(view.Inst)
}

func (s *Service) surfaceUsesWorkspaceClaims(surface *state.SurfaceConsoleRecord) bool {
	return s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal
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
		if key := normalizeWorkspaceClaimKey(pending.ThreadCWD); key != "" {
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
				if key := state.ResolveWorkspaceKey(thread.CWD); key != "" {
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
		return "已断开当前工作区接管。"
	}
	return "已断开当前实例接管。"
}

func (s *Service) detachedNoneText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "当前没有接管中的工作区。"
	}
	return "当前没有接管中的实例。"
}

func (s *Service) detachPendingText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "已放弃当前工作区接管；未发送的队列和图片已清空，正在等待当前 turn 收尾。"
	}
	return "已放弃当前实例接管；未发送的队列和图片已清空，正在等待当前 turn 收尾。"
}

func (s *Service) detachTimeoutText(surface *state.SurfaceConsoleRecord) string {
	if s.surfaceUsesWorkspaceClaims(surface) {
		return "等待当前 turn 收尾超时，已强制断开当前工作区接管。"
	}
	return "等待当前 turn 收尾超时，已强制断开当前实例接管。"
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
	if s.surfaceHasPendingSteer(surface) {
		return true
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return true
			}
		}
	}
	return len(surface.QueuedQueueItemIDs) != 0
}

func (s *Service) queueItemTargetsThread(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, threadID string) bool {
	if surface == nil || item == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	if item.FrozenThreadID != "" {
		return item.FrozenThreadID == threadID
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

func (s *Service) blockThreadSwitch(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching:
				return notice(surface, "thread_switch_dispatching", "当前请求正在派发，暂时不能切换会话。")
			case state.QueueItemRunning:
				return notice(surface, "thread_switch_running", "当前请求正在执行，暂时不能切换会话。")
			}
		}
	}
	if len(surface.QueuedQueueItemIDs) != 0 {
		return notice(surface, "thread_switch_queued", "当前还有排队消息，暂时不能切换会话。请等待队列清空、/stop，或 /detach。")
	}
	return nil
}

func (s *Service) blockFreshThreadAttach(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
		return blocked
	}
	if surface.RouteMode == state.RouteModeNewThreadReady {
		if blocked := s.blockPreparedNewThreadRouteExit(surface); blocked != nil {
			return blocked
		}
	} else if blocked := s.blockThreadSwitch(surface); blocked != nil {
		return blocked
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if s.surfaceNeedsDelayedDetach(surface, inst) {
		return notice(surface, "thread_attach_requires_detach", s.threadAttachRequiresDetachText(surface))
	}
	return nil
}

func surfaceHasRouteMutationRequestState(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	return surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil
}

func (s *Service) blockRouteMutationForRequestState(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if surface.ActiveRequestCapture != nil {
		return notice(surface, "request_capture_waiting_text", "当前正在等待你发送一条文字处理意见，请先发送文本或重新处理确认卡片。")
	}
	if pending := activePendingRequest(surface); pending != nil {
		_ = pending
		return notice(surface, "request_pending", "当前有待确认请求。请先处理确认卡片，再切换输入目标。")
	}
	return nil
}

func (s *Service) blockNewThreadPreparation(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	if item := s.preparedNewThreadActiveItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching:
			return notice(surface, "new_thread_dispatching", "当前新会话的首条消息正在派发，暂时不能再次 /new。")
		case state.QueueItemRunning:
			return notice(surface, "new_thread_running", "当前新会话的首条消息正在执行，暂时不能再次 /new。")
		}
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching:
				return notice(surface, "new_thread_blocked_dispatching", "当前请求正在派发，暂时不能新建会话。")
			case state.QueueItemRunning:
				return notice(surface, "new_thread_blocked_running", "当前请求正在执行，暂时不能新建会话。")
			}
		}
	}
	return nil
}

func (s *Service) blockPreparedNewThreadRouteExit(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady {
		return nil
	}
	if item := s.preparedNewThreadActiveItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching:
			return notice(surface, "new_thread_switch_dispatching", "当前新会话的首条消息正在派发，暂时不能切换目标。请等待它落地，或直接 /detach。")
		case state.QueueItemRunning:
			return notice(surface, "new_thread_switch_running", "当前新会话的首条消息正在执行，暂时不能切换目标。请等待它完成，或直接 /detach。")
		}
	}
	return nil
}

func (s *Service) blockPreparedNewThreadReprepare(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady {
		return nil
	}
	if item := s.preparedNewThreadActiveItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching:
			return notice(surface, "new_thread_dispatching", "当前新会话的首条消息正在派发，暂时不能再次 /new。")
		case state.QueueItemRunning:
			return notice(surface, "new_thread_running", "当前新会话的首条消息正在执行，暂时不能再次 /new。")
		}
	}
	return nil
}

func (s *Service) preparedNewThreadActiveItem(surface *state.SurfaceConsoleRecord) *state.QueueItemRecord {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady || surface.ActiveQueueItemID == "" {
		return nil
	}
	item := surface.QueueItems[surface.ActiveQueueItemID]
	if item == nil || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady {
		return nil
	}
	switch item.Status {
	case state.QueueItemDispatching, state.QueueItemRunning:
		return item
	default:
		return nil
	}
}

func (s *Service) preparedNewThreadQueuedItem(surface *state.SurfaceConsoleRecord) *state.QueueItemRecord {
	if surface == nil || surface.RouteMode != state.RouteModeNewThreadReady {
		return nil
	}
	for _, queueID := range surface.QueuedQueueItemIDs {
		item := surface.QueueItems[queueID]
		if item == nil || item.RouteModeAtEnqueue != state.RouteModeNewThreadReady || item.Status != state.QueueItemQueued {
			continue
		}
		return item
	}
	return nil
}

func (s *Service) preparedNewThreadHasPendingCreate(surface *state.SurfaceConsoleRecord) bool {
	return s.preparedNewThreadActiveItem(surface) != nil || s.preparedNewThreadQueuedItem(surface) != nil
}

func (s *Service) clearPreparedNewThread(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.PreparedThreadCWD = ""
	surface.PreparedFromThreadID = ""
	surface.PreparedAt = time.Time{}
}

func (s *Service) unboundInputBlocked(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil || surface.AttachedInstanceID == "" {
		return nil
	}
	switch surface.RouteMode {
	case state.RouteModeFollowLocal:
		if surface.SelectedThreadID != "" && s.surfaceOwnsThread(surface, surface.SelectedThreadID) {
			return nil
		}
		return notice(surface, "follow_waiting", "当前已进入跟随模式，但还没有可接管的 VS Code 会话。请先在 VS Code 里实际操作一次会话，或通过 /use 选择当前实例已知会话。")
	case state.RouteModeNewThreadReady:
		if strings.TrimSpace(surface.PreparedThreadCWD) != "" {
			return nil
		}
		return notice(surface, "new_thread_cwd_missing", "当前无法获取新会话的工作目录，请先重新 /use 一个有工作目录的会话。")
	default:
		if surface.SelectedThreadID != "" && s.surfaceOwnsThread(surface, surface.SelectedThreadID) {
			return nil
		}
		if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
			return notice(surface, "thread_unbound", "当前还没有绑定会话；请先 /use 选择一个会话，或执行 /new 准备新会话。")
		}
		return notice(surface, "thread_unbound", "当前还没有绑定会话，请先 /use 选择一个会话，或执行 /follow 进入跟随模式。")
	}
}

func (s *Service) autoPromptUseThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) []control.UIEvent {
	if surface == nil || inst == nil || len(visibleThreads(inst)) == 0 {
		return nil
	}
	return s.presentThreadSelection(surface, false)
}

func (s *Service) threadSelectionSubtitle(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, thread *state.ThreadRecord) string {
	subtitle := threadSelectionSubtitle(thread, thread.ThreadID)
	status := ""
	owner := s.threadClaimSurface(thread.ThreadID)
	switch {
	case surface != nil && s.surfaceOwnsThread(surface, thread.ThreadID):
		if surface.RouteMode == state.RouteModeFollowLocal {
			status = "当前跟随"
		} else {
			status = "当前会话"
		}
	case owner != nil:
		switch s.threadKickStatus(inst, owner, thread.ThreadID) {
		case threadKickIdle:
			status = "已被其他飞书会话占用，可强踢"
		case threadKickQueued:
			status = "已被其他飞书会话占用，对方队列未空"
		case threadKickRunning:
			status = "已被其他飞书会话占用，对方正在执行"
		}
	default:
		status = "可切换"
	}
	if status == "" {
		return subtitle
	}
	if subtitle == "" {
		return status
	}
	return subtitle + "\n" + status
}

func (s *Service) restoreStagedInputs(surface *state.SurfaceConsoleRecord, sourceMessageIDs []string) {
	if surface == nil || len(sourceMessageIDs) == 0 {
		return
	}
	allowed := map[string]bool{}
	for _, messageID := range sourceMessageIDs {
		if strings.TrimSpace(messageID) != "" {
			allowed[messageID] = true
		}
	}
	for _, image := range surface.StagedImages {
		if image == nil || image.State != state.ImageBound || !allowed[image.SourceMessageID] {
			continue
		}
		image.State = state.ImageStaged
	}
}

func (s *Service) surfaceNeedsDelayedDetach(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	if surface == nil {
		return false
	}
	if inst != nil && !inst.Online {
		return false
	}
	if s.surfaceHasPendingSteer(surface) {
		return true
	}
	if binding := s.remoteBindingForSurface(surface); binding != nil {
		return true
	}
	if surface.ActiveQueueItemID != "" {
		if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				return true
			}
		}
	}
	return inst != nil && inst.ActiveTurnID != "" && s.surfaceOwnsThread(surface, inst.ActiveThreadID)
}

func (s *Service) finalizeDetachedSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	instanceID := surface.AttachedInstanceID
	s.clearRemoteOwnership(surface)
	s.releaseSurfaceWorkspaceClaim(surface)
	s.releaseSurfaceThreadClaim(surface)
	s.releaseSurfaceInstanceClaim(surface)
	s.clearPreparedNewThread(surface)
	surface.AttachedInstanceID = ""
	surface.RouteMode = state.RouteModeUnbound
	surface.Abandoning = false
	surface.DispatchMode = state.DispatchModeNormal
	surface.ActiveTurnOrigin = ""
	surface.PromptOverride = state.ModelConfigRecord{}
	surface.PendingHeadless = nil
	surface.ActiveQueueItemID = ""
	delete(s.handoffUntil, surface.SurfaceSessionID)
	delete(s.pausedUntil, surface.SurfaceSessionID)
	delete(s.abandoningUntil, surface.SurfaceSessionID)
	clearSurfaceRequests(surface)
	surface.LastSelection = nil
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	if inst := s.root.Instances[instanceID]; inst == nil || !inst.Online {
		return nil
	}
	return s.reevaluateFollowSurfaces(instanceID)
}

func (s *Service) finishSurfaceAfterWork(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if surface.Abandoning && !s.surfaceNeedsDelayedDetach(surface, inst) {
		events := s.finalizeDetachedSurface(surface)
		return append(events, notice(surface, "detached", s.detachedText(surface))...)
	}
	if surface.RouteMode == state.RouteModeFollowLocal && !s.surfaceHasLiveRemoteWork(surface) {
		return s.reevaluateFollowSurface(surface)
	}
	return nil
}

func (s *Service) followLocal(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if s.normalizeSurfaceProductMode(surface) == state.ProductModeNormal {
		if strings.TrimSpace(surface.AttachedInstanceID) == "" {
			return notice(surface, "follow_deprecated_normal", "normal 模式不再支持 /follow。请先 /list 选择工作区，再 /use 或 /new；如需跟随 VS Code，请先 /mode vscode。")
		}
		return notice(surface, "follow_deprecated_normal", "normal 模式不再支持 /follow。请继续 /use 选择当前工作区会话，或 /new 准备新会话；如需跟随 VS Code，请先 /mode vscode。")
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
	}
	if surfaceHasRouteMutationRequestState(surface) &&
		(surface.RouteMode != state.RouteModeFollowLocal || s.followLocalWouldRetarget(surface, inst)) {
		if blocked := s.blockRouteMutationForRequestState(surface); blocked != nil {
			return blocked
		}
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
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)...)
	s.clearPreparedNewThread(surface)
	surface.RouteMode = state.RouteModeFollowLocal
	reevaluated := s.reevaluateFollowSurface(surface)
	events = append(events, reevaluated...)
	if len(reevaluated) == 0 && surface.SelectedThreadID != "" && s.surfaceOwnsThread(surface, surface.SelectedThreadID) {
		thread := s.ensureThread(inst, surface.SelectedThreadID)
		events = append(events, s.threadSelectionEvents(
			surface,
			surface.SelectedThreadID,
			string(state.RouteModeFollowLocal),
			displayThreadTitle(inst, thread, surface.SelectedThreadID),
			threadPreview(thread),
		)...)
	}
	if len(events) != 0 {
		return events
	}
	return notice(surface, "follow_local_enabled", "已进入跟随模式。后续会尝试跟随当前 VS Code 会话。")
}

func (s *Service) followLocalWouldRetarget(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) bool {
	if surface == nil || inst == nil || surface.RouteMode != state.RouteModeFollowLocal {
		return true
	}
	if s.surfaceHasLiveRemoteWork(surface) {
		return false
	}
	if inst.ActiveTurnID != "" && s.surfaceOwnsThread(surface, inst.ActiveThreadID) {
		return false
	}
	targetThreadID := strings.TrimSpace(inst.ObservedFocusedThreadID)
	if targetThreadID == "" || !threadVisible(inst.Threads[targetThreadID]) {
		return surface.SelectedThreadID != ""
	}
	if owner := s.threadClaimSurface(targetThreadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		return surface.SelectedThreadID != ""
	}
	return surface.SelectedThreadID != targetThreadID || !s.surfaceOwnsThread(surface, targetThreadID)
}

func (s *Service) reevaluateFollowSurfaces(instanceID string) []control.UIEvent {
	if strings.TrimSpace(instanceID) == "" {
		return nil
	}
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		events = append(events, s.reevaluateFollowSurface(surface)...)
	}
	return events
}

func (s *Service) reevaluateFollowSurface(surface *state.SurfaceConsoleRecord) []control.UIEvent {
	if surface == nil || surface.Abandoning || surface.AttachedInstanceID == "" || surface.RouteMode != state.RouteModeFollowLocal {
		return nil
	}
	if s.surfaceHasLiveRemoteWork(surface) {
		return nil
	}
	if surfaceHasRouteMutationRequestState(surface) {
		return nil
	}
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return nil
	}
	if inst.ActiveTurnID != "" && s.surfaceOwnsThread(surface, inst.ActiveThreadID) {
		return nil
	}
	targetThreadID := strings.TrimSpace(inst.ObservedFocusedThreadID)
	if targetThreadID == "" || !threadVisible(inst.Threads[targetThreadID]) {
		if surface.SelectedThreadID == "" {
			return nil
		}
		prevThreadID := surface.SelectedThreadID
		prevRouteMode := surface.RouteMode
		events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)
		s.releaseSurfaceThreadClaim(surface)
		return append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）", "")...)
	}
	if owner := s.threadClaimSurface(targetThreadID); owner != nil && owner.SurfaceSessionID != surface.SurfaceSessionID {
		if surface.SelectedThreadID == "" {
			return nil
		}
		prevThreadID := surface.SelectedThreadID
		prevRouteMode := surface.RouteMode
		events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)
		s.releaseSurfaceThreadClaim(surface)
		return append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）", "")...)
	}
	if surface.SelectedThreadID == targetThreadID && s.surfaceOwnsThread(surface, targetThreadID) {
		return nil
	}
	return s.bindSurfaceToThreadMode(surface, inst, targetThreadID, state.RouteModeFollowLocal)
}

func (s *Service) presentKickThreadPrompt(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string, owner *state.SurfaceConsoleRecord) []control.UIEvent {
	thread := inst.Threads[threadID]
	title := displayThreadTitle(inst, thread, threadID)
	subtitle := s.threadSelectionSubtitle(surface, inst, thread)
	return []control.UIEvent{{
		Kind:             control.UIEventSelectionPrompt,
		SurfaceSessionID: surface.SurfaceSessionID,
		SelectionPrompt: &control.SelectionPrompt{
			Kind:  control.SelectionPromptKickThread,
			Title: "强踢当前会话？",
			Hint:  "只有对方当前空闲时才能强踢；确认前会再次校验状态。",
			Options: []control.SelectionOption{
				{
					Index:       1,
					OptionID:    "cancel",
					Label:       "保留当前状态，不执行强踢。",
					ButtonLabel: "取消",
				},
				{
					Index:       2,
					OptionID:    threadID,
					Label:       title,
					Subtitle:    subtitle,
					ButtonLabel: "强踢并占用",
				},
			},
		},
	}}
}

func (s *Service) confirmKickThread(surface *state.SurfaceConsoleRecord, threadID string) []control.UIEvent {
	inst := s.root.Instances[surface.AttachedInstanceID]
	if inst == nil {
		return notice(surface, "not_attached", s.notAttachedText(surface))
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
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return append(events, notice(surface, "selection_invalid", "缺少目标会话，无法执行强踢。")...)
	}
	owner := s.threadClaimSurface(threadID)
	if owner == nil {
		return append(events, s.useThread(surface, threadID)...)
	}
	if owner.SurfaceSessionID == surface.SurfaceSessionID {
		return append(events, s.useThread(surface, threadID)...)
	}
	switch s.threadKickStatus(inst, owner, threadID) {
	case threadKickIdle:
		return append(events, s.kickThreadOwner(surface, inst, threadID, owner)...)
	case threadKickQueued:
		return append(events, notice(surface, "thread_busy_queued", "目标会话当前还有排队任务，暂时不能强踢。")...)
	case threadKickRunning:
		return append(events, notice(surface, "thread_busy_running", "目标会话当前正在执行，暂时不能强踢。")...)
	default:
		return append(events, notice(surface, "thread_busy", "目标会话当前已被其他飞书会话占用。")...)
	}
}

func (s *Service) kickThreadOwner(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string, victim *state.SurfaceConsoleRecord) []control.UIEvent {
	events := s.releaseVictimThread(victim, inst, threadID)
	events = append(events, s.bindSurfaceToThreadMode(surface, inst, threadID, s.surfaceThreadPickRouteMode(surface))...)
	events = append(events, notice(surface, "thread_kicked", "已接管目标会话。原拥有者已退回未绑定状态。")...)
	return events
}

func (s *Service) releaseVictimThread(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord, threadID string) []control.UIEvent {
	if surface == nil {
		return nil
	}
	clearSurfaceRequestsForTurn(surface, threadID, "")
	prevThreadID := surface.SelectedThreadID
	prevRouteMode := surface.RouteMode
	s.releaseSurfaceThreadClaim(surface)
	routeMode := state.RouteModeUnbound
	title := "未绑定会话"
	events := s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeUnbound)
	if surface.RouteMode == state.RouteModeFollowLocal {
		routeMode = state.RouteModeFollowLocal
		title = "跟随当前 VS Code（等待中）"
		events = s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)
	}
	surface.RouteMode = routeMode
	events = append(events, s.threadSelectionEvents(surface, "", string(routeMode), title, "")...)
	events = append(events, control.UIEvent{
		Kind:             control.UIEventNotice,
		SurfaceSessionID: surface.SurfaceSessionID,
		Notice: &control.Notice{
			Code: "thread_claim_lost",
			Text: "当前会话已被其他飞书会话接管。请重新 /use 选择会话，或等待本地切换。",
		},
	})
	if routeMode == state.RouteModeUnbound {
		events = append(events, s.autoPromptUseThread(surface, inst)...)
	} else {
		events = append(events, s.reevaluateFollowSurface(surface)...)
	}
	return events
}

func (s *Service) reconcileInstanceSurfaceThreads(instanceID string) []control.UIEvent {
	inst := s.root.Instances[instanceID]
	if inst == nil {
		return nil
	}
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		threadID := strings.TrimSpace(surface.SelectedThreadID)
		if threadID == "" {
			continue
		}
		if threadVisible(inst.Threads[threadID]) && s.surfaceOwnsThread(surface, threadID) {
			continue
		}
		clearSurfaceRequestsForTurn(surface, threadID, "")
		prevThreadID := surface.SelectedThreadID
		prevRouteMode := surface.RouteMode
		s.releaseSurfaceThreadClaim(surface)
		switch surface.RouteMode {
		case state.RouteModeFollowLocal:
			events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeFollowLocal)...)
			events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeFollowLocal), "跟随当前 VS Code（等待中）", "")...)
			events = append(events, s.reevaluateFollowSurface(surface)...)
		default:
			surface.RouteMode = state.RouteModeUnbound
			events = append(events, s.discardStagedImagesForRouteChange(surface, prevThreadID, prevRouteMode, "", state.RouteModeUnbound)...)
			events = append(events, s.threadSelectionEvents(surface, "", string(state.RouteModeUnbound), "未绑定会话", "")...)
			events = append(events, control.UIEvent{
				Kind:             control.UIEventNotice,
				SurfaceSessionID: surface.SurfaceSessionID,
				Notice: &control.Notice{
					Code: "selected_thread_lost",
					Text: "原先绑定的会话已不可用，请重新 /use 选择会话。",
				},
			})
			events = append(events, s.autoPromptUseThread(surface, inst)...)
		}
	}
	return events
}

func clearSurfaceRequests(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.PendingRequests = map[string]*state.RequestPromptRecord{}
	clearSurfaceRequestCapture(surface)
	clearSurfaceCommandCapture(surface)
}

func clearSurfaceRequestsForTurn(surface *state.SurfaceConsoleRecord, threadID, turnID string) {
	if surface == nil {
		return
	}
	if len(surface.PendingRequests) != 0 {
		for requestID, request := range surface.PendingRequests {
			if request == nil {
				delete(surface.PendingRequests, requestID)
				continue
			}
			if turnID != "" && request.TurnID != "" && request.TurnID != turnID {
				continue
			}
			if threadID != "" && request.ThreadID != "" && request.ThreadID != threadID {
				continue
			}
			delete(surface.PendingRequests, requestID)
		}
	}
	clearSurfaceRequestCaptureForTurn(surface, threadID, turnID)
}

func clearSurfaceRequestCapture(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceRequestCaptureByRequestID(surface *state.SurfaceConsoleRecord, requestID string) {
	if surface == nil || surface.ActiveRequestCapture == nil {
		return
	}
	if requestID == "" || surface.ActiveRequestCapture.RequestID != requestID {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceRequestCaptureForTurn(surface *state.SurfaceConsoleRecord, threadID, turnID string) {
	if surface == nil || surface.ActiveRequestCapture == nil {
		return
	}
	capture := surface.ActiveRequestCapture
	if turnID != "" && capture.TurnID != "" && capture.TurnID != turnID {
		return
	}
	if threadID != "" && capture.ThreadID != "" && capture.ThreadID != threadID {
		return
	}
	surface.ActiveRequestCapture = nil
}

func clearSurfaceCommandCapture(surface *state.SurfaceConsoleRecord) {
	if surface == nil {
		return
	}
	surface.ActiveCommandCapture = nil
}

func (s *Service) clearRequestsForTurn(instanceID, threadID, turnID string) {
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		clearSurfaceRequestsForTurn(surface, threadID, turnID)
	}
}

func (s *Service) clearTurnArtifacts(instanceID, threadID, turnID string) {
	deleteMatchingItemBuffers(s.itemBuffers, instanceID, threadID, turnID)
	if turnID == "" {
		return
	}
	delete(s.pendingTurnText, turnRenderKey(instanceID, threadID, turnID))
	s.clearRequestsForTurn(instanceID, threadID, turnID)
}

func (s *Service) turnSurface(instanceID, threadID, turnID string) *state.SurfaceConsoleRecord {
	if binding := s.lookupRemoteTurn(instanceID, threadID, turnID); binding != nil {
		if surface := s.root.Surfaces[binding.SurfaceSessionID]; surface != nil {
			return surface
		}
	}
	return s.threadClaimSurface(threadID)
}

func (s *Service) surfaceForInitiator(instanceID string, event agentproto.Event) *state.SurfaceConsoleRecord {
	if event.Initiator.Kind == agentproto.InitiatorRemoteSurface && strings.TrimSpace(event.Initiator.SurfaceSessionID) != "" {
		if surface := s.root.Surfaces[event.Initiator.SurfaceSessionID]; surface != nil {
			return surface
		}
	}
	return s.turnSurface(instanceID, event.ThreadID, event.TurnID)
}

func (s *Service) pauseForLocal(instanceID string) []control.UIEvent {
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		s.pausedUntil[surface.SurfaceSessionID] = s.now().Add(s.config.LocalPauseMaxWait)
		if surface.DispatchMode == state.DispatchModePausedForLocal {
			continue
		}
		surface.DispatchMode = state.DispatchModePausedForLocal
		events = append(events, notice(surface, "local_activity_detected", "检测到本地 VS Code 正在使用，飞书消息将继续排队。")...)
	}
	return events
}

func (s *Service) enterHandoff(instanceID string) []control.UIEvent {
	var events []control.UIEvent
	for _, surface := range s.findAttachedSurfaces(instanceID) {
		if surface.DispatchMode != state.DispatchModePausedForLocal {
			continue
		}
		delete(s.pausedUntil, surface.SurfaceSessionID)
		if len(surface.QueuedQueueItemIDs) == 0 {
			surface.DispatchMode = state.DispatchModeNormal
			delete(s.handoffUntil, surface.SurfaceSessionID)
			continue
		}
		surface.DispatchMode = state.DispatchModeHandoffWait
		s.handoffUntil[surface.SurfaceSessionID] = s.now().Add(s.config.TurnHandoffWait)
	}
	return events
}
