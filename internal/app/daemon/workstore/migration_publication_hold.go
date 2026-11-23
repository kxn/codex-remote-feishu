package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPersonalMigrationPublicationHoldV1 = `CREATE TABLE IF NOT EXISTS personal_migration_publication_holds(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),outbox_id TEXT PRIMARY KEY REFERENCES outbox(id),operation_id TEXT NOT NULL REFERENCES operations(id),outbox_revision INTEGER NOT NULL,operation_revision INTEGER NOT NULL,outbox_payload TEXT NOT NULL,operation_payload TEXT NOT NULL,intake_proof_id TEXT NOT NULL,evidence_hash TEXT NOT NULL,created_at TEXT NOT NULL);`

// HoldPersonalUncertainPublication preserves an unresolved old platform send as
// a component, not a receipt and not authorization to replay it. The caller
// must have stopped neutral admission and drained the publisher activity guard.
func (t *Tx) HoldPersonalUncertainPublication(scopeID, outboxID, proofID string, now time.Time) error {
	if now.IsZero() || proofID == "" {
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
	out, err := t.GetOutbox(outboxID)
	if err != nil {
		return err
	}
	if out.ScopeID != scopeID || (out.State != work.OutboxSending && out.State != work.OutboxUnknown) || out.ExternalMessageID != "" {
		return ErrConflict
	}
	op, err := t.GetOperation(out.OperationID)
	if err != nil {
		return err
	}
	if op.ScopeID != scopeID || op.Kind != "feishu.publish" || (op.State != work.OperationSending && op.State != work.OperationUnknown) || op.ExternalReceipt != "" {
		return ErrConflict
	}
	first := false
	for _, step := range op.Steps {
		if step.ID == "first_send" && (step.State == work.OperationSending || step.State == work.OperationUnknown) {
			first = true
		}
	}
	if !first {
		return ErrConflict
	}
	opRaw, _ := json.Marshal(op)
	outRaw, _ := json.Marshal(out)
	body, _ := json.Marshal([]json.RawMessage{outRaw, opRaw, json.RawMessage(proof.Data)})
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_publication_holds(scope_id,outbox_id,operation_id,outbox_revision,operation_revision,outbox_payload,operation_payload,intake_proof_id,evidence_hash,created_at) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(outbox_id) DO NOTHING`, scopeID, out.ID, op.ID, out.Revision, op.Revision, string(outRaw), string(opRaw), proofID, digest, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	var old string
	if err = t.tx.QueryRowContext(t.ctx, `SELECT evidence_hash FROM personal_migration_publication_holds WHERE scope_id=? AND outbox_id=? AND outbox_revision=? AND operation_revision=?`, scopeID, out.ID, out.Revision, op.Revision).Scan(&old); err != nil {
		return err
	}
	if old != digest {
		return ErrConflict
	}
	return nil
}
func (t *Tx) validatePersonalPublicationHolds(scopeID string) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT outbox_payload,operation_payload,intake_proof_id,evidence_hash FROM personal_migration_publication_holds WHERE scope_id=?`, scopeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct{ out, op, proof, hash string }
	var all []item
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.out, &v.op, &v.proof, &v.hash); err != nil {
			return err
		}
		all = append(all, v)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, v := range all {
		proof, e := t.GetContent(scopeID, v.proof)
		if e != nil {
			return e
		}
		raw, e := json.Marshal([]json.RawMessage{json.RawMessage(v.out), json.RawMessage(v.op), json.RawMessage(proof.Data)})
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
