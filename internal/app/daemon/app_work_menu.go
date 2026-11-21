package daemon

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"github.com/kxn/codex-remote-feishu/internal/feishuapp"
)

// SDK ACK follows this durable receipt. Destination discovery and publication
// happen in a separate lane and never fabricate a human message or group chat.
func (a *App) receiveTeamMenu(ctx context.Context, raw feishu.MenuIngressEvent) error {
	if a.teamRuntime == nil {
		return fmt.Errorf("team runtime unavailable")
	}
	gateway, err := (teamDirectory{app: a, store: a.teamRuntime.store}).ResolveGateway(ctx, raw.GatewayRef, raw.AppID)
	if err != nil {
		return err
	}
	envelope := work.MenuInteractionEnvelope{GatewayRef: raw.GatewayRef, AppID: raw.AppID, EventID: raw.EventID, Tenant: raw.Tenant, SenderIDs: raw.SenderIDs, EventKey: raw.EventKey, ClickedAt: raw.ClickedAt, ReceivedAt: raw.ReceivedAt}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { _, _, e := tx.AcceptMenuInteraction(envelope, gateway); return e })
}
func (a *App) runTeamMenus(ctx context.Context) error {
	var pending []workstore.MenuInteraction
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; pending, e = tx.ListPendingMenuInteractions(32); return e }); err != nil {
		return err
	}
	var problems []error
	for _, menu := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := a.advanceTeamMenu(ctx, menu); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
func (a *App) advanceTeamMenu(ctx context.Context, menu workstore.MenuInteraction) error {
	if menu.DestinationState != work.OperationConfirmed {
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { return tx.AdoptKnownMenuDestination(menu.ID, menu.Revision) })
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		if err == nil {
			if err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; menu, e = tx.GetMenuInteraction(menu.ID); return e }); err != nil {
				return err
			}
		} else {
			// A sending/unknown intent is never repeated. A later actual private SDK
			// observation can still establish the chat without another first message.
			if menu.DestinationState != "" {
				return nil
			}
			send := false
			err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
				var e error
				menu, send, e = tx.BeginMenuDestination(menu.ID, menu.Revision, "已收到你的菜单请求，正在打开私人工作入口。也可以直接在这里告诉我需要处理的事情。")
				return e
			})
			if err != nil {
				return err
			}
			if !send {
				return nil
			}
			binding := work.RoleBotBinding{RoleID: menu.Gateway.RoleID, GatewayRef: menu.Gateway.GatewayRef, AppID: menu.Gateway.AppID, IdentityGeneration: menu.Gateway.BotGeneration}
			gateway, e := (teamDirectory{app: a, store: a.teamRuntime.store}).gateway(ctx, binding)
			var receipt feishu.ProjectPublishReceipt
			if e == nil {
				var content work.Content
				e = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
					var err error
					content, err = tx.GetContent(menu.ScopeID, menu.DestinationTextRef)
					return err
				})
				if e == nil {
					receipt, e = gateway.ProjectChatAPI().PublishPrivateEntry(ctx, feishu.PrivateEntryPublishRequest{OpenID: menu.Envelope.SenderIDs.OpenID, Text: string(content.Data), UUID: menu.UUID})
				}
			}
			reason := ""
			if e != nil {
				reason = e.Error()
			}
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			saveErr := a.teamRuntime.store.Within(saveCtx, func(tx *workstore.Tx) error {
				return tx.FinishMenuDestination(menu.ID, menu.Revision, receipt.ChatID, receipt.MessageID, reason, e == nil)
			})
			cancel()
			if saveErr != nil {
				return errors.Join(e, saveErr)
			}
			if e != nil {
				return e
			}
			if err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { var e error; menu, e = tx.GetMenuInteraction(menu.ID); return e }); err != nil {
				return err
			}
		}
	}
	source := teamHumanSource{NavigationRevision: menu.NavigationRevision, Key: menu.ID, ScopeID: menu.ScopeID, PrincipalID: menu.PrincipalID, RoleID: menu.Gateway.RoleID, Tenant: menu.Envelope.Tenant, ChatID: menu.ChatID, MessageID: menu.MessageID, Gateway: menu.Gateway, SenderIDs: menu.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: menu.ScopeID, MenuInteractionID: menu.ID}}
	var result string
	var err error
	route := feishuapp.ResolveTeamMenu(menu.Envelope.EventKey)
	switch route.Action {
	case feishuapp.TeamMenuSteer:
		result, err = a.steerTeamPendingInputs(ctx, source)
	case feishuapp.TeamMenuReview:
		result, err = a.queueTeamReview(ctx, source, control.Action{Kind: control.ActionReviewCommand, Text: "/review"})
	case feishuapp.TeamMenuAutoWhip, feishuapp.TeamMenuAutoContinue:
		kind := control.ActionAutoWhipCommand
		if route.Action == feishuapp.TeamMenuAutoContinue {
			kind = control.ActionAutoContinueCommand
		}
		mode := route.Argument
		if mode == "" {
			mode = "status"
		}
		result, err = a.handleTeamAutomation(ctx, source, control.Action{Kind: kind, Text: "/" + string(route.Action) + " " + mode})
	case feishuapp.TeamMenuSettings:
		page := route.Argument
		if page == "" {
			page = "overview"
		}
		result, err = a.queueTeamSettings(ctx, source, page, "")
	case feishuapp.TeamMenuProjects:
		result, err = a.queueTeamProjectPicker(ctx, source)
	case feishuapp.TeamMenuResources:
		result, err = a.queueTeamResourcePicker(ctx, source)
	case feishuapp.TeamMenuHistory:
		result, err = a.queuePrivatePanel(ctx, source, "contexts", "")
	case feishuapp.TeamMenuStatus:
		result, err = a.queuePrivatePanel(ctx, source, "status", "")
	case feishuapp.TeamMenuHome:
		result, err = a.queuePrivatePanel(ctx, source, "menu", route.Note)
	default:
		return fmt.Errorf("unsupported team menu action %q", route.Action)
	}
	if err != nil {
		return err
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		latest, e := tx.GetMenuInteraction(menu.ID)
		if e != nil {
			return e
		}
		return tx.FinishMenuInteraction(menu.ID, latest.Revision, result)
	})
}
