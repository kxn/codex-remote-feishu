package workservice

import (
	"context"
	"errors"
	"strconv"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func humanSource(message work.Message) work.SourceRef {
	return work.SourceRef{Kind: "message", ID: message.ID, Version: strconv.FormatInt(message.Revision, 10), EvidenceRef: message.BodyRef}
}

// withManagement authenticates before a policy call and again in the actual
// mutation transaction. The authorizer sees a real current human source, never
// an actor or authority supplied in the model's JSON payload.
func (s *Service) withManagement(ctx context.Context, token, run, sourceID, kind, targetID string, fn func(*workstore.Tx, Caller, work.Message) error) error {
	var source work.Message
	ownerAllowed := false
	err := s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		var err error
		source, _, err = currentHumanInstruction(tx, c, sourceID)
		if err != nil {
			return err
		}
		scope, err := tx.GetScope(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeBootstrap && scope.PrincipalID != source.Source.ID {
			return workauth.ErrUnauthorized
		}
		if kind == "role.global-description" && scope.Kind != work.ScopePersonal {
			return workauth.ErrUnauthorized
		}
		if kind == "team.manage" {
			owner, e := tx.GetTeamOwnership(targetID)
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
			if e == nil && owner.PrincipalID == source.Source.ID {
				ownerAllowed = true
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !ownerAllowed {
		if s.managementAuthorizer == nil {
			return workauth.ErrUnauthorized
		}
		if err = s.managementAuthorizer.AuthorizeManagement(ctx, source.Source.ID, kind, targetID, humanSource(source)); err != nil {
			return err
		}
	}
	return s.withContentWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		current, _, err := currentHumanInstruction(tx, c, sourceID)
		if err != nil {
			return err
		}
		if current.Source != source.Source || current.ScopeID != source.ScopeID || current.Revision != source.Revision || current.BodyRef != source.BodyRef {
			return workauth.ErrUnauthorized
		}
		if ownerAllowed {
			owner, err := tx.GetTeamOwnership(targetID)
			if err != nil {
				return err
			}
			if owner.PrincipalID != current.Source.ID {
				return workauth.ErrUnauthorized
			}
		}
		return fn(tx, c, current)
	})
}
