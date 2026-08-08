package daemon

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

const (
	feishuPrimaryBootstrapFeature = "primary_room_auto_bootstrap"

	feishuPrimaryBootstrapSuccessText = "当前机器人是群内唯一机器人，已自动成为本群主机器人。之后未 @ 的普通消息将由它承接，可用 /primary off 取消。"
	feishuPrimaryBootstrapScopeText   = "当前机器人已进群，但还不能自动设置为本群主机器人：请在飞书开放平台开通“获取群组信息”权限，并确认已订阅机器人进群事件后再发布应用。"
)

type feishuChatInfoReader interface {
	GetChatInfo(context.Context, string) (feishu.ChatInfo, error)
}

func (a *App) handleFeishuBotAddedToGroup(ctx context.Context, action control.Action) *feishu.ActionResult {
	action.GatewayID = canonicalGatewayID(action.GatewayID)
	action.ChatID = strings.TrimSpace(action.ChatID)
	action.SurfaceSessionID = strings.TrimSpace(action.SurfaceSessionID)
	if action.GatewayID == "" || action.ChatID == "" || action.SurfaceSessionID == "" {
		return nil
	}

	decision := a.checkFeishuScopePermission(ctx, action.GatewayID, feishuPrimaryBootstrapFeature, false)
	if !decision.Allowed {
		a.deliverFeishuBotAddedText(ctx, action, feishuPrimaryBootstrapScopeText)
		return nil
	}

	reader, ok := a.gateway.(feishuChatInfoReader)
	if !ok {
		log.Printf("feishu bot-added primary bootstrap skipped: gateway does not expose chat info reader")
		return nil
	}
	info, err := reader.GetChatInfo(ctx, action.ChatID)
	if err != nil {
		if a.observeFeishuPermissionError(action.GatewayID, err) {
			a.deliverFeishuBotAddedText(ctx, action, feishuPrimaryBootstrapScopeText)
		} else {
			log.Printf("feishu bot-added primary bootstrap chat info failed: gateway=%s chat=%s err=%v", action.GatewayID, action.ChatID, err)
		}
		return nil
	}
	if strings.ToLower(strings.TrimSpace(info.ChatMode)) != "group" || info.BotCount != 1 {
		return nil
	}

	a.mu.Lock()
	if a.shuttingDown {
		a.mu.Unlock()
		return nil
	}
	updated := a.service.SetFeishuPrimaryGatewayIfEmpty(action, time.Now().UTC())
	if updated {
		if err := a.syncFeishuRoomStateLocked(); err != nil {
			log.Printf("feishu bot-added primary bootstrap persist failed: gateway=%s chat=%s err=%v", action.GatewayID, action.ChatID, err)
			a.service.ClearFeishuPrimaryGatewayIfMatches(action)
			a.refreshFeishuPrimaryGatewaySnapshotLocked()
			updated = false
		}
	}
	a.mu.Unlock()

	if updated {
		a.deliverFeishuBotAddedText(ctx, action, feishuPrimaryBootstrapSuccessText)
	}
	return nil
}

func (a *App) deliverFeishuBotAddedText(ctx context.Context, action control.Action, text string) {
	text = strings.TrimSpace(text)
	if text == "" || a == nil || a.gateway == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := a.gateway.Apply(ctx, []feishu.Operation{{
		Kind:             feishu.OperationSendText,
		GatewayID:        action.GatewayID,
		SurfaceSessionID: action.SurfaceSessionID,
		ChatID:           action.ChatID,
		ReceiveIDType:    "chat_id",
		ReceiveID:        action.ChatID,
		Text:             text,
	}})
	if err != nil {
		log.Printf("feishu bot-added primary bootstrap notice failed: gateway=%s chat=%s err=%v", action.GatewayID, action.ChatID, err)
	}
}
