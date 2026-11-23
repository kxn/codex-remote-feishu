package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

type OutcomeRequest struct {
	AttemptID               string
	RunRef                  work.RunRef
	ExpectedAttemptRevision int64
	ProcessedInputIDs       []string
	OutcomeRef              string
	Checkpoint              string
	TerminalState           work.AttemptState
	TerminalEvidence        []work.SourceRef
	TaskEvidence            []work.RequirementEvidence
	FollowupInbox           []work.Inbox
	Operations              []work.Operation
	Outbox                  []work.Outbox
	// Quiescent is explicit verified runtime evidence that no run/background
	// work retains these resources. A timeout alone must never set this flag.
	Quiescent bool
	Now       time.Time
}

func (t *Tx) CommitOutcome(request OutcomeRequest) error {
	return t.atomic(func() error { return t.commitOutcome(request) })
}
func (t *Tx) commitOutcome(request OutcomeRequest) error {
	data, err := json.Marshal(request)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := hex.EncodeToString(sum[:])
	var existing string
	err = t.tx.QueryRowContext(t.ctx, "SELECT request_hash FROM outcomes WHERE attempt_id=?", request.AttemptID).Scan(&existing)
	if err == nil {
		if existing != hash {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if request.OutcomeRef == "" || request.Now.IsZero() {
		return fmt.Errorf("outcome requires durable reference and time")
	}
	if request.TerminalState != work.AttemptCompleted && request.TerminalState != work.AttemptInterrupted && request.TerminalState != work.AttemptFailed {
		return fmt.Errorf("outcome requires terminal state")
	}
	attempt, err := t.GetAttempt(request.AttemptID)
	if err != nil {
		return err
	}
	if err = t.requireContent(attempt.ScopeID, request.OutcomeRef); err != nil {
		return err
	}
	pending, e := t.HasPendingAttemptInputAdmissions(attempt.ID)
	if e != nil {
		return e
	}
	if pending {
		return fmt.Errorf("native steer acceptance remains uncertain")
	}
	effectiveIDs, e := t.EffectiveAttemptInputIDs(attempt.ID)
	if e != nil {
		return e
	}
	consumptionAttempt := attempt
	consumptionAttempt.InputIDs = effectiveIDs
	if attempt.Revision != request.ExpectedAttemptRevision {
		return ErrConflict
	}
	if attempt.RunRef != request.RunRef {
		return fmt.Errorf("stale outcome run reference")
	}
	worker, err := t.GetWorker(attempt.RunRef.WorkerID)
	if err != nil {
		return err
	}
	if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
		return err
	}
	if worker.State != work.WorkerBusy && worker.State != work.WorkerRecovering {
		return fmt.Errorf("outcome worker state mismatch")
	}
	inputs := []work.Inbox{}
	for _, id := range request.ProcessedInputIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return e
		}
		inputs = append(inputs, input)
	}
	if err = work.ValidateInputConsumption(consumptionAttempt, inputs, request.ProcessedInputIDs); err != nil {
		return err
	}
	processed := map[string]bool{}
	for _, id := range request.ProcessedInputIDs {
		processed[id] = true
	}
	stop, err := t.privateAttemptStop(attempt)
	if err != nil {
		return err
	}
	for _, id := range effectiveIDs {
		input, e := t.GetInbox(id)
		if e != nil {
			return e
		}
		if input.ClaimedAttemptID != attempt.ID || input.State != work.InboxClaimed {
			return fmt.Errorf("attempt input no longer claimed")
		}
		expected := input.Revision
		input.Revision++
		if processed[id] {
			input.State = work.InboxProcessed
			input.ProcessedAt = &request.Now
			input.ProcessingOutcomeRef = request.OutcomeRef
		} else {
			input.State = work.InboxPending
			input.ClaimedAttemptID = ""
		}
		if e = t.PutInbox(input, expected); e != nil {
			return e
		}
		if !processed[id] && !stop.Cutoff.IsZero() {
			receivedAt := input.AvailableAt
			if receivedAt.IsZero() {
				receivedAt = attempt.CreatedAt
			}
			if input.MessageID != "" {
				message, e := t.GetMessage(input.MessageID)
				if e != nil {
					return e
				}
				receivedAt = message.CreatedAt
			}
			older, e := t.privateInputPredatesStop(input, receivedAt, stop.Cutoff)
			if e != nil {
				return e
			}
			if older {
				if e = t.cancelPrivateInbox(input); e != nil {
					return e
				}
			}
		}
	}
	staleTask := false
	if attempt.TaskID != "" {
		task, e := t.GetTask(attempt.TaskID)
		if e != nil {
			return e
		}
		staleTask = task.RequirementsRevision != attempt.FrozenRequirementsRevision || task.AssignmentGeneration != attempt.FrozenAssignmentGeneration || task.State == work.TaskCancelled
		if task.State == work.TaskCancelled {
			if e = t.cancelPendingTaskInputs(task); e != nil {
				return e
			}
		}
		if staleTask && task.State != work.TaskCancelled {
			if e = t.ReassignPendingTaskInputs(task, request.Now); e != nil {
				return e
			}
		}
		if staleTask && (len(request.FollowupInbox) > 0 || len(request.Operations) > 0 || len(request.Outbox) > 0) {
			return fmt.Errorf("stale task outcome may only archive historical evidence")
		}
	}
	for _, evidence := range request.TaskEvidence {
		if evidence.RequirementsRevision != attempt.FrozenRequirementsRevision || evidence.AssignmentGeneration != attempt.FrozenAssignmentGeneration || evidence.ScopeID != attempt.ScopeID || evidence.TaskID != attempt.TaskID {
			return fmt.Errorf("outcome evidence outside attempt task")
		}
		if err = t.PutEvidence(evidence); err != nil {
			return err
		}
	}
	if !staleTask {
		context, err := t.GetContext(attempt.ContextID)
		if err != nil {
			return err
		}
		expectedContext := context.Revision
		context.Checkpoint = request.Checkpoint
		context.Revision++
		context.UpdatedAt = request.Now
		if err = t.PutContext(context, expectedContext); err != nil {
			return err
		}

	}
	for _, input := range request.FollowupInbox {
		if input.ScopeID != attempt.ScopeID || input.State != work.InboxPending {
			return fmt.Errorf("followup input outside scope or not pending")
		}
		if err = t.PutInbox(input, 0); err != nil {
			return err
		}
	}
	for _, operation := range request.Operations {
		if operation.ScopeID != attempt.ScopeID {
			return fmt.Errorf("outcome operation outside scope")
		}
		if _, _, err = t.PutOperation(operation, 0); err != nil {
			return err
		}
	}
	for _, outbox := range request.Outbox {
		if outbox.ScopeID != attempt.ScopeID {
			return fmt.Errorf("outcome outbox outside scope")
		}
		if err = t.PutOutbox(outbox, 0); err != nil {
			return err
		}
	}
	expectedAttempt := attempt.Revision
	attempt.Revision++
	attempt.State = request.TerminalState
	attempt.TerminalEvidence = request.TerminalEvidence
	attempt.UpdatedAt = request.Now
	if err = t.PutAttempt(attempt, expectedAttempt); err != nil {
		return err
	}
	expectedWorker := worker.Revision
	worker.Revision++
	worker.UpdatedAt = request.Now
	if request.Quiescent {
		worker.State = work.WorkerReady
		if _, err = t.tx.ExecContext(t.ctx, "DELETE FROM task_occupancy WHERE attempt_id=?", attempt.ID); err != nil {
			return err
		}
		if _, err = t.tx.ExecContext(t.ctx, "DELETE FROM carrier_occupancy WHERE attempt_id=?", attempt.ID); err != nil {
			return err
		}
		if _, err = t.tx.ExecContext(t.ctx, "DELETE FROM resource_occupancy WHERE attempt_id=?", attempt.ID); err != nil {
			return err
		}
	} else {
		worker.State = work.WorkerRecovering
	}
	if err = t.PutWorker(worker, expectedWorker); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO outcomes(attempt_id,request_hash,payload) VALUES(?,?,?)", attempt.ID, hash, string(data))
	return err
}
