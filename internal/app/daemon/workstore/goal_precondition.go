package workstore

import (
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

var ErrGoalPreconditionStale = errors.New("会话目标已改变")

func (t *Tx) ValidateGoalPrecondition(expected *agentproto.GoalPrecondition, worker work.WorkerSession, carrier CarrierRegistration, sessionID string) error {
	if expected == nil || expected.Incarnation == "" || expected.SessionID == "" || expected.GoalVersion <= 0 || expected.Objective == "" || expected.TokenBudget <= 0 || expected.CommandID == "" || !carrier.Matches(worker) || carrier.Incarnation != expected.Incarnation || worker.NativeSessionID != expected.SessionID || sessionID != expected.SessionID || worker.Generation != expected.WorkerGeneration || worker.ControllerGeneration != expected.ControllerGeneration {
		return fmt.Errorf("%w：执行身份已更新，请刷新后重新操作", ErrGoalPreconditionStale)
	}
	if expected.Status != "active" && expected.Status != "paused" {
		return ErrScopeDenied
	}
	grant, err := t.GetContinuationGrant(expected.Incarnation, expected.SessionID)
	if err != nil {
		return err
	}
	if !grant.Reconciled || grant.GoalVersion != expected.GoalVersion || grant.CommandID != expected.CommandID || grant.Objective != expected.Objective || grant.TokenBudget != expected.TokenBudget || grant.Status != expected.Status || !carrier.Owner.SameOwner(grant.Binding) {
		return fmt.Errorf("%w，请刷新后重新操作", ErrGoalPreconditionStale)
	}
	return nil
}

// Run before claiming and again before the first native prompt. A queued resume
// may never silently become a new goal on a rebuilt or different session.
func (t *Tx) ValidateSessionGoalInputs(attempt work.ExecutionAttempt, worker work.WorkerSession) error {
	for _, id := range attempt.InputIDs {
		input, err := t.GetInbox(id)
		if err != nil {
			return err
		}
		if input.SessionGoalIntentID == "" {
			continue
		}
		intent, err := t.GetSessionGoalIntent(input.SessionGoalIntentID)
		if err != nil {
			return err
		}
		if intent.Goal.Expected == nil {
			continue
		}
		carrier, err := t.GetCarrier(worker.CarrierIncarnation)
		if err != nil {
			return err
		}
		if intent.Goal.Status != "active" || intent.Goal.Expected.Status != "paused" || intent.Goal.Objective != intent.Goal.Expected.Objective || intent.Goal.TokenBudget == nil || *intent.Goal.TokenBudget != intent.Goal.Expected.TokenBudget {
			return ErrScopeDenied
		}
		if err = t.ValidateGoalPrecondition(intent.Goal.Expected, worker, carrier, worker.NativeSessionID); err != nil {
			return err
		}
	}
	return nil
}
