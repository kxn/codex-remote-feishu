package workstore

import (
	"database/sql"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// GetOccupiedAttemptForWorker follows the durable carrier reservation, never
// whichever historical attempt has the newest timestamp.
func (t *Tx) GetOccupiedAttemptForWorker(workerID string) (work.ExecutionAttempt, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT a.id FROM carrier_occupancy o JOIN attempts a ON a.id=o.attempt_id WHERE a.worker_id=?", workerID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.ExecutionAttempt{}, ErrNotFound
	}
	if err != nil {
		return work.ExecutionAttempt{}, err
	}
	return t.GetAttempt(id)
}
