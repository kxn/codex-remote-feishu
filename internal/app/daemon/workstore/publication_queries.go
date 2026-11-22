package workstore

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// The operation index already narrows by role, scope and kind. This lookup
// lets SQLite join those operations to their outbox rows without scanning
// publication history from other roles or scopes.
const SchemaPublicationOutboxIndexV1 = `CREATE INDEX IF NOT EXISTS outbox_operation_scope ON outbox(operation_id,scope_id)`

type PublicationRecord struct {
	Outbox    work.Outbox
	Operation work.Operation
	Content   work.Content
}

func (t *Tx) ListPendingPublicationOutbox(scopeID string) ([]work.Outbox, error) {
	return listRecords[work.Outbox](t, `(
		SELECT item.id, item.payload FROM outbox AS item
		JOIN operations AS op ON op.id=item.operation_id AND op.scope_id=item.scope_id
		WHERE item.scope_id=? AND item.state IN ('pending','retryable') AND op.kind='feishu.publish'
	)`, "", scopeID)
}

// ListRoleProgressPublications finds ongoing or historical progress/final
// publications in a single role stream. A task retains one stream across
// topic-root replacement. A taskless stream remains specific to its root.
func (t *Tx) ListRoleProgressPublications(scopeID, roleID, tenant, chatID, taskID, rootID string) ([]PublicationRecord, error) {
	rows, err := t.tx.QueryContext(t.ctx, `
		SELECT item.payload, op.payload, body.id, body.scope_id, body.media_type, body.sha256, body.data, body.created_at
		FROM operations AS op
		JOIN outbox AS item ON item.operation_id=op.id AND item.scope_id=op.scope_id
		JOIN content AS body ON body.id=json_extract(item.payload,'$.payload_ref')
			AND body.id=json_extract(op.payload,'$.request_ref') AND body.scope_id=item.scope_id
		WHERE op.scope_id=?1 AND op.actor_kind='role' AND op.actor_id=?2 AND op.kind='feishu.publish'
			AND op.state<>'terminal' AND item.state<>'terminal'
			AND CASE WHEN json_valid(CAST(body.data AS TEXT)) THEN
				json_extract(CAST(body.data AS TEXT),'$.purpose') IN ('progress','final')
				AND json_extract(CAST(body.data AS TEXT),'$.role_id')=?2
				AND json_extract(CAST(body.data AS TEXT),'$.tenant')=?3
				AND json_extract(CAST(body.data AS TEXT),'$.chat_id')=?4
				AND coalesce(json_extract(CAST(body.data AS TEXT),'$.task_id'),'')=?5
				AND (?5<>'' OR coalesce(json_extract(CAST(body.data AS TEXT),'$.root_message_id'),'')=?6)
			ELSE 0 END
		ORDER BY item.id
	`, scopeID, roleID, tenant, chatID, taskID, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []PublicationRecord{}
	for rows.Next() {
		var record PublicationRecord
		var outbox, operation, createdAt string
		body := &record.Content
		if err = rows.Scan(&outbox, &operation, &body.ID, &body.ScopeID, &body.MediaType, &body.SHA256, &body.Data, &createdAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(outbox), &record.Outbox); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(operation), &record.Operation); err != nil {
			return nil, err
		}
		if record.Content, err = validateStoredContent(*body, createdAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
