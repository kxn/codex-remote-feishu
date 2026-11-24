package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPersonalMigrationControlHoldV1 = `CREATE TABLE IF NOT EXISTS personal_migration_control_holds(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),operation_id TEXT PRIMARY KEY REFERENCES operations(id),operation_revision INTEGER NOT NULL,operation_payload TEXT NOT NULL,evidence TEXT NOT NULL,intake_proof_id TEXT NOT NULL,evidence_hash TEXT NOT NULL,created_at TEXT NOT NULL);`

type PersonalControlIsolation struct {
	Operation work.Operation
	Request   agentproto.Command
	Attempt   work.ExecutionAttempt
	Native    *NativeCommand
	Carrier   CarrierRegistration
	Process   agentproto.RuntimeProcessIdentity
}

// PersonalControlIsolationCandidate binds an unknown system stop to its exact
// frozen command, attempt, carrier generation and process. It never claims
// that the command was sent, accepted, or had its intended effect.
func (t *Tx) PersonalControlIsolationCandidate(scopeID, operationID string) (PersonalControlIsolation, error) {
	var v PersonalControlIsolation
	op, err := t.GetOperation(operationID)
	if err != nil {
		return v, err
	}
	allowed := op.Kind == "thread.goal.clear" && op.Actor == (work.Actor{Kind: work.ActorSystem, ID: "daemon-shutdown"}) || op.Kind == "native.gate_stop" && op.Actor == (work.Actor{Kind: work.ActorSystem, ID: "admission-gate"})
	if op.ScopeID != scopeID || op.State != work.OperationUnknown || !allowed || op.RequestRef == "" {
		return v, ErrScopeDenied
	}
	body, err := t.GetContent(scopeID, op.RequestRef)
	if err != nil {
		return v, err
	}
	var request agentproto.Command
	if json.Unmarshal(body.Data, &request) != nil || request.Execution == nil || request.Execution.ValidateExecution() != nil || request.CommandID != op.ID || request.Execution.ScopeID != scopeID || request.Execution.ContextID != op.ContextID || request.Execution.WorkerSessionID != op.TargetID || request.Execution.SessionGeneration != op.TargetGeneration || request.Target.ThreadID == "" || op.RequestHash != body.SHA256 || (request.Kind != agentproto.CommandThreadGoalClear && request.Kind != agentproto.CommandTurnInterrupt) || (op.Kind == "thread.goal.clear" && request.Kind != agentproto.CommandThreadGoalClear) {
		return v, ErrConflict
	}
	attempt, err := t.GetAttempt(request.Execution.AttemptID)
	if err != nil {
		return v, err
	}
	if attempt.ScopeID != scopeID || attempt.ContextID != request.Execution.ContextID || attempt.RunRef.WorkerID != request.Execution.WorkerSessionID || attempt.RunRef.Generation != request.Execution.SessionGeneration || attempt.RunRef.RunID != request.Execution.RunRef {
		return v, ErrConflict
	}
	var incarnation string
	native, e := t.GetNativeCommand(op.ID)
	if e == nil {
		if native.Binding != *request.Execution || native.Kind != request.Kind || native.RequestRef != op.RequestRef || native.SessionID != request.Target.ThreadID {
			return v, ErrConflict
		}
		incarnation = native.Incarnation
		v.Native = &native
	} else if errors.Is(e, ErrNotFound) {
		// With no registered command, only the current exact isolated carrier
		// can establish which process the frozen control referred to.
		isolation, e := t.GetPersonalExecutionIsolation(scopeID, request.Execution.WorkerSessionID)
		if e != nil {
			return v, e
		}
		if isolation.Worker.Generation != request.Execution.SessionGeneration || isolation.Worker.ControllerGeneration != request.Execution.ControllerGeneration || isolation.Worker.NativeSessionID == "" || isolation.Worker.NativeSessionID != request.Target.ThreadID || !isolation.ProcessScopesStopped {
			return v, ErrConflict
		}
		incarnation = isolation.Worker.CarrierIncarnation
	} else {
		return v, e
	}
	carrier, err := t.GetCarrier(incarnation)
	if err != nil {
		return v, err
	}
	if !carrier.Owner.SameOwner(*request.Execution) {
		return v, ErrConflict
	}
	process, err := t.GetCarrierProcess(incarnation)
	if err != nil {
		return v, err
	}
	v.Operation, v.Request, v.Attempt, v.Carrier, v.Process = op, request, attempt, carrier, process
	return v, nil
}

// RecordPersonalControlIsolation requires a previously verified neutral drain;
// its caller has also verified the exact carrier process has exited.
func (t *Tx) RecordPersonalControlIsolation(scopeID, proofID string, v PersonalControlIsolation, now time.Time) error {
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return err
	}
	if fence.State != "draining" || now.IsZero() {
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
	current, err := t.PersonalControlIsolationCandidate(scopeID, v.Operation.ID)
	if err != nil {
		return err
	}
	evidence, _ := json.Marshal(v)
	actual, _ := json.Marshal(current)
	if string(evidence) != string(actual) {
		return ErrConflict
	}
	opRaw, _ := json.Marshal(v.Operation)
	frozen, _ := json.Marshal([]json.RawMessage{evidence, json.RawMessage(proof.Data)})
	hash := sha256.Sum256(frozen)
	digest := hex.EncodeToString(hash[:])
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_control_holds(scope_id,operation_id,operation_revision,operation_payload,evidence,intake_proof_id,evidence_hash,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(operation_id) DO NOTHING`, scopeID, v.Operation.ID, v.Operation.Revision, string(opRaw), string(evidence), proofID, digest, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	var prior string
	err = t.tx.QueryRowContext(t.ctx, `SELECT evidence_hash FROM personal_migration_control_holds WHERE scope_id=? AND operation_id=? AND operation_revision=? AND operation_payload=?`, scopeID, v.Operation.ID, v.Operation.Revision, string(opRaw)).Scan(&prior)
	if err != nil {
		return err
	}
	if prior != digest {
		return ErrConflict
	}
	return nil
}

func (t *Tx) validatePersonalControlHolds(scopeID string) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT operation_id,evidence,intake_proof_id,evidence_hash FROM personal_migration_control_holds WHERE scope_id=?`, scopeID)
	if err != nil {
		return err
	}
	type row struct{ id, evidence, proof, hash string }
	var all []row
	for rows.Next() {
		var r row
		if err = rows.Scan(&r.id, &r.evidence, &r.proof, &r.hash); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, r := range all {
		proof, e := t.GetContent(scopeID, r.proof)
		if e != nil {
			return e
		}
		frozen, e := json.Marshal([]json.RawMessage{json.RawMessage(r.evidence), json.RawMessage(proof.Data)})
		if e != nil {
			return e
		}
		hash := sha256.Sum256(frozen)
		if hex.EncodeToString(hash[:]) != r.hash {
			return ErrConflict
		}
		current, e := t.PersonalControlIsolationCandidate(scopeID, r.id)
		if e != nil {
			return e
		}
		raw, _ := json.Marshal(current)
		if string(raw) != r.evidence {
			return ErrConflict
		}
	}
	return nil
}

func (t *Tx) ListPersonalControlIsolations(scopeID string) ([]PersonalControlIsolation, error) {
	if err := t.validatePersonalControlHolds(scopeID); err != nil {
		return nil, err
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT evidence FROM personal_migration_control_holds WHERE scope_id=? ORDER BY operation_id`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PersonalControlIsolation
	for rows.Next() {
		var raw []byte
		var v PersonalControlIsolation
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if json.Unmarshal(raw, &v) != nil {
			return nil, ErrConflict
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
