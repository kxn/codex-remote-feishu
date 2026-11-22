package daemon

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) openTeamProjectDeliveries(ctx context.Context, interaction workstore.HumanInteraction, source workstore.UIView, action string) error {
	if err := a.verifyWorkMembership(ctx, interaction.PrincipalID, source.ScopeID, interaction.Envelope.Tenant, interaction.Envelope.SenderIDs, interaction.Gateway); err != nil {
		return err
	}
	if action == "project.deliveries.inspect" {
		if err := a.teamRuntime.service.RefreshArtifactAccessFromInteraction(ctx, interaction.ID, source.ID); err != nil {
			return err
		}
	}
	return a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.ValidateViewInteraction(source, interaction); err != nil {
			return err
		}
		if err := tx.RequireProjectPrincipalAccess(source.ScopeID, interaction.PrincipalID, time.Now().UTC()); err != nil {
			return err
		}
		id := workui.ID(interaction.ID, action)
		next := workstore.UIView{ID: id, ScopeID: source.ScopeID, SurfaceKey: "project.deliveries:" + source.ScopeID + ":" + interaction.PrincipalID, Kind: "project.delivery-index", TargetID: source.TargetID, TargetRevision: source.TargetRevision, Binding: source.Binding, Tenant: source.Tenant, ChatID: source.ChatID, PrincipalID: interaction.PrincipalID, ChannelGeneration: source.ChannelGeneration, ReplyToMessageID: source.ExternalMessageID}
		panel := workui.Panel{Title: "项目成果与历史"}
		switch action {
		case "project.deliveries.inspect":
			deliveryID, _ := interaction.Envelope.Values["delivery_id"].(string)
			delivery, err := tx.GetSentDelivery(source.ScopeID, deliveryID, source.Tenant, source.ChatID, source.ChannelGeneration)
			if err != nil {
				return err
			}
			panel.Title = deliveryLabel(delivery.Purpose, 80)
			panel.Paragraphs = []string{deliveryLabel(delivery.Summary, 2000)}
			if delivery.NeededAction != "" {
				panel.Paragraphs = append(panel.Paragraphs, "需要处理："+deliveryLabel(delivery.NeededAction, 1000))
			}
			hasFixed := false
			for _, ref := range delivery.References {
				resource, err := tx.GetResource(source.ScopeID, ref.ResourceID)
				if err != nil {
					return err
				}
				hasFixed = hasFixed || !ref.Current
				managed, access, err := tx.CurrentProjectDocumentAccess(resource, source.Tenant, source.ChatID, source.ChannelGeneration)
				if err != nil {
					return err
				}
				if ref.Current && managed && !access {
					panel.Paragraphs = append(panel.Paragraphs, deliveryLabel(resource.Purpose, 80)+"的原件群访问待核对或恢复。可在群里请项目负责人处理。")
				} else if ref.Current && resource.ConnectorIdentity != work.SavedFileConnector {
					panel.Buttons = append(panel.Buttons, workui.Button{Label: deliveryLabel(resource.Purpose, 80) + "（当前原件）", URL: resource.CanonicalURI})
				}
			}
			if hasFixed {
				panel.Buttons = append(panel.Buttons, workui.Button{Label: "查看已保存版本", Action: "artifacts.open"})
			}
			next.Kind = "project.artifacts"
			next.TargetID, next.TargetRevision = delivery.ID, delivery.Revision
			next.SurfaceKey = "project.delivery:" + delivery.ID + ":" + interaction.PrincipalID
		case "project.deliveries.open", "project.deliveries.page":
			page := 0
			if action == "project.deliveries.page" {
				var err error
				page, err = businessPageInt(interaction.Envelope.Values["page"])
				if err != nil || page < 0 {
					return fmt.Errorf("成果页码无效")
				}
			}
			visible, err := tx.ListSentDeliveries(source.ScopeID, source.Tenant, source.ChatID, source.ChannelGeneration)
			if err != nil {
				return err
			}
			sort.Slice(visible, func(i, j int) bool {
				if visible[i].CreatedAt.Equal(visible[j].CreatedAt) {
					return visible[i].ID > visible[j].ID
				}
				return visible[i].CreatedAt.After(visible[j].CreatedAt)
			})
			const pageSize = 6
			if page > len(visible)/pageSize || (page > 0 && page*pageSize == len(visible)) {
				return fmt.Errorf("成果页已变化，请从项目状态重新打开")
			}
			if len(visible) == 0 {
				panel.Paragraphs = append(panel.Paragraphs, "此项目还没有已发送的交付。")
			}
			for _, delivery := range visible[page*pageSize : min(len(visible), (page+1)*pageSize)] {
				label := delivery.CreatedAt.In(time.Local).Format("01-02 15:04") + " · " + deliveryLabel(delivery.Purpose, 50)
				panel.Buttons = append(panel.Buttons, workui.Button{Label: label, Action: "project.deliveries.inspect", Values: map[string]any{"delivery_id": delivery.ID}})
			}
			if page > 0 {
				panel.Buttons = append(panel.Buttons, workui.Button{Label: "较新的交付", Action: "project.deliveries.page", Values: map[string]any{"page": page - 1}})
			}
			if (page+1)*pageSize < len(visible) {
				panel.Buttons = append(panel.Buttons, workui.Button{Label: "更早的交付", Action: "project.deliveries.page", Values: map[string]any{"page": page + 1}})
			}
		default:
			return workstore.ErrScopeDenied
		}
		if err := workui.ReplaceFromInteraction(tx, &next, interaction.ID); err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		card, err := workui.Card(id, panel)
		if err != nil {
			return err
		}
		if _, err = workui.Queue(tx, next, card); err != nil {
			return err
		}
		return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, id)
	})
}

func deliveryLabel(text string, max int) string {
	runes := []rune(text)
	if len(runes) > max {
		return string(runes[:max]) + "…"
	}
	return text
}
