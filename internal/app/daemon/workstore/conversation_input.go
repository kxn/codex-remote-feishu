package workstore

// ConversationHumanRevision uses durable arrival order, not client clocks. A
// new authenticated human instruction creates a fresh coordination episode.
func (t *Tx) ConversationHumanRevision(scopeID, conversationID, taskID string) (int64, error) {
	var revision int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT coalesce(max(m.rowid),0) FROM messages m WHERE m.scope_id=? AND json_extract(m.payload,'$.source.kind')='principal' AND (json_extract(m.payload,'$.conversation_id')=? OR (?<>'' AND EXISTS(SELECT 1 FROM inbox i WHERE i.message_id=m.id AND i.task_id=?)))`, scopeID, conversationID, taskID, taskID).Scan(&revision)
	return revision, err
}
