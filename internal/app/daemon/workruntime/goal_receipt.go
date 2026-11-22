package workruntime

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) confirmGoalReceipt(tx *workstore.Tx, event agentproto.Event) error {
	if event.CommandID == "" || event.Kind != agentproto.EventThreadGoalCommandResult || event.ErrorMessage != "" {
		return nil
	}
	operation, err := tx.GetOperation(event.CommandID)
	if err != nil {
		return err
	}
	if operation.Kind != "native.goal.policy" && operation.Kind != "session.goal" && operation.Kind != "native.human.goal" && operation.Kind != "native.user.goal" {
		return nil
	}
	if operation.State == work.OperationConfirmed {
		return nil
	}
	if operation.State != work.OperationSending && operation.State != work.OperationUnknown {
		return nil
	}
	command, err := tx.NativeCommandPayload(event.CommandID, event.Journal.CarrierIncarnation)
	if err != nil {
		return err
	}
	switch command.Kind {
	case agentproto.CommandThreadGoalSet:
		if command.Goal.Status != "" && (event.ThreadGoal == nil || event.ThreadGoal.Status != command.Goal.Status) {
			return fmt.Errorf("native Goal response differs from requested status")
		}
		if event.ThreadGoal == nil || command.Goal.TokenBudget == nil || event.ThreadGoal.TokenBudget == nil || *command.Goal.TokenBudget != *event.ThreadGoal.TokenBudget || command.Goal.Objective != event.ThreadGoal.Objective {
			return fmt.Errorf("native Goal response differs from the canonical requested policy")
		}
	case agentproto.CommandThreadGoalClear:
		if !event.GoalCleared && !event.GoalMissing {
			return fmt.Errorf("native Goal clear has no exact success proof")
		}
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	evidence := work.NewContent(event.CommandID+":native-result", operation.ScopeID, "application/json", raw, c.options.Clock())
	if err = tx.PutContent(evidence); err != nil {
		return err
	}
	before := operation.Revision
	operation.Revision++
	operation.State = work.OperationConfirmed
	operation.ExternalReceipt = evidence.ID
	operation.UpdatedAt = c.options.Clock()
	_, _, err = tx.PutOperation(operation, before)
	return err
}
