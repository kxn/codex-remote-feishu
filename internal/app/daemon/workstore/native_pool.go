package workstore

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A prepared launch reserves capacity before any process side effect. Unknown
// launches retain it. Only a verified, confirmed carrier rebuild releases it.
const nativePoolPredicate = ` FROM workers w JOIN contexts pool_context ON pool_context.id=json_extract(w.payload,'$.context_id') JOIN locations l ON l.id=json_extract(w.payload,'$.location_id') WHERE json_extract(l.payload,'$.host_id')=? AND EXISTS(SELECT 1 FROM operations o WHERE json_extract(o.payload,'$.kind')='worker.launch' AND json_extract(o.payload,'$.target_id')=w.id AND json_extract(o.payload,'$.target_generation')=json_extract(w.payload,'$.generation') AND json_extract(o.payload,'$.state') IN ('prepared','sending','unknown','confirmed')) AND NOT EXISTS(SELECT 1 FROM operations o WHERE json_extract(o.payload,'$.kind')='worker.rebuild' AND json_extract(o.payload,'$.target_id')=w.id AND json_extract(o.payload,'$.target_generation')=json_extract(w.payload,'$.generation') AND json_extract(o.payload,'$.state')='confirmed')`

func (t *Tx) CountNativePoolCarriers(hostID string) (int, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*)`+nativePoolPredicate, hostID).Scan(&count)
	return count, err
}

func (t *Tx) CountBusinessNativePoolCarriers(hostID string) (int, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, `SELECT count(*)`+nativePoolPredicate+` AND coalesce(json_extract(pool_context.payload,'$.purpose'),'')<>'control'`, hostID).Scan(&count)
	return count, err
}

func (t *Tx) NativePoolRetirementCandidates(hostID string, limit int) ([]work.WorkerSession, error) {
	if limit <= 0 || limit > 64 {
		limit = 32
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT w.payload`+nativePoolPredicate+` AND json_extract(w.payload,'$.state')='ready' ORDER BY json_extract(w.payload,'$.updated_at'),w.id LIMIT ?`, hostID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []work.WorkerSession
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var w work.WorkerSession
		if err = json.Unmarshal([]byte(raw), &w); err != nil {
			return nil, err
		}
		values = append(values, w)
	}
	return values, rows.Err()
}
