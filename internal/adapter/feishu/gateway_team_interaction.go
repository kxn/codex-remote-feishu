package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
	larkcallback "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

func (g *LiveGateway) handleTeamCard(ctx context.Context, event *larkcallback.CardActionTriggerEvent) (*larkcallback.CardActionTriggerResponse, bool, error) {
	g.mu.Lock()
	hook := g.rawIngressHook
	g.mu.Unlock()
	if hook == nil {
		return nil, false, nil
	}
	if hook.ReceiveInteraction == nil && hook.ReceiveInteractionResponse == nil {
		return nil, true, fmt.Errorf("team interaction handler unavailable")
	}
	if event == nil || event.EventV2Base == nil || event.EventV2Base.Header == nil || event.Event == nil || event.Event.Operator == nil || event.Event.Context == nil || event.Event.Action == nil {
		return nil, true, fmt.Errorf("card callback lacks verified event, actor or message")
	}
	header := event.EventV2Base.Header
	if header.AppID != "" && header.AppID != g.config.AppID {
		return nil, true, fmt.Errorf("card callback app mismatch")
	}
	operator, source, action := event.Event.Operator, event.Event.Context, event.Event.Action
	tenant := header.TenantKey
	if tenant == "" {
		tenant = xutil.StringValue(operator.TenantKey)
	}
	if operator.TenantKey != nil && *operator.TenantKey != "" && tenant != *operator.TenantKey {
		return nil, true, fmt.Errorf("card actor tenant mismatch")
	}
	raw := platformingress.HumanInteractionEnvelope{GatewayRef: g.config.GatewayID, AppID: g.config.AppID, EventID: header.EventID, Tenant: tenant, SenderIDs: platformingress.SenderIDs{OpenID: operator.OpenID, UserID: xutil.StringValue(operator.UserID)}, ChatID: source.OpenChatID, MessageID: source.OpenMessageID, Kind: "card", Values: action.Value, Form: action.FormValue, Option: action.Option, Options: action.Options, Input: action.InputValue, ReceivedAt: time.Now().UTC()}
	if hook.ReceiveInteractionResponse != nil {
		result, err := hook.ReceiveInteractionResponse(ctx, raw)
		if err != nil {
			return nil, true, err
		}
		if len(result.RawBody) == 0 {
			return nil, true, nil
		}
		var response larkcallback.CardActionTriggerResponse
		if !json.Valid(result.RawBody) || json.Unmarshal(result.RawBody, &response) != nil {
			return nil, true, fmt.Errorf("invalid committed interaction response")
		}
		return &response, true, nil
	}
	text, err := hook.ReceiveInteraction(ctx, raw)
	if err != nil {
		return nil, true, err
	}
	return &larkcallback.CardActionTriggerResponse{Toast: &larkcallback.Toast{Type: "info", Content: text}}, true, nil
}
