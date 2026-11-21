package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// Every value comes from an exact instance Hello observation. A reused legacy
// instance ID may have several values; conservative fencing retains them all.
func (t *Tx) LegacyObservedBackends(instanceID string) ([]work.Backend, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT DISTINCT json_extract(payload,'$.instance.backend') FROM work_legacy_observations WHERE instance_id=? AND kind='hello' AND json_extract(payload,'$.instance.backend') IN ('codex','claude','opencode')`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []work.Backend
	for rows.Next() {
		var value work.Backend
		if err = rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
