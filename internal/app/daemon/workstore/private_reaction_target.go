package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

func (t *Tx) freezePrivateReactionTarget(action *PrivateReactionAction) error {
	worker, err := t.CurrentContextWorker(action.ContextID)
	if err != nil {
		return err
	}
	if worker.State != work.WorkerBusy || worker.NativeSessionID == "" {
		return ErrConflict
	}
	attempt, err := t.GetOccupiedAttemptForWorker(worker.ID)
	if err != nil {
		return err
	}
	if attempt.State != work.AttemptRunning || attempt.ScopeID != action.ScopeID || attempt.ContextID != action.ContextID {
		return ErrConflict
	}
	turns, err := t.ListNativeTurns(attempt.ID)
	if err != nil {
		return err
	}
	found := false
	for _, turn := range turns {
		if turn.CarrierIncarnation != worker.CarrierIncarnation || turn.SessionID != worker.NativeSessionID || !turn.Started || turn.TerminalStatus != "" {
			continue
		}
		if found {
			return ErrConflict
		}
		action.Execution = turn.Binding
		action.Incarnation = turn.CarrierIncarnation
		action.SessionID = turn.SessionID
		action.TurnID = turn.TurnID
		found = true
	}
	if !found {
		return ErrNotFound
	}
	return t.validatePrivateReactionTarget(*action)
}
func (t *Tx) validatePrivateReactionTarget(action PrivateReactionAction) error {
	if err := action.Execution.ValidateExecution(); err != nil {
		return err
	}
	worker, err := t.GetWorker(action.Execution.WorkerSessionID)
	if err != nil {
		return err
	}
	if worker.State != work.WorkerBusy || worker.Generation != action.Execution.SessionGeneration || worker.ControllerGeneration != action.Execution.ControllerGeneration || worker.CarrierIncarnation != action.Incarnation || worker.NativeSessionID != action.SessionID || worker.ContextID != action.ContextID || worker.ScopeID != action.ScopeID {
		return ErrConflict
	}
	attempt, err := t.GetOccupiedAttemptForWorker(worker.ID)
	if err != nil {
		return err
	}
	if attempt.ID != action.Execution.AttemptID || attempt.RunRef.RunID != action.Execution.RunRef || attempt.State != work.AttemptRunning {
		return ErrConflict
	}
	if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
		return err
	}
	connected, err := t.CarrierConnected(action.Incarnation)
	if err != nil {
		return err
	}
	if !connected {
		return ErrConflict
	}
	turns, err := t.ListNativeTurns(attempt.ID)
	if err != nil {
		return err
	}
	for _, other := range turns {
		if other.CarrierIncarnation == action.Incarnation && other.SessionID == action.SessionID && other.Started && other.TerminalStatus == "" && other.TurnID != action.TurnID {
			return ErrConflict
		}
	}
	turn, err := t.GetNativeTurn(action.Incarnation, action.SessionID, action.TurnID)
	if err != nil {
		return err
	}
	if turn.Binding != action.Execution || !turn.Started || turn.TerminalStatus != "" {
		return ErrConflict
	}
	return nil
}
