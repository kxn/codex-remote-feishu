package privatechat

import (
	"context"
	"encoding/json"
)

// MigrationPublicationHold is an unresolved historical display delivery. It
// is not a target outbox and is never sent automatically.
type MigrationPublicationHold struct {
	OutboxID, OperationID, State string
	SourceRevision               int64
}

func (j *Journal) ListMigrationPublicationHolds(ctx context.Context, owner Owner, generation int64, after string, limit int) ([]MigrationPublicationHold, error) {
	if !validOwner(owner) || generation < 1 || limit < 1 || limit > 100 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT r.source_key,r.source_revision,r.payload,r.payload_hash FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.source_table='personal_migration_publication_holds' AND r.source_key>? ORDER BY r.source_key LIMIT ?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MigrationPublicationHold
	for rows.Next() {
		var x MigrationPublicationHold
		var raw []byte
		var hash string
		if err = rows.Scan(&x.OutboxID, &x.SourceRevision, &raw, &hash); err != nil {
			return nil, err
		}
		if migrationSHA(raw) != hash {
			return nil, ErrJournalConflict
		}
		var h struct {
			OutboxID    string `json:"outbox_id"`
			OperationID string `json:"operation_id"`
			Outbox      struct {
				State string `json:"state"`
			} `json:"outbox_payload"`
		}
		if json.Unmarshal(raw, &h) != nil || h.OutboxID != x.OutboxID || h.OperationID == "" || (h.Outbox.State != "sending" && h.Outbox.State != "unknown") {
			return nil, ErrJournalConflict
		}
		x.OperationID = h.OperationID
		x.State = h.Outbox.State
		result = append(result, x)
	}
	return result, rows.Err()
}
