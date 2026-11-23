package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"strconv"
	"strings"
	"time"
)

// RawIngressHook is registered only after the daemon's single-writer cutover.
// Authorize must apply the existing gateway admission/access policy. A nil policy
// cannot silently widen access during migration. Receive must durably commit.
type InteractionResponse struct{ RawBody json.RawMessage }

type RawIngressHook struct {
	startupRouterID    string
	startupNonce       string
	Authorize          func(context.Context, platformingress.RawIngressEnvelope) (bool, error)
	Receive            func(context.Context, platformingress.RawIngressEnvelope) error
	ReceiveInteraction func(context.Context, platformingress.HumanInteractionEnvelope) (string, error)
	// ReceiveInteractionResponse returns the exact previously committed callback body.
	// Existing team hooks may continue using ReceiveInteraction.
	ReceiveInteractionResponse func(context.Context, platformingress.HumanInteractionEnvelope) (InteractionResponse, error)
	// Menu has no SDK chat/message ID. Its receiver authenticates the actual
	// operator and resolves the private destination without manufacturing a message.
	ReceiveMenu func(context.Context, MenuIngressEvent) error
	// Platform events are observations, never inferred human instructions.
	ReceivePlatformEvent func(context.Context, PlatformIngressEvent) error
}

func (g *LiveGateway) SetRawIngressHook(hook RawIngressHook) error {
	if g == nil || hook.Authorize == nil || hook.Receive == nil {
		return fmt.Errorf("durable ingress requires access policy and synchronous receiver")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if hook.startupRouterID != "" || g.rawIngressHook != nil && g.rawIngressHook.startupRouterID != "" {
		return fmt.Errorf("startup ingress cannot be changed while gateway is running")
	}
	copy := hook
	g.rawIngressHook = &copy
	return nil
}
func (g *LiveGateway) handleMessageIngress(ctx context.Context, event *larkim.P2MessageReceiveV1, legacy func() error) error {
	g.mu.Lock()
	hook := g.rawIngressHook
	g.mu.Unlock()
	if hook == nil {
		return legacy()
	}
	envelope, ok, err := g.rawMessageEnvelope(event)
	if err != nil || !ok {
		return err
	}
	allowed, err := hook.Authorize(ctx, envelope)
	if err != nil {
		return err
	}
	if !allowed {
		return nil
	}
	return hook.Receive(ctx, envelope)
}
func (g *LiveGateway) rawMessageEnvelope(event *larkim.P2MessageReceiveV1) (platformingress.RawIngressEnvelope, bool, error) {
	var raw platformingress.RawIngressEnvelope
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return raw, false, nil
	}
	if event.EventV2Base == nil || event.EventV2Base.Header == nil || event.Event.Sender == nil {
		return raw, false, fmt.Errorf("message lacks verified event header or sender")
	}
	if event.EventV2Base.Header.AppID != "" && event.EventV2Base.Header.AppID != g.config.AppID {
		return raw, false, fmt.Errorf("received event app does not match configured gateway")
	}
	message, sender := event.Event.Message, event.Event.Sender
	raw = platformingress.RawIngressEnvelope{GatewayRef: g.config.GatewayID, AppID: g.config.AppID, EventID: event.EventV2Base.Header.EventID, Tenant: event.EventV2Base.Header.TenantKey, SenderTenant: xutil.StringValue(sender.TenantKey), ChatID: xutil.StringValue(message.ChatId), ChatType: strings.ToLower(xutil.StringValue(message.ChatType)), MessageID: xutil.StringValue(message.MessageId), RootID: xutil.StringValue(message.RootId), ParentID: xutil.StringValue(message.ParentId), ThreadID: xutil.StringValue(message.ThreadId), SenderType: strings.ToLower(xutil.StringValue(sender.SenderType)), SenderIDs: rawSenderIDs(sender.SenderId), MessageType: xutil.StringValue(message.MessageType), RawContent: xutil.StringValue(message.Content), ReceivedAt: time.Now().UTC()}
	if millis, parseErr := strconv.ParseInt(xutil.StringValue(message.CreateTime), 10, 64); parseErr == nil && millis > 0 {
		raw.MessageCreatedAt = time.UnixMilli(millis).UTC()
	}
	if raw.Tenant == "" {
		raw.Tenant = raw.SenderTenant
	}
	for _, mention := range message.Mentions {
		if mention != nil {
			raw.Mentions = append(raw.Mentions, platformingress.RawMention{Key: xutil.StringValue(mention.Key), IDs: rawSenderIDs(mention.Id), Type: strings.ToLower(xutil.StringValue(mention.MentionedType)), Name: xutil.StringValue(mention.Name)})
		}
	}
	if err := raw.Validate(); err != nil {
		return raw, false, err
	}
	return raw, true, nil
}
func rawSenderIDs(ids *larkim.UserId) platformingress.SenderIDs {
	if ids == nil {
		return platformingress.SenderIDs{}
	}
	return platformingress.SenderIDs{OpenID: xutil.StringValue(ids.OpenId), UserID: xutil.StringValue(ids.UserId), UnionID: xutil.StringValue(ids.UnionId)}
}
