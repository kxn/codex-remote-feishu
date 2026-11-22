package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) validatePublicationMappingActor(operation work.Operation, mapping work.PlatformMessageMapping) error {
	if operation.Actor == (work.Actor{Kind: work.ActorRole, ID: mapping.RoleID}) {
		return nil
	}
	if operation.Actor == (work.Actor{Kind: work.ActorSystem, ID: "private-execution-display"}) {
		display, err := t.VerifyPrivateDisplayPublication(operation)
		if err != nil {
			return err
		}
		if display.Bot.RoleID != mapping.RoleID || display.Bot.IdentityGeneration != mapping.BotGeneration ||
			display.Tenant != mapping.Tenant || display.ChatID != mapping.ChatID {
			return ErrScopeDenied
		}
		return nil
	}
	if operation.Actor != (work.Actor{Kind: work.ActorSystem, ID: "team-ui"}) {
		return fmt.Errorf("publication actor has no role or exact UI ownership")
	}
	content, err := t.GetContent(mapping.ScopeID, operation.RequestRef)
	if err != nil {
		return err
	}
	var projection struct {
		UIViewID      string `json:"ui_view_id"`
		RoleID        string `json:"role_id"`
		GatewayRef    string `json:"gateway_ref"`
		AppID         string `json:"app_id"`
		BotGeneration int64  `json:"bot_generation"`
		Tenant        string `json:"tenant"`
		ChatID        string `json:"chat_id"`
	}
	if err = json.Unmarshal(content.Data, &projection); err != nil {
		return err
	}
	if projection.UIViewID == "" {
		return fmt.Errorf("system UI publication lacks registered view")
	}
	view, err := t.GetUIView(projection.UIViewID)
	if err != nil {
		return err
	}
	if view.OperationID != operation.ID || view.ScopeID != mapping.ScopeID || view.Binding.RoleID != mapping.RoleID || view.Binding.IdentityGeneration != mapping.BotGeneration || view.Tenant != mapping.Tenant || view.ChatID != mapping.ChatID || view.Binding.GatewayRef != projection.GatewayRef || view.Binding.AppID != projection.AppID || projection.RoleID != mapping.RoleID || projection.BotGeneration != mapping.BotGeneration || projection.Tenant != mapping.Tenant || projection.ChatID != mapping.ChatID {
		return fmt.Errorf("system UI view and publication identity disagree")
	}
	return nil
}
