package workruntime

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) failGoalReceipt(tx *workstore.Tx, event agentproto.Event) error {
	if event.RequestID == "" || event.CommandID == "" || event.ErrorMessage == "" {
		return fmt.Errorf("native Goal failure lacks its exact RPC receipt")
	}
	op, err := tx.GetOperation(event.CommandID)
	if err != nil {
		return err
	}
	if op.Kind != "native.goal.policy" && op.Kind != "session.goal" && op.Kind != "native.human.goal" && op.Kind != "native.user.goal" {
		return nil
	}
	if op.State == work.OperationTerminal {
		return nil
	}
	if op.State != work.OperationSending && op.State != work.OperationUnknown {
		return fmt.Errorf("Goal failure targets a command that was never sent")
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	ref := event.CommandID + ":native-failure"
	if err = tx.PutContent(work.NewContent(ref, op.ScopeID, "application/json", raw, c.options.Clock())); err != nil {
		return err
	}
	before := op.Revision
	op.State = work.OperationTerminal
	op.ExternalReceipt = ref
	op.LastError = "native Goal RPC failed; session state still requires actual inspection"
	op.Revision++
	op.UpdatedAt = c.options.Clock()
	_, _, err = tx.PutOperation(op, before)
	return err
}
