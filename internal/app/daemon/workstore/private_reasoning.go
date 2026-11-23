package workstore

import "encoding/json"

func (t *Tx) ListDirtyPrivateReasoning(limit int) ([]PrivateExecutionDisplay, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT d.payload FROM private_execution_display d WHERE coalesce(json_extract(d.payload,'$.Terminated'),0)=0 AND coalesce(json_extract(d.payload,'$.Progress.Reasoning.Revision'),0)>coalesce(json_extract(d.payload,'$.Progress.Reasoning.LastEmittedRevision'),0) AND NOT EXISTS (SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=json_extract(d.payload,'$.Binding.scopeId')) ORDER BY json_extract(d.payload,'$.Progress.LastEmittedAt'),d.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []PrivateExecutionDisplay
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v PrivateExecutionDisplay
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}
