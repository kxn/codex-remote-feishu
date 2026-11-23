package feishu

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type MenuIngressEvent = platformingress.MenuIngressEvent
type PlatformIngressEvent = platformingress.PlatformIngressEvent

func (g *LiveGateway) teamEventIdentity(base *larkevent.EventV2Base) (PlatformIngressEvent, error) {
	var value PlatformIngressEvent
	if base == nil || base.Header == nil {
		return value, fmt.Errorf("platform event lacks verified header")
	}
	h := base.Header
	if h.AppID != "" && h.AppID != g.config.AppID {
		return value, fmt.Errorf("platform event app mismatch")
	}
	if strings.TrimSpace(h.EventID) == "" || strings.TrimSpace(h.TenantKey) == "" {
		return value, fmt.Errorf("platform event lacks event or tenant identity")
	}
	return PlatformIngressEvent{GatewayRef: g.config.GatewayID, AppID: g.config.AppID, EventID: h.EventID, Tenant: h.TenantKey, ReceivedAt: time.Now().UTC()}, nil
}
func (g *LiveGateway) handleTeamMenu(ctx context.Context, event *larkapplication.P2BotMenuV6) (bool, error) {
	g.mu.Lock()
	hook := g.rawIngressHook
	g.mu.Unlock()
	if hook == nil {
		return false, nil
	}
	if event == nil || event.Event == nil {
		return true, fmt.Errorf("menu event missing payload")
	}
	base, err := g.teamEventIdentity(event.EventV2Base)
	if err != nil {
		return true, err
	}
	data := event.Event
	if data.Operator == nil || data.Operator.OperatorId == nil {
		return true, fmt.Errorf("menu event missing actual operator")
	}
	ids := data.Operator.OperatorId
	sender := platformingress.SenderIDs{OpenID: xutil.StringValue(ids.OpenId), UserID: xutil.StringValue(ids.UserId), UnionID: xutil.StringValue(ids.UnionId)}
	key := strings.TrimSpace(xutil.StringValue(data.EventKey))
	if sender.OpenID == "" && sender.UserID == "" && sender.UnionID == "" || key == "" {
		return true, fmt.Errorf("menu event missing operator or event key")
	}
	if hook.ReceiveMenu == nil {
		return true, fmt.Errorf("durable menu receiver unavailable")
	}
	value := MenuIngressEvent{GatewayRef: base.GatewayRef, AppID: base.AppID, EventID: base.EventID, Tenant: base.Tenant, SenderIDs: sender, EventKey: key, ReceivedAt: base.ReceivedAt}
	if data.Timestamp != nil && *data.Timestamp > 0 {
		value.ClickedAt = gatewaypkg.InboundMetaFromMenuEvent(event).MenuClickTime
	}
	return true, hook.ReceiveMenu(ctx, value)
}
func eventMillis(value *string) time.Time {
	millis, err := strconv.ParseInt(xutil.StringValue(value), 10, 64)
	if err != nil || millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}
func (g *LiveGateway) receiveTeamPlatform(ctx context.Context, base *larkevent.EventV2Base, fill func(*PlatformIngressEvent) error) (bool, error) {
	g.mu.Lock()
	hook := g.rawIngressHook
	g.mu.Unlock()
	if hook == nil {
		return false, nil
	}
	value, err := g.teamEventIdentity(base)
	if err != nil {
		return true, err
	}
	if err = fill(&value); err != nil {
		return true, err
	}
	if hook.ReceivePlatformEvent == nil {
		return true, fmt.Errorf("durable platform event receiver unavailable")
	}
	return true, hook.ReceivePlatformEvent(ctx, value)
}
func (g *LiveGateway) handleTeamRecall(ctx context.Context, event *larkim.P2MessageRecalledV1) (bool, error) {
	var base *larkevent.EventV2Base
	if event != nil {
		base = event.EventV2Base
	}
	return g.receiveTeamPlatform(ctx, base, func(v *PlatformIngressEvent) error {
		if event == nil || event.Event == nil {
			return fmt.Errorf("recall event missing payload")
		}
		d := event.Event
		v.Kind = "message.recalled"
		v.ChatID = xutil.StringValue(d.ChatId)
		v.MessageID = xutil.StringValue(d.MessageId)
		v.RecallType = xutil.StringValue(d.RecallType)
		v.OccurredAt = eventMillis(d.RecallTime)
		if v.ChatID == "" || v.MessageID == "" {
			return fmt.Errorf("recall event missing message/chat")
		}
		return nil
	})
}
func (g *LiveGateway) handleTeamReaction(ctx context.Context, event *larkim.P2MessageReactionCreatedV1) (bool, error) {
	var base *larkevent.EventV2Base
	if event != nil {
		base = event.EventV2Base
	}
	return g.receiveTeamPlatform(ctx, base, func(v *PlatformIngressEvent) error {
		if event == nil || event.Event == nil {
			return fmt.Errorf("reaction event missing payload")
		}
		d := event.Event
		v.Kind = "reaction.created"
		v.MessageID = xutil.StringValue(d.MessageId)
		v.OperatorType = xutil.StringValue(d.OperatorType)
		v.OperatorIDs = rawSenderIDs(d.UserId)
		v.OperatorAppID = xutil.StringValue(d.AppId)
		v.OccurredAt = eventMillis(d.ActionTime)
		if d.ReactionType != nil {
			v.Emoji = xutil.StringValue(d.ReactionType.EmojiType)
		}
		if v.MessageID == "" || v.Emoji == "" || (v.OperatorType != "user" && v.OperatorType != "app") {
			return fmt.Errorf("reaction event missing message/emoji/operator")
		}
		if v.OperatorType == "app" && v.OperatorAppID == "" || v.OperatorType == "user" && v.OperatorIDs == (platformingress.SenderIDs{}) {
			return fmt.Errorf("reaction event missing actual operator")
		}
		return nil
	})
}
func (g *LiveGateway) handleTeamBotAdded(ctx context.Context, event *larkim.P2ChatMemberBotAddedV1) (bool, error) {
	var base *larkevent.EventV2Base
	if event != nil {
		base = event.EventV2Base
	}
	return g.receiveTeamPlatform(ctx, base, func(v *PlatformIngressEvent) error {
		if event == nil || event.Event == nil {
			return fmt.Errorf("bot-added event missing payload")
		}
		d := event.Event
		v.Kind = "bot.added"
		v.ChatID = xutil.StringValue(d.ChatId)
		v.OperatorType = "user"
		v.OperatorIDs = rawSenderIDs(d.OperatorId)
		v.OperatorTenant = xutil.StringValue(d.OperatorTenantKey)
		v.External = d.External
		if v.ChatID == "" {
			return fmt.Errorf("bot-added event missing chat")
		}
		return nil
	})
}

func (g *LiveGateway) handleTeamBotRemoved(ctx context.Context, event *larkim.P2ChatMemberBotDeletedV1) (bool, error) {
	var base *larkevent.EventV2Base
	if event != nil {
		base = event.EventV2Base
	}
	return g.receiveTeamPlatform(ctx, base, func(v *PlatformIngressEvent) error {
		if event == nil || event.Event == nil {
			return fmt.Errorf("bot-removed event missing payload")
		}
		d := event.Event
		v.Kind = "bot.removed"
		v.ChatID = xutil.StringValue(d.ChatId)
		v.OperatorType = "user"
		v.OperatorIDs = rawSenderIDs(d.OperatorId)
		v.OperatorTenant = xutil.StringValue(d.OperatorTenantKey)
		v.External = d.External
		if v.ChatID == "" {
			return fmt.Errorf("bot-removed event missing chat")
		}
		return nil
	})
}
func (g *LiveGateway) handleTeamChatDeleted(ctx context.Context, event *larkim.P2ChatDisbandedV1) (bool, error) {
	var base *larkevent.EventV2Base
	if event != nil {
		base = event.EventV2Base
	}
	return g.receiveTeamPlatform(ctx, base, func(v *PlatformIngressEvent) error {
		if event == nil || event.Event == nil {
			return fmt.Errorf("chat-deleted event missing payload")
		}
		d := event.Event
		v.Kind = "chat.deleted"
		v.ChatID = xutil.StringValue(d.ChatId)
		v.OperatorType = "user"
		v.OperatorIDs = rawSenderIDs(d.OperatorId)
		v.OperatorTenant = xutil.StringValue(d.OperatorTenantKey)
		v.External = d.External
		if v.ChatID == "" {
			return fmt.Errorf("chat-deleted event missing chat")
		}
		return nil
	})
}
