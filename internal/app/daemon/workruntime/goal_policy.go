package workruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) queueNativeGoalPolicy(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event, budget int64, source string) error {
	if budget <= 0 {
		return c.gate(tx, carrier, binding.AttemptID, "native_goal_budget_required")
	}
	key, _ := json.Marshal([]any{carrier.Incarnation, event.Journal.Sequence, event.Journal.PayloadHash})
	sum := sha256.Sum256(key)
	id := "goal_policy_" + hex.EncodeToString(sum[:20])
	if _, err := tx.GetOperation(id); err == nil {
		return nil
	} else if !errors.Is(err, workstore.ErrNotFound) {
		return err
	}
	command := agentproto.Command{CommandID: id, Kind: agentproto.CommandThreadGoalSet, Execution: agentproto.CloneExecutionBinding(&binding), Target: agentproto.Target{ThreadID: event.ThreadID}, Goal: agentproto.GoalCommand{Objective: event.ThreadGoal.Objective, Status: "active", TokenBudget: &budget, Purpose: "native_goal_budget:" + source}, IssuedAt: c.options.Clock()}
	raw, err := json.Marshal(command)
	if err != nil {
		return err
	}
	body := work.NewContent(id+":command", binding.ScopeID, "application/json", raw, c.options.Clock())
	if err = tx.PutContent(body); err != nil {
		return err
	}
	raw, err = json.Marshal(event)
	if err != nil {
		return err
	}
	evidence := work.NewContent(id+":source", binding.ScopeID, "application/json", raw, c.options.Clock())
	if err = tx.PutContent(evidence); err != nil {
		return err
	}
	_, _, err = tx.PutOperation(work.Operation{ID: id, ScopeID: binding.ScopeID, ContextID: binding.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "native-goal-policy"}, Kind: "native.goal.policy", TargetID: binding.WorkerSessionID, TargetGeneration: binding.SessionGeneration, RequestRef: body.ID, RequestHash: body.SHA256, IdempotencyKey: id, State: work.OperationPrepared, Steps: []work.OperationStep{{ID: "owned_native_goal", State: work.OperationConfirmed, Receipt: evidence.ID}}, Revision: 1, CreatedAt: c.options.Clock(), UpdatedAt: c.options.Clock()}, 0)
	if err != nil {
		return err
	}
	return c.gate(tx, carrier, binding.AttemptID, "native_goal_budget_required")
}

func (c *Coordinator) RunGoalPolicies(ctx context.Context, limit int) error {
	if c.options.Driver == nil {
		return fmt.Errorf("goal policy native driver unavailable")
	}
	if limit <= 0 || limit > 256 {
		limit = 32
	}
	var pending []work.Operation
	err := c.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		pending, e = tx.ListPreparedNativeGoalPolicies(limit)
		return e
	})
	if err != nil {
		return err
	}
	var failures []error
	for _, operation := range pending {
		var command agentproto.Command
		var instance string
		err = c.store.Within(ctx, func(tx *workstore.Tx) error {
			fresh, e := tx.GetOperation(operation.ID)
			if e != nil {
				return e
			}
			if fresh.Revision != operation.Revision || fresh.State != work.OperationPrepared {
				return workstore.ErrConflict
			}
			worker, e := tx.GetWorker(fresh.TargetID)
			if e != nil {
				return e
			}
			command, e = tx.NativeCommandPayload(fresh.ID, worker.CarrierIncarnation)
			if e != nil {
				return e
			}
			instance = worker.InstanceID
			fresh.State = work.OperationSending
			fresh.Revision++
			fresh.UpdatedAt = c.options.Clock()
			_, _, e = tx.PutOperation(fresh, operation.Revision)
			return e
		})
		if err == nil {
			err = c.options.Driver.SendCommand(ctx, instance, command)
		}
		if err != nil {
			failures = append(failures, err)
			_ = c.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
				fresh, e := tx.GetOperation(operation.ID)
				if e != nil {
					return e
				}
				if fresh.State != work.OperationSending {
					return nil
				}
				before := fresh.Revision
				fresh.Revision++
				fresh.State = work.OperationUnknown
				fresh.LastError = err.Error()
				fresh.UpdatedAt = c.options.Clock()
				_, _, e = tx.PutOperation(fresh, before)
				return e
			})
		}
	}
	return errors.Join(failures...)
}

func (c *Coordinator) authorizedGoalPolicy(tx *workstore.Tx, command agentproto.Command) bool {
	if command.Kind != agentproto.CommandThreadGoalSet || command.Execution == nil || command.Goal.TokenBudget == nil || *command.Goal.TokenBudget <= 0 {
		return false
	}
	operation, err := tx.GetOperation(command.CommandID)
	if err != nil || operation.Kind != "native.goal.policy" || operation.Actor != (work.Actor{Kind: work.ActorSystem, ID: "native-goal-policy"}) || operation.State != work.OperationSending || len(operation.Steps) != 1 {
		return false
	}
	body, err := tx.GetContent(command.Execution.ScopeID, operation.Steps[0].Receipt)
	if err != nil {
		return false
	}
	var event agentproto.Event
	if json.Unmarshal(body.Data, &event) != nil || event.Journal == nil || event.ThreadGoal == nil || event.ThreadID != command.Target.ThreadID || event.ThreadGoal.Objective != command.Goal.Objective || event.TurnID == "" {
		return false
	}
	turn, err := tx.GetNativeTurn(event.Journal.CarrierIncarnation, event.ThreadID, event.TurnID)
	if err != nil || turn.Binding != *command.Execution {
		return false
	}
	recorded, err := tx.GetRuntimeEvent(event.Journal.CarrierIncarnation, event.Journal.Sequence)
	if err != nil {
		return false
	}
	a, err := agentproto.RuntimeEventPayloadHash(recorded)
	return err == nil && a == event.Journal.PayloadHash
}
