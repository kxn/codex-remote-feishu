package orchestrator

import (
	"context"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func (s *Service) handlePrimaryCommand(surface *state.SurfaceConsoleRecord, action control.Action) []eventcontract.Event {
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return notice(surface, "primary_group_required", "`/primary` 只能在群聊中设置本群主机器人。")
	}
	switch primaryCommandArgument(action.Text) {
	case "on":
		return s.handlePrimaryOn(surface, room, action)
	case "off":
		return s.handlePrimaryOff(surface, room)
	case "refresh":
		return s.handlePrimaryRefresh(surface, room, action)
	default:
		return s.handlePrimaryStatus(surface, room)
	}
}

func (s *Service) handlePrimaryOn(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord, action control.Action) []eventcontract.Event {
	decision := s.checkPrimaryBotPermission(surface, action, true)
	if !decision.Allowed {
		return notice(surface, "primary_permission_missing", "当前机器人还不能接收群普通消息，请先给这个机器人应用开通群消息权限后再设置。")
	}
	previous := strings.TrimSpace(room.PrimaryGatewayID)
	current := strings.TrimSpace(surface.GatewayID)
	if current == "" {
		return notice(surface, "primary_gateway_missing", "当前机器人身份不完整，暂时不能设置为本群主机器人。")
	}
	room.PrimaryGatewayID = current
	room.PrimaryUpdatedBy = strings.TrimSpace(action.ActorUserID)
	if room.PrimaryUpdatedBy == "" {
		room.PrimaryUpdatedBy = strings.TrimSpace(surface.ActorUserID)
	}
	room.PrimaryUpdatedAt = s.now()
	switch {
	case previous == "":
		return notice(surface, "primary_set", "已将当前机器人设置为本群主机器人。之后未 @ 的普通消息将由它承接。")
	case previous == current:
		return notice(surface, "primary_already_current", "当前机器人已经是本群主机器人。")
	default:
		return notice(surface, "primary_replaced", "已将本群主机器人切换为当前机器人。之后未 @ 的普通消息将由当前机器人承接。")
	}
}

func (s *Service) SetFeishuPrimaryGatewayIfEmpty(action control.Action, now time.Time) bool {
	if s == nil {
		return false
	}
	surface := s.ensureSurface(action)
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return false
	}
	current := strings.TrimSpace(surface.GatewayID)
	if current == "" || strings.TrimSpace(room.PrimaryGatewayID) != "" {
		return false
	}
	room.PrimaryGatewayID = current
	room.PrimaryUpdatedBy = strings.TrimSpace(action.ActorUserID)
	if room.PrimaryUpdatedBy == "" {
		room.PrimaryUpdatedBy = strings.TrimSpace(surface.ActorUserID)
	}
	room.PrimaryUpdatedAt = now
	return true
}

func (s *Service) ClearFeishuPrimaryGatewayIfMatches(action control.Action) bool {
	if s == nil || s.root == nil {
		return false
	}
	surface := s.root.Surfaces[strings.TrimSpace(action.SurfaceSessionID)]
	room := s.ensureFeishuRoomContextForSurface(surface)
	if room == nil {
		return false
	}
	current := strings.TrimSpace(action.GatewayID)
	if current == "" || strings.TrimSpace(room.PrimaryGatewayID) != current {
		return false
	}
	room.PrimaryGatewayID = ""
	room.PrimaryUpdatedBy = strings.TrimSpace(action.ActorUserID)
	room.PrimaryUpdatedAt = s.now()
	return true
}

func (s *Service) handlePrimaryOff(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord) []eventcontract.Event {
	current := strings.TrimSpace(surface.GatewayID)
	if strings.TrimSpace(room.PrimaryGatewayID) != current || current == "" {
		return notice(surface, "primary_not_current", "当前机器人不是本群主机器人，未做修改。")
	}
	room.PrimaryGatewayID = ""
	room.PrimaryUpdatedBy = strings.TrimSpace(surface.ActorUserID)
	room.PrimaryUpdatedAt = s.now()
	return notice(surface, "primary_cleared", "已取消本群主机器人。之后未 @ 的普通消息不会被机器人承接。")
}

func (s *Service) handlePrimaryStatus(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord) []eventcontract.Event {
	switch primaryBotStateForSurface(surface, room) {
	case control.CatalogPrimaryBotStateCurrent:
		return notice(surface, "primary_status", "当前机器人是本群主机器人。")
	case control.CatalogPrimaryBotStateOther:
		return notice(surface, "primary_status", "本群已有其他主机器人。")
	default:
		return notice(surface, "primary_status", "本群还没有主机器人。")
	}
}

func (s *Service) handlePrimaryRefresh(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord, action control.Action) []eventcontract.Event {
	_ = room
	decision := s.checkPrimaryBotPermission(surface, action, true)
	if decision.Allowed {
		return notice(surface, "primary_permission_refreshed", "已刷新权限状态：当前机器人可以接收群普通消息。")
	}
	return notice(surface, "primary_permission_missing", "已刷新权限状态：当前机器人还不能接收群普通消息。")
}

func (s *Service) checkPrimaryBotPermission(surface *state.SurfaceConsoleRecord, action control.Action, forceRefresh bool) PrimaryBotPermissionDecision {
	if s == nil || s.config.PrimaryBotPermissionChecker == nil || surface == nil {
		return PrimaryBotPermissionDecision{Allowed: false, Reason: "checker_unavailable"}
	}
	return s.config.PrimaryBotPermissionChecker.CheckPrimaryBotPermission(context.Background(), PrimaryBotPermissionRequest{
		GatewayID:    strings.TrimSpace(surface.GatewayID),
		ChatID:       strings.TrimSpace(surface.ChatID),
		ActorOpenID:  strings.TrimSpace(action.ActorUserID),
		ForceRefresh: forceRefresh,
	})
}

func primaryCommandArgument(text string) string {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 2 {
		return "status"
	}
	switch strings.ToLower(strings.TrimSpace(fields[1])) {
	case "on", "off", "status", "refresh":
		return strings.ToLower(strings.TrimSpace(fields[1]))
	default:
		return "status"
	}
}

func primaryBotStateForSurface(surface *state.SurfaceConsoleRecord, room *state.FeishuRoomContextRecord) control.CatalogPrimaryBotState {
	if room == nil || strings.TrimSpace(room.PrimaryGatewayID) == "" {
		return control.CatalogPrimaryBotStateNone
	}
	if surface != nil && strings.TrimSpace(surface.GatewayID) == strings.TrimSpace(room.PrimaryGatewayID) {
		return control.CatalogPrimaryBotStateCurrent
	}
	return control.CatalogPrimaryBotStateOther
}
