package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu/cardkit"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workingress"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workpublish"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) presentTeamSelections(ctx context.Context, selections []workingress.SelectionRequest) error {
	var failures []error
	for _, selection := range selections {
		if err := a.presentTeamSelection(ctx, selection); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (a *App) presentTeamSelection(ctx context.Context, selection workingress.SelectionRequest) error {
	r := a.teamRuntime
	apps, err := a.teamBootstrapConfiguredApps()
	if err != nil {
		return err
	}
	var notice workstore.SelectionNotice
	var binding work.RoleBotBinding
	send := false
	err = r.store.Within(ctx, func(tx *workstore.Tx) error {
		raw, e := tx.GetIngress(selection.IngressID)
		if e != nil {
			return e
		}
		binding, e = tx.GetRoleBotBinding(raw.Gateway.RoleID)
		if e != nil {
			return e
		}
		if old, e := tx.GetSelectionNotice(raw.ID); e == nil {
			notice = old
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		candidates := selection.CandidateRoleIDs
		if len(candidates) == 0 {
			roles, e := tx.ListRoles()
			if e != nil {
				return e
			}
			for _, role := range roles {
				if _, e := tx.GetRoleBotBinding(role.ID); e == nil {
					candidates = append(candidates, role.ID)
				}
			}
		}
		elements := []any{cardkit.PlainTextBlockElement("请指定这个项目的负责人。原来的需求已保存，选择后会继续处理。")}
		available := make([]string, 0, len(candidates))
		for _, roleID := range candidates {
			roleBinding, e := tx.GetRoleBotBinding(roleID)
			if errors.Is(e, workstore.ErrNotFound) {
				continue
			}
			if e != nil {
				return e
			}
			if apps[roleBinding.GatewayRef] != roleBinding.AppID {
				continue
			}
			role, e := tx.GetRole(roleID)
			if e != nil {
				return e
			}
			values := map[string]any{"team_setup_ingress": raw.ID, "ingress_id": raw.ID, "role_id": roleID, "binding_generation": strconv.FormatInt(raw.Route.BindingGeneration, 10)}
			available = append(available, roleID)
			elements = append(elements, map[string]any{"tag": "button", "text": cardkit.PlainText(role.Name), "behaviors": []any{map[string]any{"type": "callback", "value": values}}})
		}
		if len(elements) > 150 {
			return fmt.Errorf("负责人候选过多，需要先在群里明确 @ 一位负责人。")
		}
		card, e := json.Marshal(map[string]any{"schema": "2.0", "header": map[string]any{"title": cardkit.PlainText("选择项目负责人")}, "body": map[string]any{"elements": elements}})
		if e != nil {
			return e
		}
		notice, send, e = tx.TryBeginSelectionNotice(workstore.BeginSelectionNoticeRequest{IngressID: raw.ID, Gateway: raw.Gateway, Text: "选择项目负责人", Card: card, CandidateRoleIDs: available, Now: time.Now().UTC()})
		return e
	})
	if err != nil || !send {
		return err
	}
	live, err := (teamDirectory{app: a, store: r.store}).gateway(ctx, binding)
	var receipt feishu.ProjectPublishReceipt
	if err == nil {
		receipt, err = live.ProjectChatAPI().PublishCard(ctx, feishu.ProjectCardPublishRequest{ChatID: notice.ChatID, ReplyToMessageID: selection.Envelope.MessageID, ReplyInThread: selection.Envelope.ThreadID != "", Card: notice.Card, UUID: notice.UUID})
	}
	outcome := workstore.SelectionNoticeOutcome{State: work.OperationConfirmed, ExternalMessageID: receipt.MessageID, ChatID: receipt.ChatID, Now: time.Now().UTC()}
	if err != nil {
		outcome.State = work.OperationUnknown
		outcome.LastError = err.Error()
	}
	persistErr := r.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		err := tx.FinishSelectionNotice(notice.IngressID, notice.Revision, outcome)
		if !errors.Is(err, workstore.ErrConflict) {
			return err
		}
		current, lookupErr := tx.GetSelectionNotice(notice.IngressID)
		if lookupErr == nil && current.State == work.OperationConfirmed && (receipt.MessageID == "" || current.ExternalMessageID == receipt.MessageID) {
			return nil
		}
		return errors.Join(err, lookupErr)
	})
	return errors.Join(err, persistErr)
}

func (a *App) handleTeamLeaderSelection(ctx context.Context, interaction workstore.HumanInteraction) error {
	ingressID, _ := interaction.Envelope.Values["team_setup_ingress"].(string)
	roleID, _ := interaction.Envelope.Values["role_id"].(string)
	apps, err := a.teamBootstrapConfiguredApps()
	if err != nil {
		return err
	}
	if err := a.observeTeamLeaderSelection(ctx, ingressID, roleID, interaction); err != nil {
		return err
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		binding, err := tx.GetRoleBotBinding(roleID)
		if err != nil {
			return err
		}
		if apps[binding.GatewayRef] != binding.AppID {
			return workstore.ErrScopeDenied
		}
		_, err = tx.SelectBootstrapLeader(workstore.SelectBootstrapLeaderRequest{IngressID: ingressID, RoleID: roleID, InteractionID: interaction.ID})
		if err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, "leader_selected:"+roleID)
	})
}

func (a *App) observeTeamLeaderSelection(ctx context.Context, ingressID, roleID string, interaction workstore.HumanInteraction) error {
	r := a.teamRuntime
	var notice workstore.SelectionNotice
	var record work.IngressRecord
	var binding work.RoleBotBinding
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		notice, e = tx.GetSelectionNotice(ingressID)
		if e != nil {
			return e
		}
		if notice.State == work.OperationConfirmed {
			return nil
		}
		if notice.Gateway != interaction.Gateway || notice.Tenant != interaction.Envelope.Tenant || notice.ChatID != interaction.Envelope.ChatID ||
			!slices.Contains(notice.CandidateRoleIDs, roleID) || !workpublish.CardHasActualCallback(notice.Card, interaction.Envelope.Values) {
			return workstore.ErrScopeDenied
		}
		record, e = tx.GetIngress(ingressID)
		if e != nil {
			return e
		}
		binding, e = tx.GetRoleBotBinding(notice.Gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.GatewayRef != notice.Gateway.GatewayRef || binding.AppID != notice.Gateway.AppID || binding.IdentityGeneration != notice.Gateway.BotGeneration {
			return workstore.ErrConflict
		}
		return nil
	})
	if err != nil || notice.State == work.OperationConfirmed {
		return err
	}
	gateway, err := (teamDirectory{app: a, store: r.store}).gateway(ctx, binding)
	if err != nil {
		return err
	}
	actual, err := gateway.ProjectChatAPI().ReadPublishedMessage(ctx, interaction.Envelope.MessageID)
	if err != nil {
		return err
	}
	if err = verifyLeaderSelectionPublication(notice, interaction, record.Envelope.MessageID, actual); err != nil {
		return err
	}
	evidence, err := json.Marshal(actual)
	if err != nil {
		return err
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetRoleBotBinding(binding.RoleID)
		if e != nil {
			return e
		}
		if current != binding {
			return workstore.ErrConflict
		}
		return tx.ObserveSelectionNotice(notice.IngressID, notice.Revision, workstore.SelectionNoticeOutcome{
			State: work.OperationConfirmed, ExternalMessageID: actual.MessageID, ChatID: actual.ChatID, Now: time.Now().UTC(),
		}, evidence)
	})
}

func verifyLeaderSelectionPublication(notice workstore.SelectionNotice, interaction workstore.HumanInteraction, originalMessageID string, actual feishu.ObservedPublication) error {
	if actual.Deleted || actual.MessageID != interaction.Envelope.MessageID || actual.ChatID != notice.ChatID || actual.ParentID != originalMessageID ||
		actual.SenderType != "app" || actual.SenderIDType != "app_id" || actual.SenderID != notice.Gateway.AppID || actual.MessageType != "interactive" {
		return workstore.ErrScopeDenied
	}
	var frozen, observed any
	if json.Unmarshal(notice.Card, &frozen) != nil || json.Unmarshal([]byte(actual.Content), &observed) != nil ||
		!reflect.DeepEqual(frozen, observed) || !workpublish.CardHasActualCallback([]byte(actual.Content), interaction.Envelope.Values) {
		return workstore.ErrScopeDenied
	}
	return nil
}
