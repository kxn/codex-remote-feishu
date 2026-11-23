package workstore

import (
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A wrapper that never received execution cannot need a native turn receipt.
// This proof is intentionally stricter than ordinary idle-session recovery.
func (t *Tx) AssertUnusedCarrier(worker work.WorkerSession) error {
	if worker.NativeSessionID != "" {
		return fmt.Errorf("%w: carrier already owns native history", ErrConflict)
	}
	var used bool
	err := t.tx.QueryRowContext(t.ctx, `SELECT EXISTS(SELECT 1 FROM attempts WHERE json_extract(payload,'$.run_ref.worker_id')=? AND json_extract(payload,'$.run_ref.generation')=?) OR EXISTS(SELECT 1 FROM native_commands WHERE incarnation=? AND coalesce(json_extract(payload,'$.kind'),'') NOT IN (?,?))`, worker.ID, worker.Generation, worker.CarrierIncarnation, agentproto.CommandModelList, agentproto.CommandRuntimeInspect).Scan(&used)
	if err != nil {
		return err
	}
	if used {
		return fmt.Errorf("%w: carrier has execution or effectful command history", ErrConflict)
	}
	return nil
}
