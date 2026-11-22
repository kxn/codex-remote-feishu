package workstore

import (
	"encoding/json"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

// A processing report cannot claim that an explicit queued goal took effect
// merely because its prompt ran. Only the exact successful native RPC counts.
func (t *Tx) ValidateSessionGoalInputConsumption(input work.Inbox) error {
	if input.SessionGoalIntentID == "" {
		return nil
	}
	intent, err := t.GetSessionGoalIntent(input.SessionGoalIntentID)
	if err != nil {
		return err
	}
	if intent.InputID != input.ID || intent.ScopeID != input.ScopeID || intent.ContextID != input.RecipientContextID || intent.AttemptID != input.ClaimedAttemptID || intent.CommandID == "" {
		return ErrScopeDenied
	}
	op, err := t.GetOperation(intent.CommandID)
	if err != nil {
		return err
	}
	if op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
		return fmt.Errorf("queued session goal has no successful native receipt")
	}
	native, err := t.GetNativeCommand(intent.CommandID)
	if err != nil {
		return err
	}
	body, err := t.GetContent(intent.ScopeID, op.ExternalReceipt)
	if err != nil {
		return err
	}
	var event agentproto.Event
	if err = json.Unmarshal(body.Data, &event); err != nil {
		return err
	}
	if event.Kind != agentproto.EventThreadGoalCommandResult || event.CommandID != intent.CommandID || event.Journal == nil || event.Journal.CarrierIncarnation != native.Incarnation || event.Execution == nil || *event.Execution != native.Binding || native.Binding.AttemptID != intent.AttemptID || event.ErrorMessage != "" || event.ThreadGoal == nil || event.ThreadGoal.Objective != intent.Goal.Objective || (intent.Goal.Status != "" && event.ThreadGoal.Status != intent.Goal.Status) || event.ThreadGoal.TokenBudget == nil || intent.Goal.TokenBudget == nil || *event.ThreadGoal.TokenBudget != *intent.Goal.TokenBudget {
		return ErrScopeDenied
	}
	persisted, err := t.GetRuntimeEvent(native.Incarnation, event.Journal.Sequence)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(persisted, event) {
		return ErrScopeDenied
	}
	return nil
}
