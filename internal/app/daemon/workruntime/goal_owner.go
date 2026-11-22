package workruntime

import (
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

// Reading an idle session cannot create a continuation grant or invent an
// attempt. Clearing its goal only revokes authorization; occupancy is untouched.
func (c *Coordinator) applyOwnerGoalReceipt(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) error {
	command, err := tx.ResolveNativeCommand(carrier.Incarnation, event.CommandID)
	if err != nil {
		return err
	}
	payload, err := tx.NativeCommandPayload(event.CommandID, carrier.Incarnation)
	if err != nil {
		return err
	}
	if (command.Kind != agentproto.CommandThreadGoalGet && command.Kind != agentproto.CommandThreadGoalClear && !(command.Kind == agentproto.CommandThreadGoalSet && payload.Goal.Status == "paused" && payload.Goal.Expected != nil)) || event.RequestID == "" || command.Binding.ValidateOwner() != nil || (event.Execution != nil && command.Binding != *event.Execution) || !carrier.Owner.SameOwner(command.Binding) || command.SessionID == "" || event.ThreadID != command.SessionID {
		return fmt.Errorf("idle Goal receipt lacks an exact registered session command")
	}
	if event.ErrorMessage != "" {
		return c.failGoalReceipt(tx, event)
	}
	if err = c.confirmGoalReceipt(tx, event); err != nil {
		return err
	}
	if command.Kind == agentproto.CommandThreadGoalGet || command.Kind == agentproto.CommandThreadGoalClear {
		// Old commands without a frozen baseline still retain their actual RPC
		// receipt, but cannot overwrite a newer grant or infer native absence.
		if payload.Goal.Expected == nil {
			return nil
		}
		worker, e := tx.GetWorker(carrier.Owner.WorkerSessionID)
		if e != nil {
			return e
		}
		if e = tx.ValidateGoalControlSnapshot(payload.Goal.Expected, worker, carrier, event.ThreadID); e != nil {
			return nil
		}
		if payload.Goal.Expected.NoGrant {
			return nil
		}
		grant, e := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID)
		if e != nil {
			return e
		}
		if command.Kind == agentproto.CommandThreadGoalGet {
			if event.ThreadGoal != nil && event.ThreadGoal.UpdatedAt > 0 && event.ThreadGoal.UpdatedAt < grant.GoalVersion {
				return nil
			}
			if event.ThreadGoal == nil || event.ThreadGoal.TokenBudget == nil || grant.Objective != event.ThreadGoal.Objective || grant.TokenBudget != *event.ThreadGoal.TokenBudget || event.ThreadGoal.UpdatedAt <= 0 {
				grant.Reconciled = false
				grant.Active = false
			} else {
				grant.Status = event.ThreadGoal.Status
				grant.GoalVersion = event.ThreadGoal.UpdatedAt
				if grant.Status != "active" {
					grant.Active = false
				}
			}
		} else {
			grant.Active = false
			grant.Reconciled = true
			grant.Status = "cleared"
			grant.GoalVersion = 0
			grant.Objective = ""
			grant.TokenBudget = 0
		}
		return tx.PutContinuationGrant(grant)
	}
	if command.Kind == agentproto.CommandThreadGoalSet {
		worker, e := tx.GetWorker(carrier.Owner.WorkerSessionID)
		if e != nil {
			return e
		}
		preconditionErr := tx.ValidateGoalPrecondition(payload.Goal.Expected, worker, carrier, event.ThreadID)
		grant, e := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID)
		if e != nil {
			return e
		}
		if preconditionErr != nil {
			// A native notification can precede its correlated RPC response.
			// It may only reduce the same frozen goal to the exact paused
			// snapshot; a newer/different goal must not be overwritten.
			expected := payload.Goal.Expected
			if !carrier.Matches(worker) || worker.NativeSessionID != event.ThreadID || worker.Generation != expected.WorkerGeneration || worker.ControllerGeneration != expected.ControllerGeneration || grant.Active || !grant.Reconciled || grant.CommandID != expected.CommandID || grant.Objective != expected.Objective || grant.TokenBudget != expected.TokenBudget || grant.Status != "paused" || event.ThreadGoal == nil || grant.GoalVersion != event.ThreadGoal.UpdatedAt {
				return preconditionErr
			}
		}
		if event.ThreadGoal == nil || event.ThreadGoal.UpdatedAt <= 0 {
			return fmt.Errorf("idle Goal pause lacks native version proof")
		}
		grant.Active = false
		grant.Reconciled = true
		grant.Status = "paused"
		grant.GoalVersion = event.ThreadGoal.UpdatedAt
		grant.CommandID = command.CommandID
		grant.ConfirmedSequence = event.Journal.Sequence
		return tx.PutContinuationGrant(grant)
	}
	return nil
}

// Idle non-active notifications revoke authorization. They cannot confirm a
// command or create an execution grant; only the registered RPC receipt can.
func (c *Coordinator) applyIdleGoalObservation(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) (bool, error) {
	if event.ThreadGoal == nil {
		return false, nil
	}
	if event.Execution != nil && !carrier.Owner.SameOwner(*event.Execution) {
		return false, nil
	}
	switch event.ThreadGoal.Status {
	case "paused", "completed", "budget_exhausted":
	default:
		return false, nil
	}
	worker, err := tx.GetWorker(carrier.Owner.WorkerSessionID)
	if err != nil {
		return false, err
	}
	if !carrier.Matches(worker) || worker.NativeSessionID != event.ThreadID || event.ThreadID == "" {
		return false, nil
	}
	occupied, err := tx.HasAnyCarrierOccupancy(carrier)
	if err != nil {
		return false, err
	}
	if occupied {
		return false, nil
	}
	grant, err := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID)
	if errors.Is(err, workstore.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if event.ThreadGoal.UpdatedAt > 0 && event.ThreadGoal.UpdatedAt < grant.GoalVersion {
		return true, nil
	}
	grant.Active = false
	if event.ThreadGoal.TokenBudget == nil || grant.Objective != event.ThreadGoal.Objective || grant.TokenBudget != *event.ThreadGoal.TokenBudget || event.ThreadGoal.UpdatedAt <= 0 || event.ThreadGoal.UpdatedAt < grant.GoalVersion {
		grant.Reconciled = false
	} else {
		grant.Status = event.ThreadGoal.Status
		grant.GoalVersion = event.ThreadGoal.UpdatedAt
	}
	return true, tx.PutContinuationGrant(grant)
}
