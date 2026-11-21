package workengine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

func (e *Engine) send(ctx context.Context, p PendingDispatch) DispatchOutcome {
	result := DispatchOutcome{OperationID: p.Operation.ID, AttemptID: p.Attempt.ID, InstanceID: p.Worker.InstanceID, State: work.OperationSending}
	sendStepID := ""
	err := e.store.Within(ctx, func(tx *workstore.Tx) error {
		current, err := tx.GetOperation(p.Operation.ID)
		if err != nil {
			return err
		}
		if current.State == work.OperationConfirmed {
			return fmt.Errorf("dispatch already confirmed")
		}
		if _, err = readPending(tx, current); err != nil {
			return err
		}
		expected := current.Revision
		if current.State != work.OperationUnknown {
			current.State = work.OperationSending
		}
		sendStepID = fmt.Sprintf("send-%d", current.Revision+1)
		current.Steps = append(current.Steps, work.OperationStep{ID: sendStepID, State: work.OperationSending})
		current.Revision++
		current.UpdatedAt = e.config.Now().UTC()
		_, _, err = tx.PutOperation(current, expected)
		return err
	})
	if err != nil {
		result.Err = err
		return result
	}
	receipt, sendErr := e.dispatcher.Send(ctx, p.Worker.InstanceID, p.Command)
	state := work.OperationUnknown
	if sendErr == nil {
		if err := validateReceipt(p, receipt); err != nil {
			sendErr = err
		} else if receipt.State == DispatchDelivered {
			state = work.OperationConfirmed
		}
	}
	result.State = state
	result.Err = sendErr
	// A cancelled transport context cannot prevent saving its ambiguous result.
	// This bounded independent commit never repeats an external action.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err = e.store.Within(saveCtx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(p.Operation.ID)
		if err != nil {
			return err
		}
		if operation.State == work.OperationConfirmed {
			return nil
		}
		expected := operation.Revision
		operation.State = state
		operation.Revision++
		operation.UpdatedAt = e.config.Now().UTC()
		operation.LastError = ""
		if sendErr != nil {
			operation.LastError = sendErr.Error()
		}
		body, err := json.Marshal(struct {
			Receipt DispatchReceipt `json:"receipt"`
			Error   string          `json:"error,omitempty"`
		}{receipt, operation.LastError})
		if err != nil {
			return err
		}
		content := work.NewContent(fmt.Sprintf("dispatch-receipt:%s:%d", operation.ID, operation.Revision), operation.ScopeID, "application/json", body, operation.UpdatedAt)
		if err = tx.PutContent(content); err != nil {
			return err
		}
		operation.ExternalReceipt = content.ID
		for i := range operation.Steps {
			if operation.Steps[i].ID == sendStepID {
				operation.Steps[i].State = state
				operation.Steps[i].Receipt = content.ID
				break
			}
		}
		_, _, err = tx.PutOperation(operation, expected)
		return err
	})
	if err != nil {
		result.Err = fmt.Errorf("record dispatch receipt: %w", err)
	}
	return result
}
func validateReceipt(p PendingDispatch, r DispatchReceipt) error {
	if r.State != DispatchDelivered && r.State != DispatchUnknown {
		return fmt.Errorf("invalid dispatch receipt state")
	}
	if r.Journal != nil {
		j := r.Journal
		if j.Version != agentproto.JournalProtocolVersion || j.CommandID != p.Command.CommandID || j.PayloadHash != p.Operation.RequestHash || j.CarrierIncarnation != p.Worker.CarrierIncarnation {
			return fmt.Errorf("dispatch journal receipt binding mismatch")
		}
		switch j.Stage {
		case "prepared", "written":
		case "child_write_started", "partially_written", "unknown":
			if r.State == DispatchDelivered {
				return fmt.Errorf("ambiguous native journal cannot confirm delivery")
			}
		default:
			return fmt.Errorf("unknown journal receipt stage")
		}
	}
	if r.State == DispatchDelivered && r.Journal == nil && r.TransportReceipt == "" {
		return fmt.Errorf("delivered command lacks transport or journal receipt")
	}
	return nil
}

// ReconcilePending reconstructs frozen commands after a restart. It never sends
// them; a trusted caller may query the journal or explicitly Replay one command.
func (e *Engine) ReconcilePending(ctx context.Context) ([]PendingDispatch, error) {
	pending := []PendingDispatch{}
	err := e.store.Within(ctx, func(tx *workstore.Tx) error {
		operations, err := tx.ListPendingDispatchOperations()
		if err != nil {
			return err
		}
		for _, operation := range operations {
			value, err := readPending(tx, operation)
			if err != nil {
				value = PendingDispatch{Operation: operation, Issue: err.Error()}
			}
			pending = append(pending, value)
		}
		return nil
	})
	return pending, err
}
func (e *Engine) Replay(ctx context.Context, operationID string) (DispatchOutcome, error) {
	var pending PendingDispatch
	err := e.store.Within(ctx, func(tx *workstore.Tx) error {
		operation, err := tx.GetOperation(operationID)
		if err != nil {
			return err
		}
		if operation.Kind != "native.dispatch" || operation.State == work.OperationConfirmed || operation.State == work.OperationTerminal {
			return fmt.Errorf("operation is not pending dispatch")
		}
		pending, err = readPending(tx, operation)
		return err
	})
	if err != nil {
		return DispatchOutcome{}, err
	}
	return e.send(ctx, pending), nil
}
func readPending(tx *workstore.Tx, operation work.Operation) (PendingDispatch, error) {
	var result PendingDispatch
	content, err := tx.GetContent(operation.ScopeID, operation.RequestRef)
	if err != nil {
		return result, err
	}
	var frozen dispatchEnvelope
	if err = json.Unmarshal(content.Data, &frozen); err != nil {
		return result, err
	}
	if frozen.Command.Execution == nil {
		return result, fmt.Errorf("dispatch command lacks frozen execution")
	}
	attempt, err := tx.GetAttempt(frozen.Command.Execution.AttemptID)
	if err != nil {
		return result, err
	}
	worker, err := tx.GetWorker(attempt.RunRef.WorkerID)
	if err != nil {
		return result, err
	}
	if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
		return result, err
	}
	raw, err := json.Marshal(frozen.Command)
	if err != nil {
		return result, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != operation.RequestHash {
		return result, fmt.Errorf("pending command body hash changed")
	}
	if operation.ID != attempt.CommandID || operation.ID != frozen.Command.CommandID || operation.TargetID != worker.ID || operation.TargetGeneration != worker.Generation || worker.InstanceID != frozen.InstanceID || worker.InstanceGeneration != frozen.InstanceGeneration || worker.ProcessGeneration != frozen.ProcessGeneration || worker.CarrierIncarnation != frozen.CarrierIncarnation {
		return result, fmt.Errorf("pending command carrier or operation binding changed")
	}
	if attempt.State == work.AttemptCompleted || attempt.State == work.AttemptInterrupted || attempt.State == work.AttemptFailed {
		return result, fmt.Errorf("terminal attempt cannot replay native dispatch")
	}
	result = PendingDispatch{Operation: operation, Attempt: attempt, Worker: worker, Command: frozen.Command}
	return result, nil
}
