package workruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type goalSource struct {
	CommandID, Kind, TurnID, BudgetSource string
	Source                                work.SourceRef
	Budget, Version                       int64
	Objective                             string
	Pending                               bool
}

func (c *Coordinator) resolveGoalSource(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event, active bool, version, budget int64, objective string) (goalSource, error) {
	source := goalSource{CommandID: event.CommandID, Kind: "canonical_goal_rpc", BudgetSource: "explicit_command", Budget: budget, Version: version, Objective: objective}
	if event.CommandID != "" {
		op, err := tx.GetOperation(event.CommandID)
		if err != nil {
			return source, err
		}
		if op.Kind == "native.goal.policy" {
			command, e := tx.NativeCommandPayload(event.CommandID, carrier.Incarnation)
			if e != nil {
				return source, e
			}
			source.Kind = "canonical_goal_policy"
			source.BudgetSource = strings.TrimPrefix(command.Goal.Purpose, "native_goal_budget:")
			if len(op.Steps) > 0 {
				evidence, e := tx.GetContent(binding.ScopeID, op.Steps[0].Receipt)
				if e != nil {
					return source, e
				}
				var native agentproto.Event
				if e = json.Unmarshal(evidence.Data, &native); e != nil {
					return source, e
				}
				source.TurnID = native.TurnID
				source.Source = work.SourceRef{Kind: "native_goal_budget_policy", ID: op.ID, Version: native.Journal.PayloadHash, EvidenceRef: evidence.ID}
			}
		}
		return source, nil
	}
	if event.TurnID != "" {
		turn, err := tx.GetNativeTurn(carrier.Incarnation, event.ThreadID, event.TurnID)
		if err == nil && turn.Binding == binding {
			source.Kind = "native_owned_turn"
			source.TurnID = event.TurnID
			source.BudgetSource = "native_explicit"
			if active && budget <= 0 && event.ThreadGoal != nil && event.ThreadGoal.TokenBudget != nil {
				source.Pending = true
				return source, c.gate(tx, carrier, binding.AttemptID, "native_goal_budget_invalid")
			}
			if active && budget <= 0 {
				selected := c.options.DefaultGoalTokenBudget
				policy := "configured_default"
				if old, e := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID); e == nil && old.TokenBudget > 0 {
					selected = old.TokenBudget
					policy = "existing_positive_grant"
				} else if e != nil && !errors.Is(e, workstore.ErrNotFound) {
					return source, e
				}
				source.Pending = true
				return source, c.queueNativeGoalPolicy(tx, carrier, binding, event, selected, policy)
			}
			attempt, e := tx.GetAttempt(binding.AttemptID)
			if e != nil {
				return source, e
			}
			source.CommandID = attempt.CommandID
			raw, e := json.Marshal(event)
			if e != nil {
				return source, e
			}
			id := fmt.Sprintf("native-goal:%s:%d", carrier.Incarnation, event.Journal.Sequence)
			content := work.NewContent(id, binding.ScopeID, "application/json", raw, c.options.Clock())
			if e = tx.PutContent(content); e != nil {
				return source, e
			}
			source.Source = work.SourceRef{Kind: "native_goal_owned_turn", ID: id, Version: event.Journal.PayloadHash, EvidenceRef: id}
			return source, nil
		}
		if err != nil && !errors.Is(err, workstore.ErrNotFound) {
			return source, err
		}
	}
	old, err := tx.GetContinuationGrant(carrier.Incarnation, event.ThreadID)
	if err != nil {
		return source, err
	}
	cleared := event.GoalCleared || (event.ThreadGoal != nil && event.ThreadGoal.Cleared)
	if cleared && version == 0 {
		source.Version = old.GoalVersion
	} else if version < old.GoalVersion || (objective != "" && objective != old.Objective) {
		return source, fmt.Errorf("%w: native continuation mutation lacks an owned source turn", workstore.ErrScopeDenied)
	}
	source.CommandID, source.Kind, source.TurnID, source.BudgetSource, source.Source = old.CommandID, old.AuthorizationKind, old.SourceTurnID, old.BudgetSource, old.Source
	source.Objective = old.Objective
	if !active {
		source.Budget = old.TokenBudget
	} else if budget <= 0 {
		return source, fmt.Errorf("%w: active native goal lost its previously declared budget", workstore.ErrScopeDenied)
	}
	return source, nil
}

func (c *Coordinator) replayGrantedSession(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, session string) error {
	pending, err := tx.QuarantinedSessionEvents(carrier.Incarnation, session)
	if err != nil {
		return err
	}
	for _, event := range pending {
		if event.Kind != agentproto.EventTurnStarted && event.Kind != agentproto.EventTurnCompleted {
			continue
		}
		if deferBoundary, e := c.deferPrivateShellBoundary(tx, carrier, event); e != nil {
			return e
		} else if deferBoundary {
			continue
		}
		resolved, e := c.resolveBinding(tx, carrier, event)
		if e != nil || resolved != binding {
			continue
		}
		if _, e = c.applyEvent(tx, carrier, event, false); e != nil {
			return e
		}
		if e = tx.ResolveQuarantine(carrier.Incarnation, event.Journal.Sequence); e != nil {
			return e
		}
	}
	count, err := tx.CountUnresolvedNativeEvents(carrier.Incarnation)
	if err != nil {
		return err
	}
	if count == 0 {
		for _, reason := range []string{"native_goal_budget_required", "native_ownership_unproven"} {
			if err = tx.ClearRuntimeGate(binding.WorkerSessionID, reason); err != nil {
				return err
			}
		}
	}
	return nil
}
