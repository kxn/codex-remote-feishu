package worksettings

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func validateSource(tx *workstore.Tx, principal, role string, source workstore.PrivateActionSource) (work.WorkScope, work.RoleBotBinding, work.SourceRef, error) {
	scope, ref, err := tx.ValidatePrivateAction(source)
	if err != nil {
		return scope, work.RoleBotBinding{}, ref, err
	}
	if principal == "" || scope.PrincipalID != principal || scope.RoleID != role {
		return scope, work.RoleBotBinding{}, ref, workstore.ErrScopeDenied
	}
	binding, err := tx.GetRoleBotBinding(role)
	if err != nil {
		return scope, binding, ref, err
	}
	if source.InteractionID != "" {
		interaction, err := tx.GetHumanInteraction(source.InteractionID)
		if err != nil {
			return scope, binding, ref, err
		}
		viewID, _ := interaction.Envelope.Values["team_view"].(string)
		if viewID == "" {
			return scope, binding, ref, fmt.Errorf("settings require a persisted private view interaction")
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return scope, binding, ref, err
		}
		if view.ScopeID != scope.ID || view.PrincipalID != principal || view.Binding != binding {
			return scope, binding, ref, workstore.ErrScopeDenied
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return scope, binding, ref, err
		}
	}
	return scope, binding, ref, nil
}
func (s *Service) Discover(ctx context.Context, principal, role string, source workstore.PrivateActionSource) (Snapshot, error) {
	return s.DiscoverPage(ctx, principal, role, source, "")
}
func (s *Service) DiscoverPage(ctx context.Context, principal, role string, source workstore.PrivateActionSource, cursor string) (Snapshot, error) {
	var binding work.RoleBotBinding
	var ref work.SourceRef
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		_, b, r, err := validateSource(tx, principal, role, source)
		binding, ref = b, r
		return err
	})
	if err != nil {
		return Snapshot{}, err
	}
	if err = s.authorize(ctx, principal, binding, ref); err != nil {
		return Snapshot{}, err
	}
	snapshot, err := s.port.Snapshot(ctx, binding)
	if err != nil {
		return snapshot, err
	}
	err = s.catalog(ctx, source, binding, &snapshot, cursor)
	return snapshot, err
}
