package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PutOperation returns the original operation for a matching creation replay.
// The bool is true only when a new operation was inserted.
func (t *Tx) PutOperation(v work.Operation, expected int64) (work.Operation, bool, error) {
	if v.RequestRef != "" {
		if err := t.requireContent(v.ScopeID, v.RequestRef); err != nil {
			return v, false, err
		}
	}
	if err := v.Validate(); err != nil {
		return v, false, err
	}
	var id string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id FROM operations WHERE actor_kind=? AND actor_id=? AND scope_id=? AND kind=? AND idempotency_key=?`, v.Actor.Kind, v.Actor.ID, v.ScopeID, v.Kind, v.IdempotencyKey).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return v, false, err
	}
	if err == nil {
		old, e := t.GetOperation(id)
		if e != nil {
			return v, false, e
		}
		if e = work.ValidateReplay(old, v); e != nil {
			return v, false, fmt.Errorf("%w: %v", ErrIdempotencyConflict, e)
		}
		if expected == 0 {
			return old, false, nil
		}
		if old.ID != v.ID {
			return v, false, ErrIdempotencyConflict
		}
		if e = work.ValidateOperationTransition(old.State, v.State); e != nil {
			return v, false, e
		}
	} else if expected != 0 {
		return v, false, ErrConflict
	}
	if err = t.record("operations", v.ID, v.Revision, expected, v); err != nil {
		return v, false, err
	}
	return v, expected == 0, nil
}
func (t *Tx) GetOperation(id string) (work.Operation, error) {
	var v work.Operation
	err := t.read("operations", id, &v)
	return v, err
}
func (t *Tx) ListOperations(scopeID string) ([]work.Operation, error) {
	return listRecords[work.Operation](t, "operations", " WHERE scope_id=?", scopeID)
}
func (t *Tx) PutOutbox(v work.Outbox, expected int64) error {
	operation, err := t.GetOperation(v.OperationID)
	if err != nil {
		return err
	}
	if operation.ScopeID != v.ScopeID {
		return fmt.Errorf("outbox operation scope mismatch")
	}
	if expected > 0 {
		old, e := t.GetOutbox(v.ID)
		if e != nil {
			return e
		}
		if e = immutable(old.ScopeID != v.ScopeID || old.OperationID != v.OperationID || old.Target != v.Target || old.EventID != v.EventID || old.PayloadRef != v.PayloadRef); e != nil {
			return e
		}
		if e = validateOutboxTransition(old.State, v.State); e != nil {
			return e
		}
	}
	return t.record("outbox", v.ID, v.Revision, expected, v)
}
func validateOutboxTransition(from, to work.OutboxState) error {
	// Outbox has a pending/sent projection of the same durable effect lifecycle.
	convert := func(state work.OutboxState) work.OperationState {
		switch state {
		case work.OutboxPending:
			return work.OperationPrepared
		case work.OutboxSent:
			return work.OperationConfirmed
		default:
			return work.OperationState(state)
		}
	}
	return work.ValidateOperationTransition(convert(from), convert(to))
}
func (t *Tx) GetOutbox(id string) (work.Outbox, error) {
	var v work.Outbox
	err := t.read("outbox", id, &v)
	return v, err
}
func (t *Tx) ListOutbox(scopeID string) ([]work.Outbox, error) {
	return listRecords[work.Outbox](t, "outbox", " WHERE scope_id=?", scopeID)
}
func (t *Tx) PutEvidence(v work.RequirementEvidence) error {
	if v.AcceptedBy.Kind == work.ActorPrincipal {
		if err := t.validateInstructionSource(v.ScopeID, v.AcceptedBy, v.Source); err != nil {
			return err
		}
	}
	if err := v.Validate(); err != nil {
		return err
	}
	task, err := t.GetTask(v.TaskID)
	if err != nil {
		return err
	}
	if task.ScopeID != v.ScopeID || task.RequirementsRevision < v.RequirementsRevision {
		return fmt.Errorf("evidence task scope/revision mismatch")
	}
	snapshot, err := t.GetTaskRequirements(v.TaskID, v.RequirementsRevision)
	if err != nil {
		return err
	}
	found := false
	for _, r := range snapshot.Requirements {
		if r.ID == v.RequirementID && r.Source.Version == v.RequirementVersion {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("evidence requirement not adopted")
	}
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, "INSERT INTO evidence(id,payload) VALUES(?,?)", v.ID, string(payload))
	return err
}
func (t *Tx) ListEvidence(taskID string) ([]work.RequirementEvidence, error) {
	evidence, err := listRecords[work.RequirementEvidence](t, "evidence", " WHERE task_id=?", taskID)
	if err != nil || len(evidence) == 0 {
		return evidence, err
	}
	task, err := t.GetTask(taskID)
	if err != nil {
		return nil, err
	}
	for i := range evidence {
		record := &evidence[i]
		record.Obsolete = record.Obsolete || record.RequirementsRevision != task.RequirementsRevision || record.AssignmentGeneration != task.AssignmentGeneration
	}
	return evidence, nil
}
