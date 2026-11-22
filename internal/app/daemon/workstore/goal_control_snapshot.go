package workstore

import (
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
	"sort"
)

// Capture an authorization snapshot, or an actual read/clear observation when
// no continuation grant exists. Missing local authority is not native absence.
func (t *Tx) CaptureGoalControlSnapshot(worker work.WorkerSession, carrier CarrierRegistration) (*agentproto.GoalPrecondition, error) {
	expected := &agentproto.GoalPrecondition{Incarnation: carrier.Incarnation, SessionID: worker.NativeSessionID, WorkerGeneration: worker.Generation, ControllerGeneration: worker.ControllerGeneration}
	grant, err := t.GetContinuationGrant(carrier.Incarnation, worker.NativeSessionID)
	if err == nil {
		expected.CommandID = grant.CommandID
		expected.GoalVersion = grant.GoalVersion
		expected.Objective = grant.Objective
		expected.TokenBudget = grant.TokenBudget
		expected.Status = grant.Status
		if expected.Status == "" && grant.Active {
			expected.Status = "active"
		}
		return expected, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	expected.NoGrant = true
	expected.Status = "unobserved"
	operations, err := t.ListOperations(worker.ScopeID)
	if err != nil {
		return nil, err
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].CreatedAt.After(operations[j].CreatedAt) })
	for _, op := range operations {
		if op.Kind != "native.user.goal" || op.ContextID != worker.ContextID || op.TargetID != worker.ID || op.TargetGeneration != worker.Generation || op.State != work.OperationConfirmed || op.ExternalReceipt == "" {
			continue
		}
		command, e := t.NativeCommandPayload(op.ID, carrier.Incarnation)
		if e != nil {
			continue
		}
		if (command.Kind != agentproto.CommandThreadGoalGet && command.Kind != agentproto.CommandThreadGoalClear) || command.Execution == nil || !carrier.Owner.SameOwner(*command.Execution) || command.Target.ThreadID != worker.NativeSessionID {
			continue
		}
		registered, e := t.GetNativeCommand(op.ID)
		if e != nil || registered.Incarnation != carrier.Incarnation {
			continue
		}
		event, e := t.NativeGoalCommandResult(op.ID)
		if e != nil {
			continue
		}
		if event.CommandID != op.ID || event.ThreadID != worker.NativeSessionID || event.RequestID == "" || event.ErrorMessage != "" {
			continue
		}
		expected.ObservationID = op.ID
		if event.GoalMissing || event.GoalCleared {
			expected.Status = "missing"
			return expected, nil
		}
		if event.ThreadGoal != nil && event.ThreadGoal.UpdatedAt > 0 && event.ThreadGoal.TokenBudget != nil {
			expected.Status = event.ThreadGoal.Status
			expected.Objective = event.ThreadGoal.Objective
			expected.TokenBudget = *event.ThreadGoal.TokenBudget
			expected.GoalVersion = event.ThreadGoal.UpdatedAt
			return expected, nil
		}
	}
	return expected, nil
}

func (t *Tx) ValidateGoalControlSnapshot(expected *agentproto.GoalPrecondition, worker work.WorkerSession, carrier CarrierRegistration, sessionID string) error {
	if expected == nil || !carrier.Matches(worker) || expected.Incarnation != carrier.Incarnation || expected.SessionID == "" || expected.SessionID != sessionID || worker.NativeSessionID != sessionID || expected.WorkerGeneration != worker.Generation || expected.ControllerGeneration != worker.ControllerGeneration {
		return ErrGoalPreconditionStale
	}
	actual, err := t.CaptureGoalControlSnapshot(worker, carrier)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return ErrGoalPreconditionStale
	}
	return nil
}
