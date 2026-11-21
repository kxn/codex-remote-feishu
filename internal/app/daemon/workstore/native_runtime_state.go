package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type RuntimeGate struct {
	Revision          int64     `json:"revision"`
	RecoveryReason    string    `json:"recovery_reason,omitempty"`
	ResponsibleRoleID string    `json:"responsible_role_id,omitempty"`
	WorkerID          string    `json:"worker_id"`
	ScopeID           string    `json:"scope_id"`
	AttemptID         string    `json:"attempt_id,omitempty"`
	Reason            string    `json:"reason"`
	CreatedAt         time.Time `json:"created_at"`
}

func (t *Tx) PutRuntimeGate(v RuntimeGate) error {
	worker, err := t.GetWorker(v.WorkerID)
	if err != nil {
		return err
	}
	if worker.ScopeID != v.ScopeID || v.Reason == "" || v.CreatedAt.IsZero() {
		return fmt.Errorf("invalid worker runtime gate")
	}
	var previous *RuntimeGate
	if old, e := t.GetRuntimeGate(v.WorkerID); e == nil {
		previous = &old
		if old.RecoveryReason != "" {
			v.RecoveryReason = old.RecoveryReason
		} else if (old.Reason == "missing_processing_report" || old.Reason == "native_resume_rejected") && v.Reason != old.Reason {
			v.RecoveryReason = old.Reason
		}
		if old.Reason == v.Reason {
			v.CreatedAt = old.CreatedAt
		}
	} else if !errors.Is(e, ErrNotFound) {
		return e
	}
	if v.ResponsibleRoleID == "" {
		v.ResponsibleRoleID = worker.RoleID
	}
	if previous != nil && previous.Reason == v.Reason && previous.RecoveryReason == v.RecoveryReason && previous.AttemptID == v.AttemptID && previous.ResponsibleRoleID == v.ResponsibleRoleID {
		return nil
	}
	if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO runtime_gate_sequences(worker_id,revision) VALUES(?,1) ON CONFLICT(worker_id) DO UPDATE SET revision=revision+1`, v.WorkerID); err != nil {
		return err
	}
	if err = t.tx.QueryRowContext(t.ctx, `SELECT revision FROM runtime_gate_sequences WHERE worker_id=?`, v.WorkerID).Scan(&v.Revision); err != nil {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO runtime_gates(worker_id,reason,payload) VALUES(?,?,?) ON CONFLICT(worker_id) DO UPDATE SET reason=excluded.reason,payload=excluded.payload`, v.WorkerID, v.Reason, mustJSON(v))
	return err
}

func (t *Tx) GetRuntimeGate(workerID string) (RuntimeGate, error) {
	var v RuntimeGate
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM runtime_gates WHERE worker_id=?`, workerID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}
func (t *Tx) ClearRuntimeGate(workerID, reason string) error {
	_, err := t.tx.ExecContext(t.ctx, `DELETE FROM runtime_gates WHERE worker_id=? AND reason=?`, workerID, reason)
	return err
}

func (t *Tx) ListConnectedIdleRecoveryWorkers(limit int) ([]work.WorkerSession, error) {
	return listRecords[work.WorkerSession](t, "workers",
		" WHERE id IN (SELECT id FROM workers WHERE state='recovering' AND EXISTS(SELECT 1 FROM runtime_gates g JOIN carrier_registrations c ON c.worker_id=g.worker_id WHERE g.worker_id=workers.id AND g.reason='carrier_disconnected_reconcile' AND coalesce(json_extract(g.payload,'$.attempt_id'),'')='' AND coalesce(json_extract(g.payload,'$.recovery_reason'),'')='' AND c.incarnation=json_extract(workers.payload,'$.carrier_incarnation') AND c.connected=1) ORDER BY json_extract(payload,'$.updated_at'),id LIMIT ?)", limit)
}

func (t *Tx) CarrierConnected(incarnation string) (bool, error) {
	var connected bool
	err := t.tx.QueryRowContext(t.ctx, "SELECT connected FROM carrier_registrations WHERE incarnation=?", incarnation).Scan(&connected)
	return connected, err
}

func (t *Tx) QuarantineRuntimeEvent(event agentproto.Event, reason string) error {
	if event.Journal == nil {
		return fmt.Errorf("quarantine requires durable event identity")
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT OR IGNORE INTO runtime_quarantine(incarnation,sequence,session_id,turn_id,reason) VALUES(?,?,?,?,?)`, event.Journal.CarrierIncarnation, event.Journal.Sequence, event.ThreadID, event.TurnID, reason)
	return err
}

func (t *Tx) QuarantinedNativeEvents(incarnation, sessionID, turnID string) ([]agentproto.Event, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT r.payload FROM runtime_quarantine q JOIN runtime_receipts r ON r.carrier_incarnation=q.incarnation AND r.sequence=q.sequence WHERE q.incarnation=? AND q.session_id=? AND q.turn_id=? AND q.resolved=0 ORDER BY q.sequence LIMIT 128`, incarnation, sessionID, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []agentproto.Event
	for rows.Next() {
		var raw string
		var event agentproto.Event
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
func (t *Tx) ResolveQuarantine(incarnation string, sequence uint64) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE runtime_quarantine SET resolved=1 WHERE incarnation=? AND sequence=?`, incarnation, sequence)
	return err
}

type ContinuationGrant struct {
	AuthorizationKind string                      `json:"authorization_kind,omitempty"`
	SourceTurnID      string                      `json:"source_turn_id,omitempty"`
	BudgetSource      string                      `json:"budget_source,omitempty"`
	Source            work.SourceRef              `json:"source,omitempty"`
	Reconciled        bool                        `json:"reconciled"`
	TokenBudget       int64                       `json:"token_budget"`
	Objective         string                      `json:"objective"`
	Incarnation       string                      `json:"incarnation"`
	SessionID         string                      `json:"session_id"`
	Binding           agentproto.ExecutionBinding `json:"binding"`
	CommandID         string                      `json:"command_id"`
	GoalVersion       int64                       `json:"goal_version"`
	Active            bool                        `json:"active"`
	ConfirmedSequence uint64                      `json:"confirmed_sequence"`
}

func (t *Tx) InvalidateContinuationGrants(incarnation string) error {
	_, err := t.tx.ExecContext(t.ctx, `UPDATE runtime_continuations SET payload=json_set(payload,'$.reconciled',json('false')) WHERE incarnation=?`, incarnation)
	return err
}
func (t *Tx) HasCarrierOccupancy(carrier CarrierRegistration, attemptID string) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM carrier_occupancy WHERE instance_id=? AND instance_generation=? AND process_generation=? AND attempt_id=?`, carrier.OriginInstanceID, carrier.InstanceGeneration, carrier.ProcessGeneration, attemptID).Scan(&n)
	return n == 1, err
}
func (t *Tx) HasNativeSessionOwner(incarnation, sessionID, attemptID string) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM native_turns WHERE carrier_incarnation=? AND session_id=? AND attempt_id=?`, incarnation, sessionID, attemptID).Scan(&n)
	return n > 0, err
}
func (t *Tx) CountUnresolvedNativeEvents(incarnation string) (int, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM runtime_quarantine WHERE incarnation=? AND resolved=0`, incarnation).Scan(&n)
	return n, err
}
func (t *Tx) GetQuiescenceReceipt(attemptID string) (work.QuiescenceEvidence, error) {
	var value work.QuiescenceEvidence
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM quiescence_receipts WHERE attempt_id=?`, attemptID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}

func (t *Tx) PutContinuationGrant(v ContinuationGrant) error {
	carrier, err := t.GetCarrier(v.Incarnation)
	if err != nil {
		return err
	}
	if !carrier.Owner.SameOwner(v.Binding) || v.SessionID == "" || v.CommandID == "" || v.ConfirmedSequence == 0 {
		return fmt.Errorf("continuation requires exact carrier and command proof")
	}
	if _, err := t.ValidateNativeBinding(v.Binding); err != nil {
		return err
	}
	old, err := t.GetContinuationGrant(v.Incarnation, v.SessionID)
	if err == nil && old.Active && old.Binding != v.Binding {
		return fmt.Errorf("active continuation belongs to another attempt")
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO runtime_continuations(incarnation,session_id,payload) VALUES(?,?,?) ON CONFLICT(incarnation,session_id) DO UPDATE SET payload=excluded.payload`, v.Incarnation, v.SessionID, mustJSON(v))
	return err
}
func (t *Tx) GetContinuationGrant(incarnation, sessionID string) (ContinuationGrant, error) {
	var v ContinuationGrant
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM runtime_continuations WHERE incarnation=? AND session_id=?`, incarnation, sessionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	err = json.Unmarshal([]byte(raw), &v)
	return v, err
}

func (t *Tx) CountOpenNativeTurns(attemptID string) (int, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM native_turns WHERE attempt_id=? AND coalesce(json_extract(payload,'$.terminal_status'),'')=''`, attemptID).Scan(&count)
	return count, err
}

func (t *Tx) AttemptByCommandID(commandID string) (work.ExecutionAttempt, error) {
	var value work.ExecutionAttempt
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM attempts WHERE json_extract(payload,'$.command_id')=?`, commandID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return value, ErrNotFound
	}
	if err != nil {
		return value, err
	}
	err = json.Unmarshal([]byte(raw), &value)
	return value, err
}

func (t *Tx) HasAnyCarrierOccupancy(carrier CarrierRegistration) (bool, error) {
	var n int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM carrier_occupancy WHERE instance_id=? AND instance_generation=? AND process_generation=?`, carrier.OriginInstanceID, carrier.InstanceGeneration, carrier.ProcessGeneration).Scan(&n)
	return n > 0, err
}

func (t *Tx) QuarantinedSessionEvents(incarnation, sessionID string) ([]agentproto.Event, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT r.payload FROM runtime_quarantine q JOIN runtime_receipts r ON r.carrier_incarnation=q.incarnation AND r.sequence=q.sequence WHERE q.incarnation=? AND q.session_id=? AND q.resolved=0 ORDER BY q.sequence LIMIT 128`, incarnation, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []agentproto.Event
	for rows.Next() {
		var raw string
		var value agentproto.Event
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (t *Tx) AcknowledgeRuntimeRecovery(workerID, reason string, revision int64) error {
	gate, err := t.GetRuntimeGate(workerID)
	if err != nil {
		return err
	}
	if gate.Reason != reason || gate.Revision != revision || revision <= 0 {
		return ErrConflict
	}
	result, err := t.tx.ExecContext(t.ctx, `DELETE FROM runtime_gates WHERE worker_id=? AND reason=? AND json_extract(payload,'$.revision')=?`, workerID, reason, revision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}

func (t *Tx) HasActiveContinuation(incarnation string) (bool, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM runtime_continuations WHERE incarnation=? AND json_extract(payload,'$.active')=1`, incarnation).Scan(&count)
	return count > 0, err
}
