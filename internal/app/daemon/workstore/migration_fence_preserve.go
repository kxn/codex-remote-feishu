package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPersonalMigrationPreservedV1 = `CREATE TABLE IF NOT EXISTS personal_migration_preserved(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),kind TEXT NOT NULL CHECK(kind IN ('operation','outbox','preview')),record_id TEXT NOT NULL,record_revision INTEGER NOT NULL,payload_hash TEXT NOT NULL,payload TEXT NOT NULL CHECK(json_valid(payload)),created_at TEXT NOT NULL,PRIMARY KEY(scope_id,kind,record_id));`

// PreservePersonalMigrationUnsent records source evidence without modifying or
// sending it. Unknown, in-flight and retried writes are never inferred unsent.
func (t *Tx) PreservePersonalMigrationUnsent(scopeID, kind, id string, revision int64, now time.Time) error {
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return err
	}
	if fence.State != "draining" || id == "" || revision < 1 || now.IsZero() {
		return fmt.Errorf("draining fence, exact revision and timestamp required")
	}
	var value any
	switch kind {
	case "operation":
		op, e := t.GetOperation(id)
		if e != nil {
			return e
		}
		if op.ScopeID != scopeID || op.Revision != revision || !personalOperationDefinitelyUnsent(op) {
			return ErrConflict
		}
		value = op
		var started int
		if e = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM outbox WHERE operation_id=? AND state<>'pending'`, id).Scan(&started); e != nil {
			return e
		}
		if started != 0 {
			return ErrConflict
		}
		if e = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM native_commands WHERE command_id=?`, id).Scan(&started); e != nil {
			return e
		}
		if started != 0 {
			return ErrConflict
		}
	case "outbox":
		out, e := t.GetOutbox(id)
		if e != nil {
			return e
		}
		if out.ScopeID != scopeID || out.Revision != revision || out.State != work.OutboxPending {
			return ErrConflict
		}
		op, e := t.GetOperation(out.OperationID)
		if e != nil {
			return e
		}
		if op.ScopeID != scopeID || !personalOperationDefinitelyUnsent(op) {
			return ErrConflict
		}
		value = out
		var started int
		if e = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM outbox WHERE operation_id=? AND state<>'pending'`, op.ID).Scan(&started); e != nil {
			return e
		}
		if started != 0 {
			return ErrConflict
		}
		if e = t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM native_commands WHERE command_id=?`, op.ID).Scan(&started); e != nil {
			return e
		}
		if started != 0 {
			return ErrConflict
		}
	case "preview":
		display, e := t.GetPrivateExecutionDisplay(id)
		if e != nil {
			return e
		}
		if display.Binding.ScopeID != scopeID || display.Revision != revision || display.PreviewState != "queued" || display.PreviewTerminal == nil || display.PreviewSource != display.Pending {
			return ErrConflict
		}
		value = display
	default:
		return fmt.Errorf("unsupported preservation kind")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(raw)
	sum := hex.EncodeToString(hash[:])
	res, err := t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_preserved(scope_id,kind,record_id,record_revision,payload_hash,payload,created_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(scope_id,kind,record_id) DO NOTHING`, scopeID, kind, id, revision, sum, string(raw), now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var oldRevision int64
	var oldHash string
	if err = t.tx.QueryRowContext(t.ctx, `SELECT record_revision,payload_hash FROM personal_migration_preserved WHERE scope_id=? AND kind=? AND record_id=?`, scopeID, kind, id).Scan(&oldRevision, &oldHash); err != nil {
		return err
	}
	if oldRevision != revision || oldHash != sum {
		return ErrConflict
	}
	return nil
}
func personalOperationDefinitelyUnsent(op work.Operation) bool {
	return op.State == work.OperationPrepared && op.ExternalReceipt == "" && len(op.Steps) == 0
}
