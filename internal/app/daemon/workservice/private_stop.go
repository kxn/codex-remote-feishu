package workservice

import (
	"context"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type TrustedPrivateStopRequest struct {
	Key                 string
	Source              workstore.PrivateActionSource
	ExpectedNavRevision int64
}

func (s *Service) TrustedPrivateStop(ctx context.Context, req TrustedPrivateStopRequest) (Result, error) {
	return s.trustedPrivateControl(ctx, req, false)
}

// This only reopens a legacy private pause created before Stop was scoped to
// the occupied attempt. Project lifecycle actions use their own authority.
func (s *Service) TrustedPrivateResume(ctx context.Context, req TrustedPrivateStopRequest) (Result, error) {
	return s.trustedPrivateControl(ctx, req, true)
}

func (s *Service) trustedPrivateControl(ctx context.Context, req TrustedPrivateStopRequest, resume bool) (result Result, err error) {
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, proof, e := tx.ValidatePrivateAction(req.Source)
		if e != nil {
			return e
		}
		kind := "private.stop"
		if resume {
			kind = "private.resume"
		}
		result, e = s.privateMutate(tx, scope, kind, req.Key, scope.ID, req, func() (string, int64, error) {
			nav, e := tx.GetPrivateNavigation(scope.ID)
			if e != nil {
				return "", 0, e
			}
			if nav.Revision != req.ExpectedNavRevision {
				return "", 0, workstore.ErrConflict
			}
			contextID := nav.FrontContextID
			if target, quoted, e := quotedPrivateCommandContext(tx, req.Source); e != nil {
				return "", 0, e
			} else if quoted {
				contextID = target.ID
			}
			front, e := tx.GetContext(contextID)
			if e != nil {
				return "", 0, e
			}
			if front.ScopeID != scope.ID || front.RoleID != scope.RoleID || front.Purpose == work.PurposeControl || front.Purpose == work.PurposeReview {
				return "", 0, workstore.ErrScopeDenied
			}
			cutoff, e := privateControlReceivedAt(tx, req.Source)
			if e != nil {
				return "", 0, e
			}
			if !resume {
				if e = s.requestPrivateStop(tx, scope, contextID, proof, cutoff); e != nil {
					return "", 0, e
				}
			}
			// A previously created permanent private stop must not strand the
			// next user message after they use this control.
			id := workstore.LegacyPrivateStopGateID(scope.ID, contextID)
			gate, e := tx.GetPauseGate(id)
			if e == nil && gate.State == work.GateClosed {
				before := gate.Revision
				gate.Revision++
				gate.State = work.GateOpen
				e = tx.PutPauseGate(gate, before)
			}
			if e != nil && !errors.Is(e, workstore.ErrNotFound) {
				return "", 0, e
			}
			return contextID, nav.Revision, nil
		})
		return e
	})
	return
}

func privateControlReceivedAt(tx *workstore.Tx, source workstore.PrivateActionSource) (time.Time, error) {
	switch {
	case source.CommandID != "":
		command, err := tx.GetPrivateCommand(source.CommandID)
		if err != nil {
			return time.Time{}, err
		}
		raw, err := tx.GetIngress(command.IngressID)
		return raw.Envelope.ReceivedAt, err
	case source.InteractionID != "":
		interaction, err := tx.GetHumanInteraction(source.InteractionID)
		return interaction.Envelope.ReceivedAt, err
	case source.MenuInteractionID != "":
		menu, err := tx.GetMenuInteraction(source.MenuInteractionID)
		return menu.Envelope.ReceivedAt, err
	default:
		return time.Time{}, workstore.ErrScopeDenied
	}
}

func (s *Service) requestPrivateStop(tx *workstore.Tx, scope work.WorkScope, contextID string, proof work.SourceRef, cutoff time.Time) error {
	workers, err := tx.ListWorkers(scope.ID)
	if err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.ContextID != contextID || worker.Controller != work.ControllerAutomatic {
			continue
		}
		attempt, e := tx.GetOccupiedAttemptForWorker(worker.ID)
		if errors.Is(e, workstore.ErrNotFound) {
			continue
		}
		if e != nil {
			return e
		}
		older, e := tx.PrivateAttemptPredatesStop(attempt, scope, cutoff)
		if e != nil {
			return e
		}
		if !older {
			continue
		}
		if e = tx.RecordPrivateAttemptStop(attempt, scope, proof, cutoff, s.now()); e != nil {
			return e
		}
	}
	return tx.CancelOlderPrivateInputs(scope, contextID, cutoff)
}
