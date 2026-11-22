package workstore

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type privateDisplayIdentity struct {
	Native *struct {
		DisplayID       string `json:"display_id"`
		SourceIngressID string `json:"source_ingress_id"`
	} `json:"private_display"`
	RoleID           string `json:"role_id"`
	GatewayRef       string `json:"gateway_ref"`
	AppID            string `json:"app_id"`
	BotGeneration    int64  `json:"bot_generation"`
	Tenant           string `json:"tenant"`
	ChatID           string `json:"chat_id"`
	ReplyToMessageID string `json:"reply_to_message_id"`
	SourceRunRef     string `json:"source_run_ref"`
}

func (t *Tx) VerifyPrivateDisplayPublication(op work.Operation) (PrivateExecutionDisplay, error) {
	var display PrivateExecutionDisplay
	if op.Kind != "feishu.publish" || op.Actor != (work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"}) {
		return display, ErrScopeDenied
	}
	content, err := t.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return display, err
	}
	var projection privateDisplayIdentity
	if err = json.Unmarshal(content.Data, &projection); err != nil {
		return display, err
	}
	if projection.Native == nil || projection.ChatID != op.TargetID || projection.BotGeneration != op.TargetGeneration {
		return display, ErrScopeDenied
	}
	scope, err := t.GetScope(op.ScopeID)
	if err != nil {
		return display, err
	}
	if scope.Kind != work.ScopePersonal || scope.RoleID != projection.RoleID {
		return display, ErrScopeDenied
	}
	if projection.Native.SourceIngressID != "" {
		if projection.Native.DisplayID != "" || projection.SourceRunRef != "" {
			return display, ErrScopeDenied
		}
		source, err := t.GetIngress(projection.Native.SourceIngressID)
		if err != nil {
			return display, err
		}
		if source.Route.ScopeID != op.ScopeID || source.Route.ContextID != op.ContextID || source.Envelope.ChatType != "p2p" ||
			source.State != work.IngressProcessed || source.Envelope.SenderType != "user" ||
			source.Gateway.RoleID != projection.RoleID || source.Gateway.AppID != projection.AppID ||
			source.Gateway.GatewayRef != projection.GatewayRef || source.Gateway.BotGeneration != projection.BotGeneration ||
			source.Envelope.ChatID != projection.ChatID || source.Envelope.Tenant != projection.Tenant || source.Envelope.MessageID != projection.ReplyToMessageID {
			return display, ErrScopeDenied
		}
		display.Bot = work.RoleBotBinding{RoleID: projection.RoleID, AppID: projection.AppID, GatewayRef: projection.GatewayRef, IdentityGeneration: projection.BotGeneration}
		display.ChatID, display.Tenant = projection.ChatID, projection.Tenant
		return display, nil
	}
	display, err = t.GetPrivateExecutionDisplay(projection.Native.DisplayID)
	if err != nil {
		return display, err
	}
	if display.Binding.ScopeID != op.ScopeID || display.Binding.ContextID != op.ContextID || display.Binding.RunRef != projection.SourceRunRef ||
		display.Bot.RoleID != projection.RoleID || display.Bot.AppID != projection.AppID || display.Bot.GatewayRef != projection.GatewayRef ||
		display.Bot.IdentityGeneration != projection.BotGeneration || display.ChatID != projection.ChatID || display.Tenant != projection.Tenant ||
		display.ReplyToMessageID != projection.ReplyToMessageID {
		return display, ErrScopeDenied
	}
	if display.RejectionCommandID != "" {
		command, err := t.GetNativeCommand(display.RejectionCommandID)
		if err != nil {
			return display, err
		}
		if command.Incarnation != display.Incarnation || command.Binding != display.Binding || command.TerminalRejection == nil {
			return display, ErrScopeDenied
		}
		return display, nil
	}
	turn, err := t.GetNativeTurn(display.Incarnation, display.SessionID, display.TurnID)
	if err != nil {
		return display, err
	}
	if turn.Binding != display.Binding {
		return display, ErrScopeDenied
	}
	return display, nil
}
