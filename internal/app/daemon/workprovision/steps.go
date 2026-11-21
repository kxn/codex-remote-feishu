package workprovision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type effect struct {
	ID, Version string
	Data        any
}

func decodeStep[T any](step workstore.ProvisionStep) (T, error) {
	var value T
	err := json.Unmarshal(step.Receipt, &value)
	return value, err
}

func (s *Service) step(ctx context.Context, state snapshot, key string, identity work.RoleBotBinding, intent any, write bool, call func(context.Context, json.RawMessage) (effect, error)) (workstore.ProvisionStep, error) {
	var step workstore.ProvisionStep
	invoke := false
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := s.current(tx, state); err != nil {
			return err
		}
		if write {
			if err := s.allowEstablishmentEffect(tx, state); err != nil {
				return err
			}
		}
		op, err := tx.GetOperation(state.operation.ID)
		if err != nil {
			return err
		}
		if cancellationRequested(op) && (write || !strings.HasPrefix(key, "recovery.")) {
			return ErrCancelled
		}
		existing, err := tx.GetProvisionStep(state.operation.ID, key)
		if errors.Is(err, workstore.ErrNotFound) {
			data, e := json.Marshal(intent)
			if e != nil {
				return e
			}
			step = workstore.ProvisionStep{ID: workstore.ProvisionStepID(state.operation.ID, key), ScopeID: state.operation.ScopeID, OperationID: state.operation.ID, StepID: key, Identity: identity, State: work.OperationPrepared, Intent: data, Revision: 1, UpdatedAt: s.now()}
			if err = tx.PutProvisionStep(step, 0); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			step = existing
		}
		if step.Identity != identity {
			return ErrStale
		}
		switch step.State {
		case work.OperationConfirmed:
			return nil
		case work.OperationUnknown:
			if write {
				return ErrUnknown
			}
			previous := step.Revision
			step.Revision++
			step.State = work.OperationRetryable
			if err = tx.PutProvisionStep(step, previous); err != nil {
				return err
			}
		case work.OperationSending:
			previous := step.Revision
			step.Revision++
			step.UpdatedAt = s.now()
			if write {
				step.State = work.OperationUnknown
				step.Blocker = "此前请求未留下确定回执；恢复前须核查实际对象 ID"
			} else {
				step.State = work.OperationRetryable
			}
			if err = tx.PutProvisionStep(step, previous); err != nil {
				return err
			}
			if write {
				return nil
			}

		case work.OperationTerminal:
			return fmt.Errorf("provisioning step is terminal: %s", step.Blocker)
		}
		previous := step.Revision
		step.Revision++
		step.State = work.OperationSending
		step.UpdatedAt = s.now()
		if err = tx.PutProvisionStep(step, previous); err != nil {
			return err
		}
		invoke = true
		return nil
	})
	if err != nil {
		return step, err
	}
	if !invoke {
		if step.State == work.OperationConfirmed {
			return step, nil
		}
		return step, ErrUnknown
	}
	receipt, callErr := call(ctx, step.Intent)
	saveCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	saveErr := s.store.Within(saveCtx, func(tx *workstore.Tx) error {
		current, err := tx.GetProvisionStep(state.operation.ID, key)
		if err != nil {
			return err
		}
		previous := current.Revision
		if current.State == work.OperationConfirmed {
			step = current
			return nil
		}
		current.Revision++
		current.UpdatedAt = s.now()
		if receipt.ID != "" {
			current.ExternalID = receipt.ID
		}
		if receipt.Version != "" {
			current.Version = receipt.Version
		}
		if receipt.Data != nil {
			current.Receipt, err = json.Marshal(receipt.Data)
			if err != nil {
				return err
			}
		}
		if callErr == nil {
			current.State = work.OperationConfirmed
			current.Blocker = ""
			if len(current.Receipt) == 0 {
				current.Receipt = json.RawMessage(`{}`)
			}
		} else {
			current.Blocker = callErr.Error()
			if write {
				current.State = work.OperationUnknown
			} else {
				current.State = work.OperationRetryable
			}
		}
		if err = tx.PutProvisionStep(current, previous); err != nil {
			return err
		}
		step = current
		return nil
	})
	if saveErr != nil {
		return step, fmt.Errorf("record provisioning receipt: %w", saveErr)
	}
	return step, callErr
}
func (s *Service) confirmKnown(ctx context.Context, state snapshot, step workstore.ProvisionStep, receipt effect) (workstore.ProvisionStep, error) {
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetProvisionStep(step.OperationID, step.StepID)
		if err != nil {
			return err
		}
		if current.State == work.OperationConfirmed {
			step = current
			return nil
		}
		if current.ExternalID != "" && current.ExternalID != receipt.ID {
			return fmt.Errorf("observed external ID differs from the original receipt")
		}
		previous := current.Revision
		current.Revision++
		current.State = work.OperationConfirmed
		if receipt.ID != "" {
			current.ExternalID = receipt.ID
		}
		if receipt.Version != "" {
			current.Version = receipt.Version
		}
		current.Blocker = ""
		current.UpdatedAt = s.now()
		current.Receipt, err = json.Marshal(receipt.Data)
		if err != nil {
			return err
		}
		if err = tx.PutProvisionStep(current, previous); err != nil {
			return err
		}
		step = current
		return nil
	})
	return step, err
}
