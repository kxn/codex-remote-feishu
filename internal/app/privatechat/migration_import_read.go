package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MigrationReceipt returns only an actually committed target transaction.
func (j *Journal) MigrationReceipt(ctx context.Context, owner Owner, generation int64, importID string) (MigrationImportReceipt, error) {
	var result MigrationImportReceipt
	if !validOwner(owner) || generation < 1 || importID == "" {
		return result, ErrJournalState
	}
	var raw []byte
	err := j.db.QueryRowContext(ctx, `SELECT receipt FROM private_migration_imports WHERE import_id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, importID, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, ErrJournalState
	}
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Owner != owner || result.Generation != generation || result.ImportID != importID {
		return MigrationImportReceipt{}, ErrJournalConflict
	}
	return result, nil
}

// ImportedPersonalMemory returns immutable original entries and versions. It
// does not itself grant an agent access or alter memory authority.
func (j *Journal) ImportedPersonalMemory(ctx context.Context, owner Owner, generation int64, after string, limit int) ([]MigrationImportedRecord, error) {
	if !validOwner(owner) || generation < 1 || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT r.source_table,r.source_key,r.source_scope,r.source_order,r.source_revision,r.payload,r.payload_hash,r.data,r.data_hash FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.source_table='memories' AND r.source_key>? ORDER BY r.source_key LIMIT ?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MigrationImportedRecord
	for rows.Next() {
		var r MigrationImportedRecord
		var payload []byte
		if err = rows.Scan(&r.Table, &r.Key, &r.ScopeID, &r.Order, &r.Revision, &payload, &r.PayloadHash, &r.Data, &r.DataHash); err != nil {
			return nil, err
		}
		r.Payload = append(json.RawMessage(nil), payload...)
		if migrationSHA(r.Payload) != r.PayloadHash || migrationSHA(r.Data) != r.DataHash {
			return nil, ErrJournalConflict
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// ReadImportedPersonalMemory resolves only a valid, exact-version entry and
// verifies its personal content from the same committed import.
func (j *Journal) ReadImportedPersonalMemory(ctx context.Context, owner Owner, generation int64, id string, revision int64) (MigrationImportedRecord, []byte, error) {
	var record MigrationImportedRecord
	if !validOwner(owner) || generation < 1 || id == "" || revision < 1 {
		return record, nil, ErrJournalState
	}
	var payload, data []byte
	var dataHash string
	err := j.db.QueryRowContext(ctx, `SELECT m.source_table,m.source_key,m.source_scope,m.source_order,m.source_revision,m.payload,m.payload_hash,m.data,m.data_hash,c.data,c.data_hash FROM private_migration_records m JOIN private_migration_imports i ON i.import_id=m.import_id JOIN private_migration_records c ON c.import_id=m.import_id AND c.source_table='content' AND c.source_key=json_extract(m.payload,'$.content_ref') WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND m.source_table='memories' AND m.source_key=? AND m.source_revision=? AND json_extract(m.payload,'$.state')='valid'`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, id, revision).Scan(&record.Table, &record.Key, &record.ScopeID, &record.Order, &record.Revision, &payload, &record.PayloadHash, &record.Data, &record.DataHash, &data, &dataHash)
	if errors.Is(err, sql.ErrNoRows) {
		return MigrationImportedRecord{}, nil, ErrJournalState
	}
	if err != nil {
		return MigrationImportedRecord{}, nil, err
	}
	record.Payload = append(json.RawMessage(nil), payload...)
	if migrationSHA(record.Payload) != record.PayloadHash || migrationSHA(record.Data) != record.DataHash || migrationSHA(data) != dataHash {
		return MigrationImportedRecord{}, nil, ErrJournalConflict
	}
	return record, append([]byte(nil), data...), nil
}

// ListAvailableImportedPersonalMemory lists only valid entries for the exact
// private owner; callers must authenticate that owner before invoking it.
func (j *Journal) ListAvailableImportedPersonalMemory(ctx context.Context, owner Owner, generation int64, after string, limit int) ([]MigrationImportedRecord, error) {
	if !validOwner(owner) || generation < 1 || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	rows, err := j.db.QueryContext(ctx, `SELECT r.source_table,r.source_key,r.source_scope,r.source_order,r.source_revision,r.payload,r.payload_hash,r.data,r.data_hash FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.source_table='memories' AND json_extract(r.payload,'$.state')='valid' AND r.source_key>? ORDER BY r.source_key LIMIT ?`, owner.TenantID, owner.AppID, owner.GatewayID, owner.SurfaceID, owner.ChatID, owner.UserID, generation, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MigrationImportedRecord
	for rows.Next() {
		var r MigrationImportedRecord
		var payload []byte
		if err = rows.Scan(&r.Table, &r.Key, &r.ScopeID, &r.Order, &r.Revision, &payload, &r.PayloadHash, &r.Data, &r.DataHash); err != nil {
			return nil, err
		}
		r.Payload = append(json.RawMessage(nil), payload...)
		if migrationSHA(r.Payload) != r.PayloadHash || migrationSHA(r.Data) != r.DataHash {
			return nil, ErrJournalConflict
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
