package workstore

import (
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// Queued compact is a safe-boundary request. It does not interrupt the current
// attempt, but that same generation cannot claim a subsequent one first.
func (t *Tx) AssertHostCompactionAvailable(workerID string, generation int64) error {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*) FROM operations WHERE kind='worker.compact.rebuild' AND json_extract(payload,'$.target_id')=? AND json_extract(payload,'$.target_generation')=? AND state IN ('prepared','sending','unknown')`, workerID, generation).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("worker has a queued safe history rebuild")
	}
	return nil
}

const listHostCompactionsSQL = `SELECT o.payload FROM operations o JOIN workers w ON w.id=json_extract(o.payload,'$.target_id') JOIN locations l ON l.id=json_extract(w.payload,'$.location_id') WHERE o.kind='worker.compact.rebuild' AND o.state IN ('prepared','sending','unknown') AND json_extract(l.payload,'$.host_id')=? ORDER BY json_extract(o.payload,'$.created_at'),o.id LIMIT ?`

func (t *Tx) ListHostCompactions(hostID string, limit int) ([]work.Operation, error) {
	if limit <= 0 || limit > 64 {
		limit = 16
	}
	rows, err := t.tx.QueryContext(t.ctx, listHostCompactionsSQL, hostID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var operations []work.Operation
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var op work.Operation
		if err = json.Unmarshal([]byte(raw), &op); err != nil {
			return nil, err
		}
		operations = append(operations, op)
	}
	return operations, rows.Err()
}
