// Package workservice is the application boundary shared by agent CLI and UI.
// It owns transactional use cases, never a second in-memory task state machine.
package workservice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Service struct {
	resourceCalls        *resourceCallActivity
	nativeDispatcher     NativeCommandDispatcher
	store                *workstore.Store
	signer               *workauth.Signer
	now                  func() time.Time
	connectors           map[string]ResourceConnector
	connectorResolver    ResourceConnectorResolver
	managementAuthorizer ManagementAuthorizer
}

func New(store *workstore.Store, signer *workauth.Signer, now func() time.Time) (*Service, error) {
	if store == nil || signer == nil {
		return nil, fmt.Errorf("work store and credential signer are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Service{store: store, signer: signer, now: now, resourceCalls: &resourceCallActivity{}}, nil
}
func newID(prefix string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(b), nil
}

type Caller struct {
	Claims  workauth.Claims
	Worker  work.WorkerSession
	Attempt work.ExecutionAttempt
}

// withWorker verifies both the signature and current authority under the same
// transaction as the command. Asynchronous effects retain their accepted caller
// in an Operation and do not impersonate the next active attempt.
func (s *Service) withWorker(ctx context.Context, token, runRef string, fn func(*workstore.Tx, Caller) error) error {
	return s.withWorkerMode(ctx, token, runRef, false, false, fn)
}
func (s *Service) withWorkerReceipt(ctx context.Context, token, runRef string, fn func(*workstore.Tx, Caller) error) error {
	return s.withWorkerMode(ctx, token, runRef, true, false, fn)
}
func (s *Service) withContentWorker(ctx context.Context, token, runRef string, fn func(*workstore.Tx, Caller) error) error {
	return s.withWorkerMode(ctx, token, runRef, false, true, fn)
}
func (s *Service) withWorkerMode(ctx context.Context, token, runRef string, receiptOnly, bootstrapContent bool, fn func(*workstore.Tx, Caller) error) error {
	claims, err := s.signer.Verify(token)
	if err != nil {
		return err
	}
	if claims.Purpose != "worker" {
		return workauth.ErrUnauthorized
	}
	return s.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker(claims.WorkerID)
		if err != nil {
			return workauth.ErrUnauthorized
		}
		attempt, err := tx.GetAttemptByRunRef(runRef)
		if err != nil || attempt.RunRef.WorkerID != worker.ID {
			return workauth.ErrUnauthorized
		}
		effectiveIDs, err := tx.EffectiveAttemptInputIDs(attempt.ID)
		if err != nil {
			return err
		}
		attempt.InputIDs = effectiveIDs
		binding, err := tx.GetRoleBotBinding(claims.RoleID)
		if err != nil {
			return workauth.ErrUnauthorized
		}
		wc, err := tx.GetContext(worker.ContextID)
		if err != nil {
			return err
		}
		if wc.Purpose == work.PurposeReview {
			return workauth.ErrUnauthorized
		}
		if receiptOnly {
			if err = workauth.AuthorizeReport(claims, runRef, worker, attempt, binding); err != nil {
				return err
			}
			return fn(tx, Caller{Claims: claims, Worker: worker, Attempt: attempt})
		}
		if wc.Purpose == work.PurposeControl {
			return workauth.ErrUnauthorized
		}
		if bootstrapContent {
			scope, err := tx.GetScope(worker.ScopeID)
			if err != nil {
				return err
			}
			if scope.Kind == work.ScopeBootstrap {
				caller, _, err := s.authorizeBootstrapWorker(tx, claims, runRef)
				if err != nil {
					return err
				}
				return fn(tx, caller)
			}
		}
		gates, err := tx.ListPauseGates(worker.ScopeID)
		if err != nil {
			return err
		}
		if err = workauth.AuthorizeRun(claims, runRef, worker, attempt, binding, gates); err != nil {
			return err
		}
		if err = ensureMember(tx, worker.ScopeID, worker.RoleID); err != nil {
			return err
		}
		if attempt.TaskID != "" {
			task, err := tx.GetTask(attempt.TaskID)
			if err != nil {
				return err
			}
			if task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.OwnerRoleID != worker.RoleID || task.State == work.TaskCancelled || task.State == work.TaskDone {
				return workauth.ErrUnauthorized
			}
		}
		return fn(tx, Caller{Claims: claims, Worker: worker, Attempt: attempt})
	})
}

type ContextView struct {
	Scope             work.WorkScope        `json:"scope"`
	Role              work.Role             `json:"role"`
	Context           work.WorkContext      `json:"context"`
	Worker            work.WorkerSession    `json:"worker"`
	Attempt           work.ExecutionAttempt `json:"attempt"`
	Tasks             []work.Task           `json:"tasks"`
	Inbox             []work.Inbox          `json:"inbox"`
	PendingInputCount int                   `json:"pending_input_count"`
}

func (s *Service) Context(ctx context.Context, token, runRef string) (ContextView, error) {
	var result ContextView
	err := s.withManagementWorker(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		var err error
		result.Worker = c.Worker
		result.Attempt = c.Attempt
		if result.Scope, err = tx.GetScope(c.Worker.ScopeID); err != nil {
			return err
		}
		if result.Role, err = tx.GetRole(c.Worker.RoleID); err != nil {
			return err
		}
		if result.Context, err = tx.GetContext(c.Worker.ContextID); err != nil {
			return err
		}
		result.Tasks = []work.Task{}
		if c.Attempt.TaskID != "" {
			task, err := tx.GetTask(c.Attempt.TaskID)
			if err != nil {
				return err
			}
			result.Tasks = append(result.Tasks, task)
		}
		inputs, err := tx.ListInbox(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		result.Inbox = []work.Inbox{}
		for _, input := range inputs {
			if input.RecipientRoleID == c.Worker.RoleID && (input.RecipientContextID == "" || input.RecipientContextID == c.Worker.ContextID) {
				if input.State == work.InboxPending {
					result.PendingInputCount++
				}
				if input.ClaimedAttemptID == c.Attempt.ID {
					result.Inbox = append(result.Inbox, input)
				}
			}
		}
		return nil
	})
	return result, err
}
