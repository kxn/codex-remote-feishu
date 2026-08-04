package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type fakePrimaryPermissionChecker struct {
	decision PrimaryBotPermissionDecision
	requests []PrimaryBotPermissionRequest
}

func (f *fakePrimaryPermissionChecker) CheckPrimaryBotPermission(_ context.Context, req PrimaryBotPermissionRequest) PrimaryBotPermissionDecision {
	f.requests = append(f.requests, req)
	return f.decision
}

func TestPrimaryCommandSingleChatNoops(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	checker := &fakePrimaryPermissionChecker{decision: PrimaryBotPermissionDecision{Allowed: true, Scope: "im:message.group_msg"}}
	svc := newServiceForTest(&now)
	svc.config.PrimaryBotPermissionChecker = checker

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:user:ou_user",
		GatewayID:        "app-1",
		ChatID:           "ou_user",
		ActorUserID:      "ou_user",
		Text:             "/primary on",
	})

	if len(checker.requests) != 0 {
		t.Fatalf("single chat should not check primary permissions: %#v", checker.requests)
	}
	if len(svc.root.FeishuRoomContexts) != 0 {
		t.Fatalf("single chat should not create room primary state: %#v", svc.root.FeishuRoomContexts)
	}
	if got := noticeText(events); !strings.Contains(got, "只能在群聊中") {
		t.Fatalf("single chat primary notice = %q", got)
	}
}

func TestPrimaryCommandOnSetsCurrentGatewayWhenPermissionAllowed(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	checker := &fakePrimaryPermissionChecker{decision: PrimaryBotPermissionDecision{Allowed: true, Scope: "im:message.group_msg"}}
	svc := newServiceForTest(&now)
	svc.config.PrimaryBotPermissionChecker = checker

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/primary on",
	})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room == nil || room.PrimaryGatewayID != "app-1" || room.PrimaryUpdatedBy != "ou_user" || !room.PrimaryUpdatedAt.Equal(now) {
		t.Fatalf("primary room state = %#v, want app-1/ou_user/%v", room, now)
	}
	if len(checker.requests) != 1 || !checker.requests[0].ForceRefresh || checker.requests[0].GatewayID != "app-1" || checker.requests[0].ChatID != "oc_room" {
		t.Fatalf("permission requests = %#v, want forced app-1/oc_room", checker.requests)
	}
	if got := noticeText(events); !strings.Contains(got, "已将当前机器人设置为本群主机器人") {
		t.Fatalf("primary on notice = %q", got)
	}
}

func TestPrimaryCommandOnRejectsMissingPermission(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	checker := &fakePrimaryPermissionChecker{decision: PrimaryBotPermissionDecision{Allowed: false, Reason: "missing"}}
	svc := newServiceForTest(&now)
	svc.config.PrimaryBotPermissionChecker = checker

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/primary on",
	})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room != nil && room.PrimaryGatewayID != "" {
		t.Fatalf("missing permission should not set primary: %#v", room)
	}
	if got := noticeText(events); !strings.Contains(got, "还不能接收群普通消息") {
		t.Fatalf("missing permission notice = %q", got)
	}
}

func TestPrimaryCommandOnUsesPrimaryPermissionCheck(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	checker := &fakePrimaryPermissionChecker{decision: PrimaryBotPermissionDecision{Allowed: true, Scope: "im:message.group_msg"}}
	svc := newServiceForTest(&now)
	svc.config.PrimaryBotPermissionChecker = checker

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_member",
		Text:             "/primary on",
	})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room == nil || room.PrimaryGatewayID != "app-1" {
		t.Fatalf("primary on should use the primary permission check without chat-admin gating: %#v", room)
	}
	if len(checker.requests) != 1 || !checker.requests[0].ForceRefresh {
		t.Fatalf("primary permission requests = %#v, want one forced check", checker.requests)
	}
	if got := noticeText(events); strings.Contains(got, "群管理员") {
		t.Fatalf("primary on should not mention chat admin authorization: %q", got)
	}
}

func TestPrimaryCommandOnReplacesPreviousGateway(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	checker := &fakePrimaryPermissionChecker{decision: PrimaryBotPermissionDecision{Allowed: true, Scope: "im:message.group_msg"}}
	svc := newServiceForTest(&now)
	svc.config.PrimaryBotPermissionChecker = checker
	svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		PrimaryGatewayID: "app-old",
	}})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/primary on",
	})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room.PrimaryGatewayID != "app-1" {
		t.Fatalf("primary gateway = %q, want app-1", room.PrimaryGatewayID)
	}
	if got := noticeText(events); !strings.Contains(got, "切换为当前机器人") {
		t.Fatalf("replace notice = %q", got)
	}
}

func TestPrimaryCommandOffClearsOnlyCurrentGateway(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeFeishuRoomState([]state.FeishuRoomStateRecord{{
		RoomID:           "feishu:chat:oc_room",
		ChatID:           "oc_room",
		PrimaryGatewayID: "app-1",
	}})

	events := svc.ApplySurfaceAction(control.Action{
		Kind:             control.ActionPrimaryCommand,
		SurfaceSessionID: "feishu:app-1:chat:oc_room",
		GatewayID:        "app-1",
		ChatID:           "oc_room",
		ActorUserID:      "ou_user",
		Text:             "/primary off",
	})

	room := svc.root.FeishuRoomContexts["feishu:chat:oc_room"]
	if room.PrimaryGatewayID != "" {
		t.Fatalf("primary gateway = %q, want cleared", room.PrimaryGatewayID)
	}
	if got := noticeText(events); !strings.Contains(got, "已取消本群主机器人") {
		t.Fatalf("off notice = %q", got)
	}
}

func noticeText(events []eventcontract.Event) string {
	for _, event := range events {
		if event.Notice != nil {
			return event.Notice.Text
		}
	}
	return ""
}
