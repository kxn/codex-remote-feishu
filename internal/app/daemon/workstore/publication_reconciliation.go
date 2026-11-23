package workstore

// ListConfirmedPresentationRefs supplies bounded durable candidates for the
// neutral router projection. Each candidate must still be reverified against
// its confirmed publication receipt before registration.
func (t *Tx) ListConfirmedPresentationRefs(kind, after string, limit int) ([]string, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	var query string
	switch kind {
	case "team_view":
		query = `SELECT id FROM team_ui_views WHERE id>? AND json_extract(payload,'$.external_message_id')<>'' AND json_extract(payload,'$.invalidated')=0 ORDER BY id LIMIT ?`
	case "team_setup_ingress":
		query = `SELECT ingress_id FROM ingress_selection_notices WHERE ingress_id>? AND json_extract(payload,'$.state')='confirmed' AND json_extract(payload,'$.external_message_id')<>'' ORDER BY ingress_id LIMIT ?`
	case "team_message":
		query = `SELECT o.id FROM outbox o JOIN operations p ON p.id=o.operation_id WHERE o.id>? AND o.state='sent' AND p.kind='feishu.publish' AND json_extract(o.payload,'$.external_message_id')<>'' ORDER BY o.id LIMIT ?`
	default:
		return nil, ErrConflict
	}
	rows, err := t.tx.QueryContext(t.ctx, query, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}
