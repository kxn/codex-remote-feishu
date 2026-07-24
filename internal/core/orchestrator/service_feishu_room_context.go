package orchestrator

import (
	"context"
	"sort"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

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
	authorizer := s.config.ChatAdminAuthorizer
	if authorizer == nil {
		return notice(surface, "room_workspace_admin_required", "无法确认群管理员身份，不能切换群 workspace。")
	}
	decision := authorizer.AuthorizeChatAdmin(context.Background(), ChatAdminAuthorizationRequest{
		GatewayID:   strings.TrimSpace(surface.GatewayID),
		ChatID:      strings.TrimSpace(room.ChatID),
		ActorOpenID: strings.TrimSpace(surface.ActorUserID),
	})
	if !decision.Allowed {
		return notice(surface, "room_workspace_admin_required", "只有群管理员可以切换群 workspace。")
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

func (s *Service) resetFeishuRoomWorkspaceSurfaces(room *state.FeishuRoomContextRecord, keep *state.SurfaceConsoleRecord) {
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
	parts := strings.Split(strings.TrimSpace(surface.SurfaceSessionID), ":")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "chat" && strings.TrimSpace(parts[i+1]) != "" {
			return true
		}
	}
	return false
}
