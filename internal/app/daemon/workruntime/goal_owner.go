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
	if (command.Kind != agentproto.CommandThreadGoalGet && command.Kind != agentproto.CommandThreadGoalClear) || event.RequestID == "" || event.Execution == nil || event.Execution.ValidateOwner() != nil || command.Binding != *event.Execution || !carrier.Owner.SameOwner(*event.Execution) || command.SessionID == "" || event.ThreadID != command.SessionID {
		return fmt.Errorf("idle Goal receipt lacks an exact registered session command")
	}
	if event.ErrorMessage != "" {
		return c.failGoalReceipt(tx, event)
	}
	if err = c.confirmGoalReceipt(tx, event); err != nil {
		return err
	}
	if command.Kind == agentproto.CommandThreadGoalGet {
		return nil
	}
	if !event.GoalCleared && !event.GoalMissing {
		return fmt.Errorf("idle Goal clear has no native success proof")
	}
	grant, err := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	grant.Active = false
	grant.Reconciled = true
	return tx.PutContinuationGrant(grant)
}
