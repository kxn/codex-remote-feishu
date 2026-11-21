package workservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Only named management/read-only use cases call this helper. The ordinary
// withWorker boundary rejects control contexts even after the project resumes.
func (s *Service) withManagementWorker(ctx context.Context, token, run string, fn func(*workstore.Tx, Caller) error) error {
	claims, err := s.signer.Verify(token)
	if err != nil {
		return err
	}
	var control bool
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		wc, e := tx.GetContext(claims.ContextID)
		control = wc.Purpose == work.PurposeControl
		return e
	})
	if err != nil {
		return err
	}
	if !control {
		return s.withWorker(ctx, token, run, fn)
	}
	return s.withWorkerReceipt(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		if err := s.authorizeControlCaller(tx, c); err != nil {
			return err
		}
		return fn(tx, c)
	})
}

func (s *Service) authorizeControlCaller(tx *workstore.Tx, c Caller) error {
	wc, err := tx.GetContext(c.Worker.ContextID)
	if err != nil {
		return err
	}
	if err = tx.ValidateControlContext(wc); err != nil {
		return err
	}
	scope, err := tx.GetScope(c.Worker.ScopeID)
	if err != nil {
		return err
	}
	binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
	if err != nil {
		return err
	}
	gates, err := tx.ListPauseGates(scope.ID)
	if err != nil {
		return err
	}
	if _, err = tx.GetRuntimeGate(c.Worker.ID); err == nil {
		return workauth.ErrUnauthorized
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	if err = ensureMember(tx, scope.ID, c.Worker.RoleID); err != nil {
		return err
	}
	if scope.Kind == work.ScopePersonal {
		return workauth.AuthorizePersonalControlRun(c.Claims, c.Attempt.RunRef.RunID, c.Worker, c.Attempt, binding, wc, scope, gates)
	}
	project, err := tx.GetProject(scope.ProjectID)
	if err != nil {
		return err
	}
	return workauth.AuthorizeControlRun(c.Claims, c.Attempt.RunRef.RunID, c.Worker, c.Attempt, binding, wc, project, gates)
}

type ControlConsumeRequest struct {
	ManagementOperationID string `json:"management_operation_id"`
	Key                   string `json:"key"`
	SourceMessageID       string `json:"source_message_id"`
}

// ConsumeControlInput acknowledges only the original business copy of this
// run's human management instruction. Other retained work is untouched.
func (s *Service) ConsumeControlInput(ctx context.Context, token, run string, req ControlConsumeRequest) (Result, error) {
	var result Result
	err := s.withManagementWorker(ctx, token, run, func(tx *workstore.Tx, c Caller) error {
		wc, e := tx.GetContext(c.Worker.ContextID)
		if e != nil {
			return e
		}
		if wc.Purpose != work.PurposeControl {
			return workauth.ErrUnauthorized
		}
		if _, _, e = currentHumanInstruction(tx, c, req.SourceMessageID); e != nil {
			return e
		}
		if e = s.validateControlManagementEvidence(tx, c, req); e != nil {
			return e
		}
		result, e = s.mutate(tx, c, "control.consume", req.Key, req.SourceMessageID, req, func() (string, int64, error) {
			for _, id := range c.Attempt.InputIDs {
				input, e := tx.GetInbox(id)
				if e != nil {
					return "", 0, e
				}
				if input.MessageID != req.SourceMessageID || input.ControlOfInputID == "" {
					continue
				}
				original, e := tx.GetInbox(input.ControlOfInputID)
				if e != nil {
					return "", 0, e
				}
				if original.ScopeID != c.Worker.ScopeID || original.MessageID != input.MessageID {
					return "", 0, workstore.ErrScopeDenied
				}
				if original.State != work.InboxPending {
					return "", 0, fmt.Errorf("original instruction is no longer pending")
				}
				previous := original.Revision
				original.Revision++
				original.State = work.InboxCancelled
				if e = tx.PutInbox(original, previous); e != nil {
					return "", 0, e
				}
				return original.ID, original.Revision, nil
			}
			return "", 0, workauth.ErrUnauthorized
		})
		return e
	})
	return result, err
}

func (s *Service) validateControlManagementEvidence(tx *workstore.Tx, c Caller, req ControlConsumeRequest) error {
	if req.ManagementOperationID == "" {
		return fmt.Errorf("control consume needs an actual management operation receipt")
	}
	op, err := tx.GetOperation(req.ManagementOperationID)
	if err != nil {
		return err
	}
	if op.ScopeID != c.Worker.ScopeID || op.ContextID != c.Worker.ContextID || op.Actor != (work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}) || op.State != work.OperationConfirmed {
		return workauth.ErrUnauthorized
	}
	if !c.Attempt.CreatedAt.IsZero() && op.CreatedAt.Before(c.Attempt.CreatedAt) {
		return workauth.ErrUnauthorized
	}
	request, err := tx.GetContent(op.ScopeID, op.RequestRef)
	if err != nil {
		return err
	}
	if request.SHA256 != op.RequestHash {
		return workstore.ErrIdempotencyConflict
	}
	switch op.Kind {
	case "project.resume", "project.pause", "project.archive", "project.reopen", "agreement.apply", "private.control.resume":
		var source struct {
			SourceMessageID string `json:"source_message_id"`
		}
		if err = json.Unmarshal(request.Data, &source); err != nil {
			return err
		}
		if source.SourceMessageID != req.SourceMessageID {
			return workauth.ErrUnauthorized
		}
	case "message.publish":
		var reply PublishMessageRequest
		if err = json.Unmarshal(request.Data, &reply); err != nil {
			return err
		}
		input, err := tx.GetInbox(reply.ReplyToInputID)
		if err != nil {
			return err
		}
		if input.ControlOfInputID == "" || input.MessageID != req.SourceMessageID || input.ClaimedAttemptID != c.Attempt.ID || input.State != work.InboxClaimed {
			return workauth.ErrUnauthorized
		}
		var receipt Result
		if err = json.Unmarshal([]byte(op.ExternalReceipt), &receipt); err != nil {
			return err
		}
		outbox, err := tx.GetOutbox(stableID("outbox", receipt.ObjectID))
		if err != nil {
			return err
		}
		if outbox.ScopeID != op.ScopeID || outbox.OperationID != receipt.ObjectID {
			return workauth.ErrUnauthorized
		}
	default:
		return workauth.ErrUnauthorized
	}
	return nil
}
