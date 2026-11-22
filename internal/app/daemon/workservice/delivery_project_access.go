package workservice

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (s *Service) refreshDeliveryProjectAccess(ctx context.Context, token, run string, req PublishDeliveryRequest) error {
	var targets []projectDocumentAccessTarget
	err := s.withDeliveryPublisher(ctx, token, run, req.TaskID, func(tx *workstore.Tx, c Caller) error {
		opID := stableID("op", c.Worker.ScopeID, string(work.ActorRole), c.Worker.RoleID, "delivery.publish", req.Key)
		if _, err := tx.GetOperation(opID); err == nil {
			return nil // A replay returns the historical publication receipt, without sending again.
		} else if !errors.Is(err, workstore.ErrNotFound) {
			return err
		}
		projection, err := s.authorizePublication(tx, c, req.ReplyToInputID, req.TaskID, req.GrantID, publicationDelivery, req.References)
		if err != nil {
			return err
		}
		if projection.Authority.Kind == publicationDelegated {
			id := delegatedDeliveryID(c.Worker.ScopeID, req.GrantID)
			if err = requireFirstBoundPublication(tx, stableID("delivery-notification", id)); err != nil {
				return err
			}
		}
		targets, err = s.loadDeliveryAccessTargets(ctx, tx, c.Worker.ScopeID, projection, req.References)
		return err
	})
	if err != nil {
		return err
	}
	return s.observeDeliveryAccessTargets(ctx, targets)
}

// RefreshArtifactAccessFromInteraction is a trusted Feishu view use case. Only
// lists the frozen current group's ACL on documents the project created.
func (s *Service) RefreshArtifactAccessFromInteraction(ctx context.Context, interactionID, viewID string) error {
	var targets []projectDocumentAccessTarget
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(interactionID)
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(viewID)
		if err != nil {
			return err
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil || scope.Kind != work.ScopeProject {
			return err
		}
		if err = tx.RequireProjectPrincipalAccess(scope.ID, interaction.PrincipalID, s.now()); err != nil {
			return err
		}
		action, _ := interaction.Envelope.Values["action"].(string)
		id := view.TargetID
		if view.Kind == "project.delivery-index" && action == "project.deliveries.inspect" {
			id, _ = interaction.Envelope.Values["delivery_id"].(string)
		} else if (view.Kind != "project.artifacts" && view.Kind != "business.decision") || (action != "artifacts.open" && action != "artifacts.page") {
			return nil
		}
		var references []work.DeliveryRef
		delivery, err := tx.GetSentDelivery(scope.ID, id, view.Tenant, view.ChatID, view.ChannelGeneration)
		if err == nil {
			if delivery.ConversationTarget != view.ChatID {
				return workstore.ErrScopeDenied
			}
			references = delivery.References
		} else if errors.Is(err, workstore.ErrNotFound) && (view.Kind == "business.decision" || view.Kind == "project.artifacts") {
			decision, err := tx.GetDecisionRequest(scope.ID, id)
			if err != nil {
				return err
			}
			if decision.Presentation != nil {
				references = decision.Presentation.References
			}
		} else {
			return err
		}
		projection := MessageProjection{Tenant: view.Tenant, ChatID: view.ChatID, ChannelGeneration: view.ChannelGeneration}
		targets, err = s.loadDeliveryAccessTargets(ctx, tx, scope.ID, projection, references)
		return err
	})
	if err != nil {
		return err
	}
	return s.observeDeliveryAccessTargets(ctx, targets)
}

func (s *Service) loadDeliveryAccessTargets(ctx context.Context, tx *workstore.Tx, scopeID string, destination MessageProjection, references []work.DeliveryRef) ([]projectDocumentAccessTarget, error) {
	if destination.ChannelGeneration < 1 {
		return nil, nil
	}
	var targets []projectDocumentAccessTarget
	seen := make(map[string]bool)
	for _, ref := range references {
		if seen[ref.ResourceID] {
			continue
		}
		seen[ref.ResourceID] = true
		resource, err := tx.GetResource(scopeID, ref.ResourceID)
		if err != nil {
			return nil, err
		}
		target, err := s.projectDocumentAccessTarget(ctx, tx, resource, destination)
		if errors.Is(err, workstore.ErrNotFound) {
			continue // External sources keep their own access policy.
		}
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func (s *Service) observeDeliveryAccessTargets(ctx context.Context, targets []projectDocumentAccessTarget) error {
	for _, target := range targets {
		if _, err := s.observeProjectDocumentAccess(ctx, target); err != nil {
			var readErr projectDocumentPermissionReadError
			if !errors.As(err, &readErr) {
				return err
			}
			// The failed observation is durable. Saved versions remain available.
		}
	}
	return nil
}
