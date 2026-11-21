package workruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) noteNativeGoalBudgetStop(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event) error {
	identity, _ := json.Marshal([]any{carrier.Incarnation, event.ThreadID, event.ThreadGoal.CreatedAt, event.ThreadGoal.Objective})
	sum := sha256.Sum256(identity)
	id := "native_goal_budget_stop_" + hex.EncodeToString(sum[:20])
	if err := c.gate(tx, carrier, binding.AttemptID, "native_goal_budget_exhausted"); err != nil {
		return err
	}
	if err := tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: binding.WorkerSessionID, ScopeID: binding.ScopeID, AttemptID: binding.AttemptID, Reason: "native_goal_budget_exhausted", RecoveryReason: "native_goal_budget_exhausted", ResponsibleRoleID: binding.RoleID, CreatedAt: c.options.Clock()}); err != nil {
		return err
	}
	if _, err := tx.GetMessage(id + ":message"); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	evidence := work.NewContent(id+":evidence", binding.ScopeID, "application/json", raw, c.options.Clock())
	if err = tx.PutContent(evidence); err != nil {
		return err
	}
	text := []byte("本次会话续航预算已耗尽，未完成工作仍保留。先核对已经完成的操作和未处理输入，再由负责人明确恢复；这不表示项目目标完成或验收通过。")
	body := work.NewContent(id+":body", binding.ScopeID, "text/plain", text, c.options.Clock())
	if err = tx.PutContent(body); err != nil {
		return err
	}
	message := work.Message{ID: id + ":message", ScopeID: binding.ScopeID, ConversationID: "native-goal:" + binding.ContextID, Source: work.Actor{Kind: work.ActorSystem, ID: "native-goal-runtime"}, BodyRef: body.ID, DerivedFrom: &work.SourceRef{Kind: "native_goal_budget_exhausted", ID: event.ThreadID, Version: fmt.Sprint(event.Journal.Sequence), EvidenceRef: evidence.ID}, Revision: 1, CreatedAt: c.options.Clock()}
	_, _, err = tx.AcceptMessage(message, []work.Inbox{{ID: id + ":inbox", ScopeID: binding.ScopeID, RecipientContextID: binding.ContextID, RecipientRoleID: binding.RoleID, EventID: id, State: work.InboxPending, AvailableAt: c.options.Clock(), Revision: 1}})
	return err
}
