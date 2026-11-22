package workui

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ReplaceFromInteraction only reuses the exact card a person just operated.
// Slash commands and proactive notices therefore still create visible replies.
func ReplaceFromInteraction(tx *workstore.Tx, view *workstore.UIView, interactionID string) error {
	view.ReplacesViewID = ""
	if interactionID == "" {
		return nil
	}
	interaction, err := tx.GetHumanInteraction(interactionID)
	if err != nil {
		return err
	}
	oldID, _ := interaction.Envelope.Values["team_view"].(string)
	if oldID == "" {
		return nil
	}
	old, err := tx.GetUIView(oldID)
	if err != nil {
		return err
	}
	if old.ScopeID != view.ScopeID || old.SurfaceKey != view.SurfaceKey || old.Binding != view.Binding || old.Tenant != view.Tenant || old.ChatID != view.ChatID || old.PrincipalID != view.PrincipalID {
		return nil
	}
	if err = tx.ValidateViewInteraction(old, interaction); err != nil {
		return err
	}
	op, err := tx.GetOperation(old.OperationID)
	if err != nil {
		return err
	}
	if old.ExternalMessageID != "" && op.State == work.OperationConfirmed {
		view.ReplacesViewID = old.ID
	}
	return nil
}

// PrivateNavigationFromInteraction gives the private menu and its pages a common
// presentation owner. Their view kinds, frozen objects and revisions still
// own their separate authorization and mutation contracts.
func PrivateNavigationFromInteraction(tx *workstore.Tx, view *workstore.UIView, interactionID string) error {
	view.SurfaceKey = "private-menu:" + view.ScopeID
	return ReplaceFromInteraction(tx, view, interactionID)
}

// Private navigation used separate presentation keys before menu pages shared
// one message. Retire only those keys when publishing a new private menu page.
func legacyPrivateNavigationOwner(old, next workstore.UIView) bool {
	if next.SurfaceKey != "private-menu:"+next.ScopeID || old.ScopeID != next.ScopeID || old.PrincipalID != next.PrincipalID {
		return false
	}
	switch old.Kind {
	case "bot.settings":
		return old.SurfaceKey == "settings:"+old.ScopeID
	case "private.project-picker", "private.context-picker", "execution.location-picker":
		return old.SurfaceKey == old.ScopeID+":"+old.Kind
	default:
		return false
	}
}
