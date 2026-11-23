package workstore

import (
	"time"
)

// Read a bounded, resumable scan. A registered unknown response is never
// selected: a missing native acknowledgement is not permission to resend it.
func (t *Tx) ListRecoverableNativeResponseIDs(after string, limit int, now time.Time) ([]string, error) {
	if limit < 1 || limit > 100 {
		limit = 32
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT r.id FROM native_ui_requests r JOIN operations o ON o.id=json_extract(r.payload,'$.response_operation_id') WHERE r.id>? AND r.state='response_pending' AND o.kind='native.request_response' AND (coalesce(json_extract(o.payload,'$.next_attempt_at'),'')='' OR julianday(json_extract(o.payload,'$.next_attempt_at'))<=julianday(?)) AND ((o.state='unknown' AND json_extract(o.payload,'$.target_id')=r.id AND NOT EXISTS(SELECT 1 FROM native_commands n WHERE n.command_id=o.id)) OR (o.state='retryable' AND EXISTS(SELECT 1 FROM json_each(o.payload,'$.steps') s WHERE json_extract(s.value,'$.id') IN ('native_response_unsent','local_registration_target_repair') AND json_extract(s.value,'$.state')='confirmed' AND EXISTS(SELECT 1 FROM content c WHERE c.id=json_extract(s.value,'$.receipt'))))) ORDER BY r.id LIMIT ?`, after, now.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
