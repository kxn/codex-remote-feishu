package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPersonalMigrationReadArchiveV1 = `CREATE TABLE IF NOT EXISTS personal_migration_read_archives(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),operation_id TEXT PRIMARY KEY REFERENCES operations(id),revision INTEGER NOT NULL,operation_payload TEXT NOT NULL,command_payload TEXT NOT NULL,process_payload TEXT NOT NULL,evidence_hash TEXT NOT NULL,observed_at TEXT NOT NULL);`

// PersonalReadArchiveCandidate is a frozen local candidate. A registered
// command requires the host to prove the exact original process has exited.
type PersonalReadArchiveCandidate struct {
	Operation work.Operation
	Command   *NativeCommand
	Process   *agentproto.RuntimeProcessIdentity
}

func (t *Tx) PersonalReadArchiveCandidates(scopeID string) ([]PersonalReadArchiveCandidate, error) {
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return nil, err
	}
	if fence.State != "draining" {
		return nil, ErrConflict
	}
	ops, err := t.ListOperations(scopeID)
	if err != nil {
		return nil, err
	}
	var result []PersonalReadArchiveCandidate
	for _, op := range ops {
		if op.State != work.OperationUnknown || (op.Kind != string(agentproto.CommandRuntimeInspect) && op.Kind != string(agentproto.CommandModelList)) {
			continue
		}
		v := PersonalReadArchiveCandidate{Operation: op}
		command, e := t.GetNativeCommand(op.ID)
		if errors.Is(e, ErrNotFound) {
			result = append(result, v)
			continue
		}
		if e != nil {
			return nil, e
		}
		if command.Binding.ScopeID != scopeID || command.Kind != agentproto.CommandKind(op.Kind) {
			return nil, ErrConflict
		}
		carrier, e := t.GetCarrier(command.Incarnation)
		if e != nil {
			return nil, e
		}
		if carrier.Owner.ScopeID != scopeID || carrier.Owner.WorkerSessionID != command.Binding.WorkerSessionID || carrier.Owner.SessionGeneration != command.Binding.SessionGeneration || carrier.Owner.ControllerGeneration != command.Binding.ControllerGeneration {
			return nil, ErrConflict
		}
		process, e := t.GetCarrierProcess(command.Incarnation)
		if e != nil {
			if errors.Is(e, ErrNotFound) {
				continue
			}
			return nil, e
		}
		v.Command = &command
		v.Process = &process
		result = append(result, v)
	}
	return result, nil
}

// RecordPersonalReadArchive is called only after the host has verified the
// candidate's exact process identity exited (or no native command existed).
// It never claims the read succeeded and never changes the original operation.
func (t *Tx) RecordPersonalReadArchive(scopeID string, v PersonalReadArchiveCandidate, now time.Time) error {
	if now.IsZero() || v.Operation.ScopeID != scopeID || v.Operation.State != work.OperationUnknown || (v.Operation.Kind != string(agentproto.CommandRuntimeInspect) && v.Operation.Kind != string(agentproto.CommandModelList)) {
		return ErrConflict
	}
	fence, err := t.GetPersonalMigrationFence(scopeID)
	if err != nil {
		return err
	}
	if fence.State != "draining" {
		return ErrConflict
	}
	current, err := t.GetOperation(v.Operation.ID)
	if err != nil {
		return err
	}
	a, _ := json.Marshal(v.Operation)
	b, _ := json.Marshal(current)
	if string(a) != string(b) {
		return ErrConflict
	}
	native, err := t.GetNativeCommand(current.ID)
	if errors.Is(err, ErrNotFound) {
		if v.Command != nil || v.Process != nil {
			return ErrConflict
		}
	} else if err != nil {
		return err
	} else {
		if v.Command == nil || v.Process == nil || native.Binding.ScopeID != scopeID || native.Kind != agentproto.CommandKind(current.Kind) {
			return ErrConflict
		}
		x, _ := json.Marshal(native)
		y, _ := json.Marshal(v.Command)
		if string(x) != string(y) {
			return ErrConflict
		}
		process, e := t.GetCarrierProcess(native.Incarnation)
		if e != nil {
			return e
		}
		if process != *v.Process {
			return ErrConflict
		}
	}
	commandRaw, _ := json.Marshal(v.Command)
	processRaw, _ := json.Marshal(v.Process)
	frozen, _ := json.Marshal([]json.RawMessage{a, commandRaw, processRaw})
	sum := sha256.Sum256(frozen)
	digest := hex.EncodeToString(sum[:])
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_read_archives(scope_id,operation_id,revision,operation_payload,command_payload,process_payload,evidence_hash,observed_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(operation_id) DO NOTHING`, scopeID, current.ID, current.Revision, string(a), string(commandRaw), string(processRaw), digest, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	var oldHash string
	if err = t.tx.QueryRowContext(t.ctx, `SELECT evidence_hash FROM personal_migration_read_archives WHERE scope_id=? AND operation_id=? AND revision=?`, scopeID, current.ID, current.Revision).Scan(&oldHash); err != nil {
		return err
	}
	if oldHash != digest {
		return fmt.Errorf("read archive conflicts: %w", ErrConflict)
	}
	return nil
}

func (t *Tx) validatePersonalReadArchives(scopeID string) error {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT operation_payload,command_payload,process_payload,evidence_hash FROM personal_migration_read_archives WHERE scope_id=?`, scopeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a, b, c, expected string
		if err = rows.Scan(&a, &b, &c, &expected); err != nil {
			return err
		}
		frozen, e := json.Marshal([]json.RawMessage{json.RawMessage(a), json.RawMessage(b), json.RawMessage(c)})
		if e != nil {
			return e
		}
		sum := sha256.Sum256(frozen)
		if hex.EncodeToString(sum[:]) != expected {
			return ErrConflict
		}
	}
	return rows.Err()
}
