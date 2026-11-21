package daemon

import (
	"context"
	"fmt"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// teamManagementAuthorizer preserves the existing private Bot-management
// policy: an authenticated person may edit this conversation's bound Bot.
// There is no implicit installation-wide admin or team-owner override.
type teamManagementAuthorizer struct{ app *App }

func (a teamManagementAuthorizer) AuthorizeManagement(ctx context.Context, principal, kind, target string, source work.SourceRef) error {
	if a.app == nil || a.app.teamRuntime == nil {
		return fmt.Errorf("management runtime unavailable")
	}
	if kind != "role.global-description" {
		return fmt.Errorf("trusted global team management policy is not configured; team owner authority is required")
	}
	var binding work.RoleBotBinding
	err := a.app.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		if source.Kind != "message" {
			return fmt.Errorf("management requires an actual saved private human message")
		}
		message, err := tx.GetMessage(source.ID)
		if err != nil {
			return err
		}
		if message.Source != (work.Actor{Kind: work.ActorPrincipal, ID: principal}) || source.Version != strconv.FormatInt(message.Revision, 10) || source.EvidenceRef != message.BodyRef {
			return workstore.ErrScopeDenied
		}
		scope, err := tx.GetScope(message.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind != work.ScopePersonal || scope.PrincipalID != principal || scope.RoleID != target {
			return workstore.ErrScopeDenied
		}
		if _, err = tx.GetContent(scope.ID, message.BodyRef); err != nil {
			return err
		}
		if message.PlatformKey == nil {
			if message.DerivedFrom == nil || message.DerivedFrom.Kind != "human_interaction" {
				return fmt.Errorf("global role changes require a real private message or management interaction")
			}
			interaction, err := tx.GetHumanInteraction(message.DerivedFrom.ID)
			if err != nil {
				return err
			}
			if interaction.PrincipalID != principal || interaction.Gateway.RoleID != target {
				return workstore.ErrScopeDenied
			}
			viewID, _ := interaction.Envelope.Values["team_view"].(string)
			view, err := tx.GetUIView(viewID)
			if err != nil {
				return err
			}
			if view.ScopeID != scope.ID || view.PrincipalID != principal {
				return workstore.ErrScopeDenied
			}
			if err = tx.ValidateViewInteraction(view, interaction); err != nil {
				return err
			}
		}
		if message.PlatformKey != nil && (scope.Tenant != "" && message.PlatformKey.Tenant != scope.Tenant || scope.ChatID != "" && message.PlatformKey.ChatID != scope.ChatID) {
			return workstore.ErrScopeDenied
		}
		binding, err = tx.GetRoleBotBinding(target)
		return err
	})
	if err != nil {
		return err
	}
	// The current configuration is consulted outside the SQL transaction. Service
	// then revalidates the admitted source/run and binding before its mutation.
	_, err = (teamDirectory{app: a.app, store: a.app.teamRuntime.store}).ResolveGateway(ctx, binding.GatewayRef, binding.AppID)
	return err
}
