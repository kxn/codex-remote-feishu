package daemon

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// steerTeamPrivateReply tries only the exact newly received input. The admission
// transaction verifies that its direct parent belongs to the occupied attempt.
func (a *App) steerTeamPrivateReply(ctx context.Context, ingressID string) error {
	var source teamHumanSource
	var inputID string
	err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		raw, err := tx.GetIngress(ingressID)
		if err != nil {
			return err
		}
		if raw.State != work.IngressProcessed || raw.Envelope.ChatType != "p2p" || raw.Envelope.ParentID == "" || raw.MessageID == "" {
			return nil
		}
		scope, err := tx.GetScope(raw.Route.ScopeID)
		if err != nil {
			return err
		}
		inputs, err := tx.ListPendingInputs(scope.ID, scope.RoleID)
		if err != nil {
			return err
		}
		for _, input := range inputs {
			if input.MessageID == raw.MessageID {
				inputID = input.ID
				break
			}
		}
		if inputID == "" {
			return nil
		}
		source = teamHumanSource{Key: raw.ID, ScopeID: scope.ID, PrincipalID: scope.PrincipalID, RoleID: scope.RoleID, Tenant: raw.Envelope.Tenant, ChatID: raw.Envelope.ChatID, MessageID: raw.Envelope.MessageID, Gateway: raw.Gateway, SenderIDs: raw.Envelope.SenderIDs, Authority: workstore.PrivateActionSource{PersonalScopeID: scope.ID, IngressID: raw.ID}}
		return nil
	})
	if err != nil || inputID == "" {
		return err
	}
	_, err = a.admitTeamPendingInputs(ctx, source, inputID, false)
	if errors.Is(err, workstore.ErrScopeDenied) || errors.Is(err, workstore.ErrConflict) {
		return nil
	}
	return err
}
