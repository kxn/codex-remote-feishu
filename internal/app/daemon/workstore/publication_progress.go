package workstore

// A result accepted in this run closes its task or taskless topic to further
// progress from the same run. Separate work streams may still report progress.
func (t *Tx) HasFinalPublication(scopeID, attemptID, taskID, rootMessageID string) (bool, error) {
	var exists bool
	err := t.tx.QueryRowContext(t.ctx, `
		SELECT EXISTS (
			SELECT 1 FROM operations o
			JOIN content c ON c.id=json_extract(o.payload,'$.request_ref')
			WHERE o.scope_id=? AND o.kind='feishu.publish' AND o.actor_kind='role'
			  AND json_extract(CAST(c.data AS TEXT),'$.authority.attempt_id')=?
			  AND json_extract(CAST(c.data AS TEXT),'$.purpose')='final'
			  AND coalesce(json_extract(CAST(c.data AS TEXT),'$.task_id'),'')=?
			  AND (?<>'' OR coalesce(json_extract(CAST(c.data AS TEXT),'$.root_message_id'),'')=?)
		)`, scopeID, attemptID, taskID, taskID, rootMessageID).Scan(&exists)
	return exists, err
}
