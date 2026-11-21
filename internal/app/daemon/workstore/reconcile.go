package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"reflect"
)

type ReconcileQuiescenceRequest struct {
	Evidence               work.QuiescenceEvidence
	ExpectedWorkerRevision int64
}

func (t *Tx) ReconcileQuiescence(input ReconcileQuiescenceRequest) error {
	return t.atomic(func() error {
		proof := input.Evidence
		if err := proof.Validate(); err != nil {
			return err
		}
		attempt, err := t.GetAttemptByRunRef(proof.RunRef.RunID)
		if err != nil {
			return err
		}
		if attempt.RunRef != proof.RunRef {
			return fmt.Errorf("quiescence run mismatch")
		}
		worker, err := t.GetWorker(proof.RunRef.WorkerID)
		if err != nil {
			return err
		}
		if err = work.ValidateAttemptBinding(attempt, worker); err != nil {
			return err
		}
		if worker.InstanceID != proof.InstanceID || worker.InstanceGeneration != proof.InstanceGeneration || worker.ProcessGeneration != proof.ProcessGeneration || worker.CarrierIncarnation != proof.CarrierIncarnation {
			return fmt.Errorf("quiescence carrier generation mismatch")
		}
		if err = t.requireContent(attempt.ScopeID, proof.Source.EvidenceRef); err != nil {
			return fmt.Errorf("quiescence proof not durable: %w", err)
		}
		var payload string
		err = t.tx.QueryRowContext(t.ctx, "SELECT payload FROM quiescence_receipts WHERE attempt_id=?", attempt.ID).Scan(&payload)
		if err == nil {
			var old work.QuiescenceEvidence
			if err = json.Unmarshal([]byte(payload), &old); err != nil {
				return err
			}
			if !reflect.DeepEqual(old, proof) {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if worker.Revision != input.ExpectedWorkerRevision {
			return ErrConflict
		}
		if worker.State != work.WorkerRecovering && worker.State != work.WorkerBusy {
			return fmt.Errorf("worker is not awaiting runtime reconciliation")
		}
		if attempt.State != work.AttemptCompleted && attempt.State != work.AttemptInterrupted && attempt.State != work.AttemptFailed {
			return fmt.Errorf("attempt terminal outcome must be recorded before resource release")
		}
		for _, table := range []string{"resource_occupancy", "carrier_occupancy", "task_occupancy"} {
			if _, err = t.tx.ExecContext(t.ctx, "DELETE FROM "+table+" WHERE attempt_id=?", attempt.ID); err != nil {
				return err
			}
		}
		expected := worker.Revision
		worker.Revision++
		worker.State = work.WorkerReady
		worker.UpdatedAt = proof.ObservedAt
		if err = t.PutWorker(worker, expected); err != nil {
			return err
		}
		_, err = t.tx.ExecContext(t.ctx, "INSERT INTO quiescence_receipts(attempt_id,payload) VALUES(?,?)", attempt.ID, mustJSON(proof))
		return err
	})
}
