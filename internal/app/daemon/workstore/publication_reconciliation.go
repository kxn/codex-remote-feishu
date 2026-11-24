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
		query = `SELECT v.id FROM team_ui_views v LEFT JOIN scopes s ON s.id=v.scope_id WHERE v.id>? AND (s.id IS NULL OR s.kind IN ('project','bootstrap')) AND json_extract(v.payload,'$.external_message_id')<>'' AND json_extract(v.payload,'$.invalidated')=0 ORDER BY v.id LIMIT ?`
	case "team_setup_ingress":
		query = `SELECT n.ingress_id FROM ingress_selection_notices n LEFT JOIN raw_ingress r ON r.id=n.ingress_id WHERE n.ingress_id>? AND (r.id IS NULL OR json_extract(r.payload,'$.envelope.chat_type')='group') AND json_extract(n.payload,'$.state')='confirmed' AND json_extract(n.payload,'$.external_message_id')<>'' ORDER BY n.ingress_id LIMIT ?`
	case "team_message":
		query = `SELECT o.id FROM outbox o LEFT JOIN operations p ON p.id=o.operation_id LEFT JOIN scopes s ON s.id=o.scope_id WHERE o.id>? AND (s.id IS NULL OR s.kind IN ('project','bootstrap')) AND o.state='sent' AND (p.id IS NULL OR p.kind='feishu.publish') AND json_extract(o.payload,'$.external_message_id')<>'' ORDER BY o.id LIMIT ?`
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
