package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
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
	err = a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error { _, _, e := tx.AcceptMenuInteraction(envelope, gateway); return e })
	if err == nil {
		a.teamRuntime.wakeLane("menus")
	}
	return err
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
	end, err := a.teamRuntime.beginPersonalSourceActivity(ctx, menu.ScopeID)
	if errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		return nil
	}
	if err != nil {
		return err
	}
	defer end()
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
	// The shared catalog defines the published command keys and their arguments.
	// Dispatch those through the same path as literal slash commands so aliases,
	// direct settings and operation semantics cannot drift between entry points.
	if action, ok := control.ParseFeishuMenuActionWithoutCatalog(menu.Envelope.EventKey); ok {
		result, err = a.dispatchTeamPrivateCommand(ctx, source, action)
		if err != nil {
			result, err = a.queuePrivatePanel(ctx, source, "command-error", "这次操作没有完成："+err.Error())
		}
	} else {
		// These project-specific destinations have no slash command.
		switch strings.ToLower(strings.TrimSpace(menu.Envelope.EventKey)) {
		case "settings":
			result, err = a.queueTeamSettings(ctx, source, "overview", "")
		case "project", "projects":
			result, err = a.queueTeamProjectPicker(ctx, source)
		case "resources":
			result, err = a.queueTeamResourcePicker(ctx, source)
		default:
			result, err = a.queuePrivatePanel(ctx, source, "menu", "这个菜单入口已不可用。请从工作菜单选择当前可用的操作。")
		}
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
