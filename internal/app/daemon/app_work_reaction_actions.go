package daemon

import (
	"context"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (a *App) runTeamPrivateReactionActions(ctx context.Context) error {
	var actions []workstore.PrivateReactionAction
	if err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		actions, e = tx.ListPreparedPrivateReactionActions(16)
		return e
	}); err != nil {
		return err
	}
	for _, action := range actions {
		var source teamHumanSource
		eligible := false
		err := a.teamRuntime.store.Within(ctx, func(tx *workstore.Tx) error {
			scope, _, e := tx.ValidatePrivateReactionAction(action.ID)
			if e != nil {
				return e
			}
			input, e := tx.GetInbox(action.InputID)
			if e != nil {
				return e
			}
			if input.State != "pending" || input.ContentState == "pending" {
				return nil
			}
			message, e := tx.GetMessage(input.MessageID)
			if e != nil {
				return e
			}
			observation, e := tx.GetPlatformObservation(action.ID)
			if e != nil {
				return e
			}
			source = teamHumanSource{Key: action.ID, ScopeID: scope.ID, PrincipalID: scope.PrincipalID, RoleID: scope.RoleID, Tenant: message.PlatformKey.Tenant, ChatID: message.PlatformKey.ChatID, MessageID: message.PlatformKey.MessageID, Gateway: observation.Gateway, Authority: workstore.PrivateActionSource{PersonalScopeID: scope.ID, ReactionID: action.ID}, NavigationRevision: action.NavigationRevision}
			eligible = true
			return nil
		})
		if err == nil && eligible {
			_, err = a.admitTeamPendingInputs(ctx, source, action.InputID, action.Emoji == "APPLAUSE")
		}
		reason := ""
		if err != nil {
			reason = err.Error()
		}
		if e := a.teamRuntime.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
			if errors.Is(err, errTeamReactionUnsupported) || errors.Is(err, errTeamPrivateShellUnsent) {
				if e := queueTeamReactionUnsupported(tx, action, source, err.Error()); e != nil {
					return e
				}
			}
			return tx.FinishPrivateReactionAction(action.ID, reason)
		}); e != nil {
			return e
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return nil
}

// The wrapper writes this structured payload to a private local file and runs
// its fixed read command. User text is never interpolated into shell syntax.
func teamReactionShellPayload(inputs []agentproto.Input) (string, error) {
	payload, _, err := agentproto.QueuedInputShellPayload(inputs)
	return payload, err
}
