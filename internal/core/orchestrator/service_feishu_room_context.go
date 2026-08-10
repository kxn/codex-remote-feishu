package orchestrator

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

const feishuRoomActiveAutoNoticeCooldown = time.Minute

const feishuRoomGroupOnDemandReservationReason = "headless_group_on_demand"

func feishuRoomContextID(chatID string) string {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return ""
	}
	return "feishu:chat:" + chatID
}

func surfaceFeishuRoomID(surface *state.SurfaceConsoleRecord) string {
	if surface == nil || !surfaceIsFeishuChat(surface) {
		return ""
	}
	return feishuRoomContextID(surface.ChatID)
}

func (s *Service) ensureFeishuRoomContextForSurface(surface *state.SurfaceConsoleRecord) *state.FeishuRoomContextRecord {
	if s == nil || s.root == nil {
		return nil
	}
	roomID := surfaceFeishuRoomID(surface)
	if roomID == "" {
		return nil
	}
	if s.root.FeishuRoomContexts == nil {
		s.root.FeishuRoomContexts = map[string]*state.FeishuRoomContextRecord{}
	}
	room := s.root.FeishuRoomContexts[roomID]
	if room == nil {
		room = &state.FeishuRoomContextRecord{
			RoomID:             roomID,
			ChatID:             strings.TrimSpace(surface.ChatID),
			ActiveReservations: map[string]*state.FeishuRoomActiveReservationRecord{},
			GatewayIDs:         map[string]bool{},
			SurfaceSessionIDs:  map[string]bool{},
		}
		s.root.FeishuRoomContexts[roomID] = room
	}
	if room.GatewayIDs == nil {
		room.GatewayIDs = map[string]bool{}
	}
	if room.SurfaceSessionIDs == nil {
		room.SurfaceSessionIDs = map[string]bool{}
	}
	if room.ActiveReservations == nil {
		room.ActiveReservations = map[string]*state.FeishuRoomActiveReservationRecord{}
	}
	if gatewayID := strings.TrimSpace(surface.GatewayID); gatewayID != "" {
		room.GatewayIDs[gatewayID] = true
	}
	if surfaceID := strings.TrimSpace(surface.SurfaceSessionID); surfaceID != "" {
		room.SurfaceSessionIDs[surfaceID] = true
	}
	return room
}

func (s *Service) feishuRoomSurfaces(roomID string) []*state.SurfaceConsoleRecord {
	if s == nil || s.root == nil {
		return nil
	}
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return nil
	}
	room := s.root.FeishuRoomContexts[roomID]
	if room == nil {
		return nil
	}
	surfaces := make([]*state.SurfaceConsoleRecord, 0, len(room.SurfaceSessionIDs))
	for surfaceID := range room.SurfaceSessionIDs {
		surface := s.root.Surfaces[surfaceID]
		if surface != nil && surfaceFeishuRoomID(surface) == roomID {
			surfaces = append(surfaces, surface)
		}
	}
	sort.Slice(surfaces, func(i, j int) bool {
		return surfaces[i].SurfaceSessionID < surfaces[j].SurfaceSessionID
	})
	return surfaces
}

func (s *Service) prepareFeishuRoomWorkspaceChange(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return s.checkFeishuRoomWorkspaceChange(surface, workspaceKey, true)
}

func (s *Service) preflightFeishuRoomWorkspaceChange(surface *state.SurfaceConsoleRecord, workspaceKey string) []eventcontract.Event {
	return s.checkFeishuRoomWorkspaceChange(surface, workspaceKey, false)
}

func (s *Service) checkFeishuRoomWorkspaceChange(surface *state.SurfaceConsoleRecord, workspaceKey string, mutate bool) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return nil
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	current := normalizeWorkspaceClaimKey(room.WorkspaceKey)
	if current == "" || current == workspaceKey {
		return nil
	}
	if blocked := s.blockUnsafeFeishuRoomWorkspaceReset(room); blocked != "" {
		return notice(surface, "room_workspace_busy", blocked)
	}
	if primaryBotStateForSurface(surface, room) != control.CatalogPrimaryBotStateCurrent {
		return notice(surface, "room_workspace_primary_required", "请先对当前机器人执行 `/primary on`，再切换群 workspace。")
	}
	if !mutate {
		return nil
	}
	s.resetFeishuRoomWorkspaceSurfaces(room, surface)
	room.WorkspaceResetGeneration++
	room.WorkspaceKey = ""
	return nil
}

func (s *Service) detachWorkspace(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return s.detach(surface)
	}
	if primaryBotStateForSurface(surface, room) != control.CatalogPrimaryBotStateCurrent {
		return notice(surface, "room_workspace_primary_required", "只有当前群主机器人可以解除或切换群 workspace。请先对当前机器人执行 `/primary on`。")
	}
	if blocked := s.blockUnsafeFeishuRoomWorkspaceReset(room); blocked != "" {
		return notice(surface, "room_workspace_busy", blocked)
	}
	if normalizeWorkspaceClaimKey(room.WorkspaceKey) == "" {
		s.resetFeishuRoomWorkspaceSurfaces(room, nil)
		return notice(surface, "room_workspace_not_attached", "当前群还没有绑定 workspace。")
	}
	s.resetFeishuRoomWorkspaceSurfaces(room, nil)
	room.WorkspaceResetGeneration++
	room.WorkspaceKey = ""
	room.WorkspaceUpdatedBy = strings.TrimSpace(surface.ActorUserID)
	room.WorkspaceUpdatedAt = s.now()
	return notice(surface, "room_workspace_detached", "已解除本群 workspace 绑定，并清理同群所有机器人的工作上下文。")
}

func (s *Service) syncFeishuRoomWorkspaceBinding(surface *state.SurfaceConsoleRecord, workspaceKey string) {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" {
		return
	}
	if normalizeWorkspaceClaimKey(room.WorkspaceKey) == workspaceKey {
		return
	}
	room.WorkspaceKey = workspaceKey
	room.WorkspaceUpdatedBy = strings.TrimSpace(surface.ActorUserID)
	room.WorkspaceUpdatedAt = s.now()
}

func (s *Service) blockFeishuRoomNoWorkspaceDataPlane(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return nil
	}
	if normalizeWorkspaceClaimKey(room.WorkspaceKey) != "" {
		return nil
	}
	if !feishuRoomActionRequiresWorkspace(action) {
		return nil
	}
	if feishuRoomNoWorkspaceTextCanOpenPicker(surface, room, action) {
		return nil
	}
	return roomWorkspaceRequiredNotice(surface)
}

func feishuRoomNoWorkspaceTextCanOpenPicker(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord, action control.Action) bool {
	if action.Kind != control.ActionTextMessage {
		return false
	}
	return primaryBotStateForSurface(surface, room) == control.CatalogPrimaryBotStateCurrent
}

func feishuRoomActionRequiresWorkspace(action control.Action) bool {
	switch action.Kind {
	case control.ActionTextMessage,
		control.ActionImageMessage,
		control.ActionFileMessage,
		control.ActionNewThread,
		control.ActionCompact,
		control.ActionSteerAll,
		control.ActionReviewCommand,
		control.ActionReviewStart,
		control.ActionReviewStartUncommitted,
		control.ActionReviewApply,
		control.ActionRespondRequest,
		control.ActionControlRequest:
		return true
	case control.ActionPlanProposalDecision:
		switch strings.TrimSpace(action.OptionID) {
		case planProposalActionExecute, planProposalActionExecuteNew:
			return true
		default:
			return false
		}
	default:
		return false
	}
}

func (s *Service) blockFeishuRoomNoWorkspaceAutoDispatch(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return nil
	}
	if normalizeWorkspaceClaimKey(room.WorkspaceKey) != "" {
		return nil
	}
	return roomWorkspaceRequiredNotice(surface)
}

func roomWorkspaceRequiredNotice(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	return notice(surface, "room_workspace_required", "当前群还没有绑定 workspace。请先使用 `/workspace` 选择或新建群 workspace。")
}

func (s *Service) blockUnsafeFeishuRoomWorkspaceReset(room *state.FeishuRoomContextRecord) string {
	for _, surface := range s.feishuRoomSurfaces(room.RoomID) {
		if surface == nil {
			continue
		}
		if surface.PendingHeadless != nil {
			return "当前群内有工作区正在启动，请等待完成后再切换。"
		}
		if surface.ActiveRequestCapture != nil || activePendingRequest(surface) != nil {
			return "当前群内有请求正在等待处理，请处理完成后再切换。"
		}
		if review := s.activeReviewSession(surface); review != nil && strings.TrimSpace(review.ActiveTurnID) != "" {
			return "当前群内有审阅请求正在执行，请等待完成后再切换。"
		}
		if surface.ActiveQueueItemID != "" {
			if item := surface.QueueItems[surface.ActiveQueueItemID]; item != nil {
				switch item.Status {
				case state.QueueItemDispatching, state.QueueItemRunning:
					return "当前群内有请求正在执行，请等待完成后再切换。"
				}
			}
		}
		if inst := s.root.Instances[surface.AttachedInstanceID]; inst != nil && strings.TrimSpace(inst.ActiveTurnID) != "" {
			return "当前群内有请求正在执行，请等待完成后再切换。"
		}
	}
	return ""
}

func (s *Service) blockFeishuRoomActiveDispatch(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return nil
	}
	if !s.feishuRoomActiveSlotAvailableForSurface(room, surface) {
		return notice(surface, "room_workspace_active", "当前群内已有机器人正在处理这个 workspace，请等待完成后再发送。")
	}
	return nil
}

func (s *Service) sameRoomWorkspaceIndependentContextAllowed(surface *state.SurfaceConsoleRecord, workspaceKey string) bool {
	if surface == nil || !s.surfaceIsHeadless(surface) {
		return false
	}
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return false
	}
	workspaceKey = normalizeWorkspaceClaimKey(workspaceKey)
	if workspaceKey == "" || normalizeWorkspaceClaimKey(room.WorkspaceKey) != workspaceKey {
		return false
	}
	return s.feishuRoomActiveSlotAvailable(room)
}

func (s *Service) instanceClaimedBySameRoomSibling(surface *state.SurfaceConsoleRecord, instanceID string) bool {
	owner := s.instanceClaimSurface(instanceID)
	if owner == nil || surface == nil || owner.SurfaceSessionID == surface.SurfaceSessionID {
		return false
	}
	roomID := surfaceFeishuRoomID(surface)
	return roomID != "" && surfaceFeishuRoomID(owner) == roomID
}

func (s *Service) blockFeishuRoomActiveAutoDispatch(surface *state.SurfaceConsoleRecord) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return nil
	}
	if s.feishuRoomActiveSlotAvailable(room) {
		return nil
	}
	if !s.allowActiveNotice("room_workspace_active", surface.SurfaceSessionID, room.RoomID, "", "", feishuRoomActiveAutoNoticeCooldown) {
		return nil
	}
	return notice(surface, "room_workspace_active", "当前群内已有机器人正在处理这个 workspace，请等待完成后再发送。")
}

func (s *Service) reconcileFeishuRoomActiveReservations(room *state.FeishuRoomContextRecord) {
	if room == nil {
		return
	}
	if room.ActiveReservations == nil {
		room.ActiveReservations = map[string]*state.FeishuRoomActiveReservationRecord{}
	}
	observed := map[string]*state.FeishuRoomActiveReservationRecord{}
	activeSurfaceIDs := map[string]bool{}
	for _, surface := range s.feishuRoomSurfaces(room.RoomID) {
		if surface == nil {
			continue
		}
		item := activeQueueItem(surface)
		if item != nil {
			switch item.Status {
			case state.QueueItemDispatching, state.QueueItemRunning:
				activeSurfaceIDs[strings.TrimSpace(surface.SurfaceSessionID)] = true
				reservationID := feishuRoomQueueReservationID(surface, item.ID)
				reason := "dispatching"
				if inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]; inst != nil && strings.TrimSpace(inst.ActiveTurnID) != "" {
					reason = "running"
				}
				observed[reservationID] = s.feishuRoomReservationRecord(room.ActiveReservations[reservationID], reservationID, surface, item, reason)
				continue
			}
		}
		inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
		if inst == nil || strings.TrimSpace(inst.ActiveTurnID) == "" {
			continue
		}
		activeSurfaceIDs[strings.TrimSpace(surface.SurfaceSessionID)] = true
		reservationID := feishuRoomTurnReservationID(surface, inst)
		observed[reservationID] = s.feishuRoomReservationRecord(room.ActiveReservations[reservationID], reservationID, surface, item, "running")
	}
	for reservationID, reservation := range room.ActiveReservations {
		if reservation == nil || reservationID == "" {
			continue
		}
		if _, ok := observed[reservationID]; ok {
			continue
		}
		reason := strings.TrimSpace(reservation.Reason)
		if strings.HasPrefix(reason, "headless_") && activeSurfaceIDs[strings.TrimSpace(reservation.SurfaceSessionID)] {
			continue
		}
		if strings.HasPrefix(reason, "review_") {
			surface := s.root.Surfaces[strings.TrimSpace(reservation.SurfaceSessionID)]
			if surface != nil && surface.ReviewSession != nil {
				observed[reservationID] = reservation
			}
			continue
		}
		if strings.HasPrefix(reason, "headless_") {
			observed[reservationID] = reservation
		}
	}
	room.ActiveReservations = observed
}

func feishuRoomQueueReservationID(surface *state.SurfaceConsoleRecord, queueItemID string) string {
	return "queue:" + strings.TrimSpace(surface.SurfaceSessionID) + ":" + strings.TrimSpace(queueItemID)
}

func feishuRoomTurnReservationID(surface *state.SurfaceConsoleRecord, inst *state.InstanceRecord) string {
	return "turn:" + strings.TrimSpace(surface.SurfaceSessionID) + ":" + strings.TrimSpace(inst.InstanceID) + ":" + strings.TrimSpace(inst.ActiveTurnID)
}

func (s *Service) feishuRoomReservationRecord(existing *state.FeishuRoomActiveReservationRecord, reservationID string, surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, reason string) *state.FeishuRoomActiveReservationRecord {
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	turnID := ""
	queueItemID := ""
	if item != nil {
		queueItemID = strings.TrimSpace(item.ID)
	}
	if inst != nil {
		if threadID == "" {
			threadID = strings.TrimSpace(inst.ActiveThreadID)
		}
		turnID = strings.TrimSpace(inst.ActiveTurnID)
	}
	if existing == nil {
		existing = &state.FeishuRoomActiveReservationRecord{ReservationID: reservationID}
	}
	existing.ReservationID = reservationID
	existing.SurfaceSessionID = strings.TrimSpace(surface.SurfaceSessionID)
	existing.InstanceID = strings.TrimSpace(surface.AttachedInstanceID)
	existing.ThreadID = threadID
	existing.TurnID = turnID
	existing.QueueItemID = queueItemID
	existing.Reason = strings.TrimSpace(reason)
	existing.UpdatedAt = s.now()
	return existing
}

func (s *Service) feishuRoomActiveReservationCount(room *state.FeishuRoomContextRecord) int {
	s.reconcileFeishuRoomActiveReservations(room)
	return len(room.ActiveReservations)
}

func (s *Service) feishuRoomActiveSlotAvailable(room *state.FeishuRoomContextRecord) bool {
	if room == nil {
		return true
	}
	limit := state.FeishuRoomConcurrencyLimit(room.ConcurrencyLimit)
	return limit == 0 || s.feishuRoomActiveReservationCount(room) < limit
}

func (s *Service) feishuRoomActiveSlotAvailableForSurface(room *state.FeishuRoomContextRecord, surface *state.SurfaceConsoleRecord) bool {
	if room == nil || surface == nil {
		return true
	}
	if s.feishuRoomActiveSlotAvailable(room) {
		return true
	}
	for _, reservation := range room.ActiveReservations {
		if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == strings.TrimSpace(surface.SurfaceSessionID) {
			return true
		}
	}
	return false
}

func (s *Service) reserveFeishuRoomActiveSlot(surface *state.SurfaceConsoleRecord, reason string) bool {
	return s.reserveFeishuRoomActiveSlotForQueueItem(surface, activeQueueItem(surface), reason)
}

func (s *Service) reserveFeishuRoomActiveSlotForQueueItem(surface *state.SurfaceConsoleRecord, item *state.QueueItemRecord, reason string) bool {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return true
	}
	s.reconcileFeishuRoomActiveReservations(room)
	reservationID := ""
	if item != nil {
		reservationID = feishuRoomQueueReservationID(surface, item.ID)
	}
	if reservationID != "" {
		if _, ok := room.ActiveReservations[reservationID]; ok {
			return true
		}
		for existingID, reservation := range room.ActiveReservations {
			if reservation == nil || strings.TrimSpace(reservation.SurfaceSessionID) != strings.TrimSpace(surface.SurfaceSessionID) || strings.TrimSpace(reservation.QueueItemID) != "" || !strings.HasPrefix(strings.TrimSpace(reservation.Reason), "headless_") {
				continue
			}
			delete(room.ActiveReservations, existingID)
			room.ActiveReservations[reservationID] = s.feishuRoomReservationRecord(reservation, reservationID, surface, item, reason)
			return true
		}
	} else {
		for _, reservation := range room.ActiveReservations {
			if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == strings.TrimSpace(surface.SurfaceSessionID) && strings.TrimSpace(reservation.QueueItemID) == "" && strings.TrimSpace(reservation.Reason) == strings.TrimSpace(reason) {
				return true
			}
		}
	}
	limit := state.FeishuRoomConcurrencyLimit(room.ConcurrencyLimit)
	if limit > 0 && len(room.ActiveReservations) >= limit {
		return false
	}
	if reservationID == "" {
		reservationID = fmt.Sprintf("dispatch:%s:%d", strings.TrimSpace(surface.SurfaceSessionID), s.now().UnixNano())
	}
	room.ActiveReservations[reservationID] = s.feishuRoomReservationRecord(nil, reservationID, surface, item, reason)
	return true
}

func (s *Service) feishuRoomHasReservation(surface *state.SurfaceConsoleRecord, reason string) bool {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return false
	}
	s.reconcileFeishuRoomActiveReservations(room)
	for _, reservation := range room.ActiveReservations {
		if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == strings.TrimSpace(surface.SurfaceSessionID) && strings.TrimSpace(reservation.QueueItemID) == "" && strings.TrimSpace(reservation.Reason) == strings.TrimSpace(reason) {
			return true
		}
	}
	return false
}

func (s *Service) feishuRoomActiveSlotAvailableAfterReviewRelease(surface *state.SurfaceConsoleRecord) bool {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return true
	}
	s.reconcileFeishuRoomActiveReservations(room)
	limit := state.FeishuRoomConcurrencyLimit(room.ConcurrencyLimit)
	if limit == 0 {
		return true
	}
	active := len(room.ActiveReservations)
	for _, reservation := range room.ActiveReservations {
		if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == strings.TrimSpace(surface.SurfaceSessionID) && strings.HasPrefix(strings.TrimSpace(reservation.Reason), "review_") {
			active--
		}
	}
	return active < limit
}

func (s *Service) releaseFeishuRoomActiveReservation(surface *state.SurfaceConsoleRecord, queueItemID string) {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return
	}
	queueItemID = strings.TrimSpace(queueItemID)
	for reservationID, reservation := range room.ActiveReservations {
		if reservation == nil || strings.TrimSpace(reservation.SurfaceSessionID) != strings.TrimSpace(surface.SurfaceSessionID) {
			continue
		}
		if queueItemID != "" && strings.TrimSpace(reservation.QueueItemID) != queueItemID {
			continue
		}
		delete(room.ActiveReservations, reservationID)
	}
}

func (s *Service) releaseFeishuRoomQueueReservations(surface *state.SurfaceConsoleRecord, queueItemID string) {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return
	}
	queueItemID = strings.TrimSpace(queueItemID)
	for reservationID, reservation := range room.ActiveReservations {
		if reservation == nil || strings.TrimSpace(reservation.SurfaceSessionID) != strings.TrimSpace(surface.SurfaceSessionID) || strings.TrimSpace(reservation.QueueItemID) == "" {
			continue
		}
		if queueItemID != "" && strings.TrimSpace(reservation.QueueItemID) != queueItemID {
			continue
		}
		delete(room.ActiveReservations, reservationID)
	}
}

func (s *Service) releaseFeishuRoomActiveReservationByReason(surface *state.SurfaceConsoleRecord, reason string) {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return
	}
	reason = strings.TrimSpace(reason)
	for reservationID, reservation := range room.ActiveReservations {
		if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == strings.TrimSpace(surface.SurfaceSessionID) && strings.TrimSpace(reservation.Reason) == reason {
			delete(room.ActiveReservations, reservationID)
		}
	}
}

func (s *Service) releaseFeishuRoomReviewReservations(surface *state.SurfaceConsoleRecord) {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return
	}
	for reservationID, reservation := range room.ActiveReservations {
		if reservation != nil && strings.TrimSpace(reservation.SurfaceSessionID) == strings.TrimSpace(surface.SurfaceSessionID) && strings.HasPrefix(strings.TrimSpace(reservation.Reason), "review_") {
			delete(room.ActiveReservations, reservationID)
		}
	}
}

func (s *Service) FeishuRoomActiveCount(surfaceID string) int {
	if s == nil || s.root == nil {
		return 0
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return 0
	}
	return s.feishuRoomActiveReservationCount(room)
}

func (s *Service) FeishuRoomConcurrencyLimit(surfaceID string) *int {
	if s == nil || s.root == nil {
		return nil
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return nil
	}
	room := s.root.FeishuRoomContexts[surfaceFeishuRoomID(surface)]
	if room == nil {
		return nil
	}
	return state.CloneFeishuRoomConcurrencyLimit(room.ConcurrencyLimit)
}

func (s *Service) RestoreFeishuRoomConcurrencyLimit(surfaceID string, value *int) {
	if s == nil || s.root == nil {
		return
	}
	surface := s.root.Surfaces[strings.TrimSpace(surfaceID)]
	if surface == nil {
		return
	}
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return
	}
	room.ConcurrencyLimit = state.CloneFeishuRoomConcurrencyLimit(value)
}

func (s *Service) ReleaseFeishuRoomGroupOnDemandReservation(surfaceID string) {
	if s == nil || s.root == nil {
		return
	}
	s.releaseFeishuRoomActiveReservationByReason(s.root.Surfaces[strings.TrimSpace(surfaceID)], feishuRoomGroupOnDemandReservationReason)
}

func (s *Service) reserveFeishuRoomGroupOnDemandSlot(surface *state.SurfaceConsoleRecord) (bool, []eventcontract.Event) {
	if surface == nil || !surfaceIsFeishuChat(surface) {
		return true, nil
	}
	if s.feishuRoomHasReservation(surface, feishuRoomGroupOnDemandReservationReason) {
		return true, nil
	}
	if blocked := s.blockFeishuRoomActiveAutoDispatch(surface); blocked != nil {
		return false, blocked
	}
	if !s.reserveFeishuRoomActiveSlot(surface, feishuRoomGroupOnDemandReservationReason) {
		return false, s.blockFeishuRoomActiveAutoDispatch(surface)
	}
	return true, nil
}

func activeQueueItem(surface *state.SurfaceConsoleRecord) *state.QueueItemRecord {
	if surface == nil || strings.TrimSpace(surface.ActiveQueueItemID) == "" {
		return nil
	}
	return surface.QueueItems[strings.TrimSpace(surface.ActiveQueueItemID)]
}

func (s *Service) resetFeishuRoomWorkspaceSurfaces(room *state.FeishuRoomContextRecord, keep *state.SurfaceConsoleRecord) {
	if room != nil {
		room.ActiveReservations = map[string]*state.FeishuRoomActiveReservationRecord{}
	}
	for _, surface := range s.feishuRoomSurfaces(room.RoomID) {
		if surface == nil || surface == keep {
			continue
		}
		_ = s.finalizeDetachedSurface(surface)
		surface.QueueItems = map[string]*state.QueueItemRecord{}
		surface.QueuedQueueItemIDs = nil
		surface.StagedImages = map[string]*state.StagedImageRecord{}
		surface.StagedFiles = map[string]*state.StagedFileRecord{}
		clearSurfaceRequests(surface)
		surface.ActiveRequestCapture = nil
		surface.ActiveExecProgress = nil
		surface.ActiveReasoning = nil
		s.releaseFeishuRoomReviewReservations(surface)
		s.clearPendingReviewStart(surface)
		surface.ReviewSession = nil
		s.clearPlanProposalRuntime(surface)
		s.clearTargetPickerRuntime(surface)
	}
}

func surfaceIsFeishuChat(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil || strings.TrimSpace(surface.ChatID) == "" {
		return false
	}
	if surface.Platform != "" && surface.Platform != "feishu" {
		return false
	}
	ref, ok := feishuidentity.ParseSurfaceRef(surface.SurfaceSessionID)
	return ok && ref.IsChat()
}
