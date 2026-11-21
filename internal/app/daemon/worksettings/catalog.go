package worksettings

import (
	"context"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// FrozenContext is selected by a persisted command/view or exact admitted run.
// It never substitutes whichever private context happens to be visible now.
func FrozenContext(tx *workstore.Tx, source workstore.PrivateActionSource) (work.WorkContext, int64, error) {
	scope, _, err := tx.ValidatePrivateAction(source)
	if err != nil {
		return work.WorkContext{}, 0, err
	}
	id := ""
	navigation := int64(0)
	switch {
	case source.MenuInteractionID != "":
		menu, err := tx.GetMenuInteraction(source.MenuInteractionID)
		if err != nil {
			return work.WorkContext{}, 0, err
		}
		id = menu.ContextID
		navigation = menu.NavigationRevision
	case source.CommandID != "":
		command, err := tx.GetPrivateCommand(source.CommandID)
		if err != nil {
			return work.WorkContext{}, 0, err
		}
		id = command.ContextID
		navigation = command.NavigationRevision
	case source.InteractionID != "":
		interaction, err := tx.GetHumanInteraction(source.InteractionID)
		if err != nil {
			return work.WorkContext{}, 0, err
		}
		viewID, _ := interaction.Envelope.Values["team_view"].(string)
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return work.WorkContext{}, 0, err
		}
		if view.ScopeID != scope.ID || view.PrincipalID != scope.PrincipalID {
			return work.WorkContext{}, 0, workstore.ErrScopeDenied
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return work.WorkContext{}, 0, err
		}
		id = view.ContextID
		navigation = view.TargetRevision
	case source.MessageID != "":
		attempt, err := tx.GetAttemptByRunRef(source.RunRef.RunID)
		if err != nil {
			return work.WorkContext{}, 0, err
		}
		id = attempt.ContextID
	}
	context, err := tx.GetContext(id)
	if err != nil {
		return context, navigation, err
	}
	if context.ScopeID != scope.ID || context.RoleID != scope.RoleID {
		return context, navigation, workstore.ErrScopeDenied
	}
	if navigation > 0 {
		current, err := tx.GetPrivateNavigation(scope.ID)
		if err != nil {
			return context, navigation, err
		}
		if current.Revision != navigation || current.FrontContextID != context.ID {
			return context, navigation, fmt.Errorf("private context changed; use the current settings view")
		}
	}
	return context, navigation, nil
}
func (s *Service) catalog(ctx context.Context, source workstore.PrivateActionSource, binding work.RoleBotBinding, snapshot *Snapshot, cursors ...string) error {
	reader, ok := s.port.(CatalogReader)
	if !ok {
		return nil
	}
	cursor := ""
	if len(cursors) > 0 {
		cursor = cursors[0]
	}
	owner := CatalogOwner{Cursor: cursor, Backend: snapshot.Settings.Backend, ProfileID: selectedProfile(snapshot.Settings)}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		context, _, err := FrozenContext(tx, source)
		if err != nil {
			return err
		}
		owner.ContextID = context.ID
		role, err := tx.GetRole(binding.RoleID)
		if err != nil {
			return err
		}
		owner.SettingsRevision = role.SettingsRevision
		if source.MessageID != "" {
			worker, err := tx.GetWorker(source.RunRef.WorkerID)
			if err != nil {
				return err
			}
			owner.WorkerID = worker.ID
			owner.SessionGeneration = worker.Generation
			return nil
		}
		workers, err := tx.ListWorkers(context.ScopeID)
		if err != nil {
			return err
		}
		for _, worker := range workers {
			if worker.ContextID != context.ID || worker.RoleID != binding.RoleID || worker.State == work.WorkerRetired {
				continue
			}
			if owner.WorkerID != "" {
				return fmt.Errorf("private context has multiple native workers; exact model-catalog ownership is required")
			}
			owner.WorkerID = worker.ID
			owner.SessionGeneration = worker.Generation
		}
		return nil
	})
	if err != nil {
		return err
	}
	observed, err := reader.ReadCatalog(ctx, binding, owner)
	if err != nil {
		return err
	}
	snapshot.Catalog = observed.Snapshot
	snapshot.CatalogBackend = owner.Backend
	snapshot.CatalogProfileID = owner.ProfileID
	snapshot.CatalogEvidence = observed.Evidence
	snapshot.CatalogUnavailable = observed.Unavailable
	return nil
}
