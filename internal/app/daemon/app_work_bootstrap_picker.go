package daemon

import (
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workui"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (a *App) queueBootstrapLocationPicker(tx *workstore.Tx, scope work.WorkScope, message work.Message, key string) (string, error) {
	binding, err := tx.GetRoleBotBinding(scope.RoleID)
	if err != nil {
		return "", err
	}
	preference, err := tx.GetExecutionPreference(scope.ID)
	if err != nil && !errors.Is(err, workstore.ErrNotFound) {
		return "", err
	}
	locations, err := tx.ListLocations()
	if err != nil {
		return "", err
	}
	source := teamHumanSource{BootstrapSourceMessageID: message.ID, NavigationRevision: scope.BindingGeneration}
	panel, err := a.teamResourcePickerPanel(source, scope.ID, 0, preference, locations)
	if err != nil {
		return "", err
	}
	id := workui.ID(scope.ID, "bootstrap-location-picker", key)
	card, err := workui.Card(id, panel)
	if err != nil {
		return "", err
	}
	view := workstore.UIView{ID: id, ScopeID: scope.ID, SurfaceKey: scope.ID + ":execution.location-picker", Kind: "execution.location-picker",
		TargetID: scope.ID, TargetRevision: max(1, preference.Revision), Binding: binding, Tenant: scope.Tenant, ChatID: scope.ChatID,
		PrincipalID: scope.PrincipalID, ChannelGeneration: scope.BindingGeneration, ReplyToMessageID: message.PlatformKey.MessageID}
	_, err = workui.Queue(tx, view, card)
	return id, err
}

func resumeBootstrapWithDirectory(tx *workstore.Tx, source teamHumanSource, view workstore.UIView, receiptID string) error {
	scope, err := tx.GetScope(view.ScopeID)
	if err != nil {
		return err
	}
	if err = tx.RequireOpenBootstrapScope(scope); err != nil {
		return err
	}
	message, err := tx.GetMessage(source.BootstrapSourceMessageID)
	if err != nil {
		return err
	}
	if message.ScopeID != scope.ID || message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) || message.RecalledEvidenceRef != "" ||
		message.PlatformKey == nil || message.PlatformKey.ChatID != scope.ChatID || message.PlatformKey.Tenant != scope.Tenant {
		return workstore.ErrScopeDenied
	}
	now := time.Now().UTC()
	id := receiptID + ":resume"
	input := work.Inbox{ID: id, ScopeID: scope.ID, RecipientRoleID: scope.RoleID, EventID: id, MessageID: message.ID, State: work.InboxPending, ContentState: work.InputContentReady, AvailableAt: now, Revision: 1}
	if err = tx.PutInbox(input, 0); err != nil {
		return err
	}
	noticeID := workui.ID(source.Key, "bootstrap.location.selected")
	card, err := workui.Card(noticeID, workui.Panel{Title: "已保存执行目录", Paragraphs: []string{"负责人会继续处理原来的项目需求。"}})
	if err != nil {
		return err
	}
	notice := workstore.UIView{ID: noticeID, ScopeID: scope.ID, SurfaceKey: view.SurfaceKey, Kind: "bootstrap.location.selected",
		TargetID: scope.ID, TargetRevision: view.TargetRevision, Binding: view.Binding, Tenant: scope.Tenant, ChatID: scope.ChatID,
		PrincipalID: scope.PrincipalID, ChannelGeneration: scope.BindingGeneration, ReplyToMessageID: message.PlatformKey.MessageID}
	if _, err = workui.Queue(tx, notice, card); err != nil {
		return err
	}
	interaction, err := tx.GetHumanInteraction(source.Authority.InteractionID)
	if err != nil {
		return err
	}
	return tx.FinishHumanInteraction(interaction.ID, interaction.Revision, noticeID)
}
