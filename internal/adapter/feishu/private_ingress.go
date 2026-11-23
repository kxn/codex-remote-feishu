package feishu

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	larkevent "github.com/larksuite/oapi-sdk-go/v3/event"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkapplication "github.com/larksuite/oapi-sdk-go/v3/service/application/v6"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func privateString(v string) *string { return &v }
func privateSDKBase(tenant, app, event string) *larkevent.EventV2Base {
	return &larkevent.EventV2Base{Header: &larkevent.EventHeader{TenantKey: tenant, AppID: app, EventID: event}}
}
func privateSDKUser(ids platformingress.SenderIDs) *larkim.UserId {
	return &larkim.UserId{OpenId: privateString(ids.OpenID), UserId: privateString(ids.UserID), UnionId: privateString(ids.UnionID)}
}

// DecodePrivateMessage uses the same SDK planner and text parser as the legacy
// gateway. Non-text input and quote fetch require the host's asynchronous
// staging port, never an I/O call inside a private checkpoint transaction.
func DecodePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope) (control.Action, bool, error) {
	if err := raw.Validate(); err != nil {
		return control.Action{}, false, err
	}
	if raw.ChatType != "p2p" {
		return control.Action{}, false, fmt.Errorf("private message must be p2p")
	}
	event := privateSDKMessage(raw)
	env := gatewaypkg.InboundEnv{GatewayID: raw.GatewayRef, ParseTextActionWithoutCatalog: control.ParseFeishuTextActionWithoutCatalog, RecordSurfaceMessage: func(string, string) {}}
	return gatewaypkg.ParsePlannedTextAction(ctx, env, event)
}

// DecodeHistoricalPrivateMedia reuses the live SDK planner with a trusted,
// source-verified asset lookup. It never downloads or guesses quoted content.
func DecodeHistoricalPrivateMedia(ctx context.Context, raw platformingress.RawIngressEnvelope, lookup func(messageID, key, name string) (path, mime string, err error)) (control.Action, bool, error) {
	if err := raw.Validate(); err != nil {
		return control.Action{}, false, err
	}
	if raw.ChatType != "p2p" || raw.ParentID != "" || raw.RootID != "" || (raw.MessageType != "image" && raw.MessageType != "file") || lookup == nil {
		return control.Action{}, false, gatewaypkg.ErrInboundStaging
	}
	env := gatewaypkg.InboundEnv{GatewayID: raw.GatewayRef, ParseTextActionWithoutCatalog: control.ParseFeishuTextActionWithoutCatalog, RecordSurfaceMessage: func(string, string) {}, DownloadImage: func(_ context.Context, message, key string) (string, string, error) { return lookup(message, key, "") }, DownloadFile: func(_ context.Context, message, key, name string) (string, error) {
		path, _, err := lookup(message, key, name)
		return path, err
	}}
	return gatewaypkg.ParsePlannedInboundAction(ctx, env, privateSDKMessage(raw))
}
func DecodePrivateCard(raw platformingress.HumanInteractionEnvelope, surface string) (control.Action, bool) {
	event := &larkcallback.CardActionTriggerEvent{EventV2Base: privateSDKBase(raw.Tenant, raw.AppID, raw.EventID), Event: &larkcallback.CardActionTriggerRequest{Operator: &larkcallback.Operator{OpenID: raw.SenderIDs.OpenID, UserID: privateString(raw.SenderIDs.UserID)}, Context: &larkcallback.Context{OpenChatID: raw.ChatID, OpenMessageID: raw.MessageID}, Action: &larkcallback.CallBackAction{Value: raw.Values, FormValue: raw.Form, Option: raw.Option, Options: raw.Options, InputValue: raw.Input}}}
	return gatewaypkg.ParseCardActionTriggerEvent(gatewaypkg.RoutingEnv{GatewayID: raw.GatewayRef, SurfaceForCardAction: func(lookup gatewaypkg.CardActionSurfaceLookup) string {
		if lookup.MessageID != raw.MessageID || lookup.ChatID != raw.ChatID {
			return ""
		}
		return surface
	}}, event)
}
func DecodePrivateMenu(raw platformingress.MenuIngressEvent) (control.Action, bool) {
	event := &larkapplication.P2BotMenuV6{EventV2Base: privateSDKBase(raw.Tenant, raw.AppID, raw.EventID), Event: &larkapplication.P2BotMenuV6Data{EventKey: privateString(raw.EventKey), Operator: &larkapplication.Operator{OperatorId: &larkapplication.UserId{OpenId: privateString(raw.SenderIDs.OpenID), UserId: privateString(raw.SenderIDs.UserID), UnionId: privateString(raw.SenderIDs.UnionID)}}}}
	return gatewaypkg.ParseMenuEvent(raw.GatewayRef, event)
}
func DecodePrivateObservation(raw platformingress.PlatformIngressEvent, surface string) (control.Action, bool) {
	env := gatewaypkg.InboundEnv{GatewayID: raw.GatewayRef, LookupSurfaceMessage: func(id string) string {
		if id == raw.MessageID {
			return surface
		}
		return ""
	}}
	switch raw.Kind {
	case "message.recalled":
		return gatewaypkg.ParseMessageRecalledEvent(env, &larkim.P2MessageRecalledV1{EventV2Base: privateSDKBase(raw.Tenant, raw.AppID, raw.EventID), Event: &larkim.P2MessageRecalledV1Data{MessageId: privateString(raw.MessageID), ChatId: privateString(raw.ChatID), RecallType: privateString(raw.RecallType)}})
	case "reaction.created":
		if raw.OperatorType != "user" {
			return control.Action{}, false
		}
		return gatewaypkg.ParseMessageReactionCreatedEvent(env, &larkim.P2MessageReactionCreatedV1{EventV2Base: privateSDKBase(raw.Tenant, raw.AppID, raw.EventID), Event: &larkim.P2MessageReactionCreatedV1Data{MessageId: privateString(raw.MessageID), UserId: privateSDKUser(raw.OperatorIDs), ReactionType: &larkim.Emoji{EmojiType: privateString(raw.Emoji)}}})
	}
	return control.Action{}, false
}

func privateSDKMessage(raw platformingress.RawIngressEnvelope) *larkim.P2MessageReceiveV1 {
	msg := &larkim.EventMessage{MessageId: privateString(raw.MessageID), ChatId: privateString(raw.ChatID), ChatType: privateString(raw.ChatType), MessageType: privateString(raw.MessageType), Content: privateString(raw.RawContent), RootId: privateString(raw.RootID), ParentId: privateString(raw.ParentID), ThreadId: privateString(raw.ThreadID)}
	if !raw.MessageCreatedAt.IsZero() {
		msg.CreateTime = privateString(strconv.FormatInt(raw.MessageCreatedAt.UnixMilli(), 10))
	}
	for _, m := range raw.Mentions {
		msg.Mentions = append(msg.Mentions, &larkim.MentionEvent{Key: privateString(m.Key), Id: privateSDKUser(m.IDs), MentionedType: privateString(m.Type), Name: privateString(m.Name)})
	}
	event := &larkim.P2MessageReceiveV1{EventV2Base: privateSDKBase(raw.Tenant, raw.AppID, raw.EventID), Event: &larkim.P2MessageReceiveV1Data{Message: msg, Sender: &larkim.EventSender{SenderType: privateString(raw.SenderType), TenantKey: privateString(raw.SenderTenant), SenderId: privateSDKUser(raw.SenderIDs)}}}

	return event
}

// StagePrivateMessage reuses the existing media and quote implementation and
// must be invoked from a durable host worker, never a checkpoint transaction.
func (g *LiveGateway) StagePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope, routerID string) (control.Action, bool, error) {
	if err := raw.Validate(); err != nil {
		return control.Action{}, false, err
	}
	if raw.ChatType != "p2p" || raw.GatewayRef != g.config.GatewayID || raw.AppID != g.config.AppID {
		return control.Action{}, false, fmt.Errorf("private staging gateway mismatch")
	}
	proof, err := g.StartupIngressProof()
	if err != nil || proof.RouterID != routerID {
		return control.Action{}, false, fmt.Errorf("private staging requires active neutral gateway")
	}
	base := g.config.TempDir
	if base == "" {
		base = os.TempDir()
	}
	base, err = filepath.Abs(base)
	if err != nil {
		return control.Action{}, false, err
	}
	if err = os.MkdirAll(base, 0700); err != nil {
		return control.Action{}, false, err
	}
	store, err := privatestaging.Open(base, privatestaging.Owner{Tenant: raw.Tenant, AppID: raw.AppID, Gateway: raw.GatewayRef, EventID: raw.EventID, MessageID: raw.MessageID})
	if err != nil {
		return control.Action{}, false, err
	}
	ctx = privatestaging.WithStore(ctx, store)
	env := g.inboundEnv()
	env.RecordSurfaceMessage = func(string, string) {}
	return gatewaypkg.ParsePlannedInboundAction(ctx, env, privateSDKMessage(raw))
}
func (c *MultiGatewayController) StagePrivateMessage(ctx context.Context, raw platformingress.RawIngressEnvelope, routerID string) (control.Action, bool, error) {
	c.mu.Lock()
	w := c.workers[raw.GatewayRef]
	if w == nil || w.runtime == nil || w.config.AppID != raw.AppID {
		c.mu.Unlock()
		return control.Action{}, false, fmt.Errorf("private staging gateway unavailable")
	}
	g, ok := w.runtime.(*LiveGateway)
	c.mu.Unlock()
	if !ok {
		return control.Action{}, false, fmt.Errorf("private staging unsupported")
	}
	proof, err := g.StartupIngressProof()
	if err != nil {
		return control.Action{}, false, err
	}
	action, handled, err := g.StagePrivateMessage(ctx, raw, routerID)
	if err != nil {
		return action, handled, err
	}
	if err = c.VerifyStartupIngressProof(proof); err != nil {
		return control.Action{}, false, err
	}
	return action, handled, nil
}

// PrivateStagingBase returns the configured SDK download root for the exact
// current gateway identity. A stale or replaced gateway cannot claim files.
func (c *MultiGatewayController) PrivateStagingBase(gatewayID, appID string) (string, error) {
	c.mu.Lock()
	w := c.workers[gatewayID]
	if w == nil || w.config.AppID != appID {
		c.mu.Unlock()
		return "", fmt.Errorf("private staging gateway unavailable")
	}
	base := w.config.ImageTempDir
	c.mu.Unlock()
	if base == "" {
		base = os.TempDir()
	}
	return filepath.Abs(base)
}
