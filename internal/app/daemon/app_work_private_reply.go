package daemon

import (
	"context"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) presentBlockedPrivateReplies(ctx context.Context, ids []string) error {
	var failures []error
	for _, id := range ids {
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			raw, err := tx.GetIngress(id)
			if err != nil {
				return err
			}
			if raw.State != work.IngressPending || raw.Route.Kind != work.ScopePersonal || raw.Route.ReplyRoutingError == "" && raw.ReplyDeliveryError == "" {
				return nil
			}
			scope, err := tx.GetScope(raw.Route.ScopeID)
			if err != nil {
				return err
			}
			binding, err := tx.GetRoleBotBinding(raw.Gateway.RoleID)
			if err != nil {
				return err
			}
			if binding.AppID != raw.Gateway.AppID || binding.IdentityGeneration != raw.Gateway.BotGeneration || binding.GatewayRef != raw.Gateway.GatewayRef {
				expected := raw.Revision
				raw.Revision++
				raw.State = work.IngressIgnored
				raw.NextAttemptAt = nil
				raw.LastError = "回复归属提示未发送：原 Bot 身份已变化，不能使用旧聊天地址向新身份投递。"
				return tx.PutIngress(raw, expected)
			}
			viewID := workui.ID("private-reply-routing", raw.ID)
			reason := raw.Route.ReplyRoutingError
			if reason == "" {
				reason = raw.ReplyDeliveryError
			}
			card, err := workui.Card(viewID, workui.Panel{Title: "需要明确这条回复的归属", Paragraphs: []string{reason}})
			if err != nil {
				return err
			}
			_, err = workui.Queue(tx, workstore.UIView{ID: viewID, ScopeID: scope.ID, SurfaceKey: viewID, Kind: "private.reply.notice", TargetID: raw.ID, TargetRevision: raw.Revision, Binding: binding, Tenant: raw.Envelope.Tenant, ChatID: raw.Envelope.ChatID, PrincipalID: scope.PrincipalID, ReplyToMessageID: raw.Envelope.MessageID}, card)
			if err != nil {
				return err
			}
			expected := raw.Revision
			raw.Revision++
			raw.State = work.IngressIgnored
			return tx.PutIngress(raw, expected)
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
