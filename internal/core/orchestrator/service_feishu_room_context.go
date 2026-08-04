package orchestrator

import (
	"sort"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/feishuidentity"
)

const feishuRoomActiveAutoNoticeCooldown = time.Minute

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
			RoomID:            roomID,
			ChatID:            strings.TrimSpace(surface.ChatID),
			GatewayIDs:        map[string]bool{},
			SurfaceSessionIDs: map[string]bool{},
		}
		s.root.FeishuRoomContexts[roomID] = room
	}
	if room.GatewayIDs == nil {
		room.GatewayIDs = map[string]bool{}
	}
	if room.SurfaceSessionIDs == nil {
		room.SurfaceSessionIDs = map[string]bool{}
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
	s.resetFeishuRoomWorkspaceSurfaces(room, surface)
	room.WorkspaceResetGeneration++
	room.WorkspaceKey = ""
	return nil
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
	if holder := s.feishuRoomActiveLockHolder(room, surface); holder != nil {
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
	return s.feishuRoomActiveLockHolder(room, surface) == nil
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
	holder := s.feishuRoomActiveLockHolder(room, surface)
	if holder == nil {
		return nil
	}
	if !s.allowActiveNotice("room_workspace_active", surface.SurfaceSessionID, room.RoomID, holder.SurfaceSessionID, "", feishuRoomActiveAutoNoticeCooldown) {
		return nil
	}
	return notice(surface, "room_workspace_active", "当前群内已有机器人正在处理这个 workspace，请等待完成后再发送。")
}

func (s *Service) feishuRoomActiveLockHolder(room *state.FeishuRoomContextRecord, current *state.SurfaceConsoleRecord) *state.SurfaceConsoleRecord {
	if room == nil {
		return nil
	}
	if room.ActiveLock != nil {
		lockSurfaceID := strings.TrimSpace(room.ActiveLock.SurfaceSessionID)
		if current == nil || lockSurfaceID != strings.TrimSpace(current.SurfaceSessionID) {
			if holder := s.root.Surfaces[lockSurfaceID]; s.surfaceHasRoomActiveWork(holder) {
				return holder
			}
			room.ActiveLock = nil
		}
	}
	for _, candidate := range s.feishuRoomSurfaces(room.RoomID) {
		if candidate == nil || current != nil && candidate.SurfaceSessionID == current.SurfaceSessionID {
			continue
		}
		if s.surfaceHasRoomActiveWork(candidate) {
			s.refreshFeishuRoomActiveLock(room, candidate, "observed_active")
			return candidate
		}
	}
	return nil
}

func (s *Service) surfaceHasRoomActiveWork(surface *state.SurfaceConsoleRecord) bool {
	if surface == nil {
		return false
	}
	if item := activeQueueItem(surface); item != nil {
		switch item.Status {
		case state.QueueItemDispatching, state.QueueItemRunning:
			return true
		}
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	return inst != nil && strings.TrimSpace(inst.ActiveTurnID) != ""
}

func (s *Service) refreshFeishuRoomActiveLock(room *state.FeishuRoomContextRecord, surface *state.SurfaceConsoleRecord, reason string) {
	if room == nil || surface == nil {
		return
	}
	item := activeQueueItem(surface)
	queueItemID := ""
	if item != nil {
		queueItemID = strings.TrimSpace(item.ID)
	}
	inst := s.root.Instances[strings.TrimSpace(surface.AttachedInstanceID)]
	threadID := strings.TrimSpace(surface.SelectedThreadID)
	turnID := ""
	if inst != nil {
		if threadID == "" {
			threadID = strings.TrimSpace(inst.ActiveThreadID)
		}
		turnID = strings.TrimSpace(inst.ActiveTurnID)
	}
	room.ActiveLock = &state.FeishuRoomActiveLockRecord{
		SurfaceSessionID: strings.TrimSpace(surface.SurfaceSessionID),
		InstanceID:       strings.TrimSpace(surface.AttachedInstanceID),
		ThreadID:         threadID,
		TurnID:           turnID,
		QueueItemID:      queueItemID,
		Reason:           strings.TrimSpace(reason),
		UpdatedAt:        s.now(),
	}
}

func (s *Service) releaseFeishuRoomActiveLock(surface *state.SurfaceConsoleRecord, queueItemID string) {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil || room.ActiveLock == nil {
		return
	}
	if strings.TrimSpace(room.ActiveLock.SurfaceSessionID) != strings.TrimSpace(surface.SurfaceSessionID) {
		return
	}
	queueItemID = strings.TrimSpace(queueItemID)
	if queueItemID != "" && strings.TrimSpace(room.ActiveLock.QueueItemID) != queueItemID {
		return
	}
	room.ActiveLock = nil
}

func activeQueueItem(surface *state.SurfaceConsoleRecord) *state.QueueItemRecord {
	if surface == nil || strings.TrimSpace(surface.ActiveQueueItemID) == "" {
		return nil
	}
	return surface.QueueItems[strings.TrimSpace(surface.ActiveQueueItemID)]
}

func (s *Service) resetFeishuRoomWorkspaceSurfaces(room *state.FeishuRoomContextRecord, keep *state.SurfaceConsoleRecord) {
	if room != nil {
		room.ActiveLock = nil
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
