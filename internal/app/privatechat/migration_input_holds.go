package privatechat

import "context"

// ListMigrationInputHolds exposes source inputs whose prior delivery remains
// uncertain or incomplete. They are retained, never automatically replayed.
func (j *Journal) ListMigrationInputHolds(ctx context.Context, owner Owner, generation int64, after string, limit int) ([]MigrationInputHold, error) {
	if !validOwner(owner) || generation < 1 || limit < 1 || limit > 100 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT h.inbox_id,h.message_id,h.reason,h.payload_hash FROM private_migration_input_holds h JOIN private_migration_imports i ON i.import_id=h.import_id JOIN private_migration_records r ON r.import_id=h.import_id AND r.source_table='inbox' AND r.source_key=h.inbox_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND h.inbox_id>? AND h.payload_hash=r.payload_hash ORDER BY h.inbox_id LIMIT ?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MigrationInputHold
	for rows.Next() {
		var h MigrationInputHold
		if err = rows.Scan(&h.InboxID, &h.MessageID, &h.Reason, &h.PayloadHash); err != nil {
			return nil, err
		}
		result = append(result, h)
	}
	return result, rows.Err()
}
