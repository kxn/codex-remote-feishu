package workstore

import "encoding/json"

func (t *Tx) ListDirtyPrivateReasoning(limit int) ([]PrivateExecutionDisplay, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM private_execution_display WHERE coalesce(json_extract(payload,'$.Terminated'),0)=0 AND coalesce(json_extract(payload,'$.Progress.Reasoning.Revision'),0)>coalesce(json_extract(payload,'$.Progress.Reasoning.LastEmittedRevision'),0) ORDER BY json_extract(payload,'$.Progress.LastEmittedAt'),id LIMIT ?`, limit)
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
