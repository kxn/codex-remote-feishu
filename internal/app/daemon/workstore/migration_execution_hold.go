package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPersonalMigrationExecutionHoldV1 = `CREATE TABLE IF NOT EXISTS personal_migration_execution_holds(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),worker_id TEXT PRIMARY KEY REFERENCES workers(id),worker_revision INTEGER NOT NULL,worker_payload TEXT NOT NULL,process_payload TEXT NOT NULL,intake_proof_id TEXT NOT NULL,evidence TEXT NOT NULL,evidence_hash TEXT NOT NULL,created_at TEXT NOT NULL);`

type PersonalExecutionIsolation struct {
	Worker                                                                                                                   work.WorkerSession                `json:"worker"`
	Process                                                                                                                  agentproto.RuntimeProcessIdentity `json:"process"`
	LaunchHash, JournalLaunchHash, SourceReceiptHash, InventoryHash                                                          string
	NativeCommandsIdle, InputsIdle, ToolsIdle, GoalsIdle, TasksIdle, TurnsTerminal, UncertaintiesEmpty, ProcessScopesStopped bool
}

func (t *Tx) GetPersonalExecutionIsolation(scopeID, workerID string) (PersonalExecutionIsolation, error) {
	var result PersonalExecutionIsolation
	if err := t.validatePersonalExecutionHolds(scopeID); err != nil {
		return result, err
	}
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT evidence FROM personal_migration_execution_holds WHERE scope_id=? AND worker_id=?`, scopeID, workerID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	if json.Unmarshal([]byte(raw), &result) != nil {
		return PersonalExecutionIsolation{}, ErrConflict
	}
	current, err := t.GetWorker(workerID)
	if err != nil {
		return result, err
	}
	a, _ := json.Marshal(current)
	b, _ := json.Marshal(result.Worker)
	if string(a) != string(b) || current.ScopeID != scopeID {
		return PersonalExecutionIsolation{}, ErrConflict
	}
	process, err := t.GetCarrierProcess(current.CarrierIncarnation)
	if err != nil {
		return result, err
	}
	if process != result.Process {
		return PersonalExecutionIsolation{}, ErrConflict
	}
	return result, nil
}

// HoldPersonalExecutionIsolation preserves unresolved native state as a
// component only after the host has proven the exact old process scopes gone.
// It never marks an attempt successful or a remote task finished.
func (t *Tx) HoldPersonalExecutionIsolation(scopeID, proofID string, v PersonalExecutionIsolation, now time.Time) error {
	if now.IsZero() || proofID == "" || !v.ProcessScopesStopped || v.InventoryHash == "" || v.LaunchHash == "" || v.JournalLaunchHash == "" || v.Worker.ScopeID != scopeID {
		return ErrConflict
	}
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return err
	}
	if fence.State != "draining" {
		return ErrConflict
	}
	proof, err := t.GetContent(scopeID, proofID)
	if err != nil {
		return err
	}
	var intake struct {
		MigrationID string `json:"migration_id"`
		Digest      string `json:"digest"`
	}
	if json.Unmarshal(proof.Data, &intake) != nil || intake.MigrationID != fence.MigrationID || intake.Digest == "" || proofID != "migration-intake-proof:"+intake.Digest {
		return ErrConflict
	}
	current, err := t.GetWorker(v.Worker.ID)
	if err != nil {
		return err
	}
	workerRaw, _ := json.Marshal(v.Worker)
	actualRaw, _ := json.Marshal(current)
	if string(workerRaw) != string(actualRaw) {
		return ErrConflict
	}
	process, err := t.GetCarrierProcess(current.CarrierIncarnation)
	if err != nil {
		return err
	}
	if process != v.Process {
		return ErrConflict
	}
	carrier, err := t.GetCarrier(current.CarrierIncarnation)
	if err != nil {
		return err
	}
	if carrier.Owner.ScopeID != scopeID || carrier.Owner.WorkerSessionID != current.ID || carrier.Owner.SessionGeneration != current.Generation {
		return ErrConflict
	}
	if v.LaunchHash != v.JournalLaunchHash && v.SourceReceiptHash == "" {
		return ErrConflict
	}
	processRaw, _ := json.Marshal(process)
	evidenceRaw, _ := json.Marshal(v)
	frozen, _ := json.Marshal([]json.RawMessage{workerRaw, processRaw, evidenceRaw, json.RawMessage(proof.Data)})
	sum := sha256.Sum256(frozen)
	digest := hex.EncodeToString(sum[:])
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_execution_holds(scope_id,worker_id,worker_revision,worker_payload,process_payload,intake_proof_id,evidence,evidence_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(worker_id) DO NOTHING`, scopeID, current.ID, current.Revision, string(workerRaw), string(processRaw), proofID, string(evidenceRaw), digest, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	var old string
	if err = t.tx.QueryRowContext(t.ctx, `SELECT evidence_hash FROM personal_migration_execution_holds WHERE scope_id=? AND worker_id=? AND worker_revision=?`, scopeID, current.ID, current.Revision).Scan(&old); err != nil {
		return err
	}
	if old != digest {
		return ErrConflict
	}
	return nil
}
func (t *Tx) validatePersonalExecutionHolds(scopeID string) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT worker_payload,process_payload,evidence,intake_proof_id,evidence_hash FROM personal_migration_execution_holds WHERE scope_id=?`, scopeID)
	if err != nil {
		return err
	}
	type item struct{ worker, process, evidence, proof, hash string }
	var all []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.worker, &v.process, &v.evidence, &v.proof, &v.hash); err != nil {
			rows.Close()
			return err
		}
		all = append(all, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, v := range all {
		proof, e := t.GetContent(scopeID, v.proof)
		if e != nil {
			return e
		}
		raw, e := json.Marshal([]json.RawMessage{json.RawMessage(v.worker), json.RawMessage(v.process), json.RawMessage(v.evidence), json.RawMessage(proof.Data)})
		if e != nil {
			return e
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != v.hash {
			return ErrConflict
		}
	}
	return nil
}
