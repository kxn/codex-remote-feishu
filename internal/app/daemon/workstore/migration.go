package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type MigrationState struct {
	ID, SourceFingerprint, Phase  string
	Revision, AuthorityGeneration int64
	Snapshot, Fence               json.RawMessage
}
type MigrationItem struct {
	MigrationID, Key, UnitKey, Kind, State, Reason, ScopeID, ContextID, Owner string
	Payload                                                                   json.RawMessage
	Revision                                                                  int64
}
type MigrationAuthority struct {
	Owner, MigrationID   string
	Generation, Revision int64
}
type MigrationUnit struct{ MigrationID, UnitKey, PrincipalID, RoleID, ScopeID, ContextID string }

func (t *Tx) GetMigration(id string) (MigrationState, error) {
	var v MigrationState
	var fence sql.NullString
	var raw string
	err := t.tx.QueryRowContext(t.ctx, `SELECT id,source_fingerprint,phase,revision,authority_generation,snapshot,fence FROM work_migrations WHERE id=?`, id).Scan(&v.ID, &v.SourceFingerprint, &v.Phase, &v.Revision, &v.AuthorityGeneration, &raw, &fence)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	v.Snapshot = []byte(raw)
	if fence.Valid {
		v.Fence = []byte(fence.String)
	}
	return v, err
}
func (t *Tx) GetMigrationAuthority() (MigrationAuthority, error) {
	var v MigrationAuthority
	err := t.tx.QueryRowContext(t.ctx, `SELECT owner,migration_id,generation,revision FROM work_authority WHERE id='runtime'`).Scan(&v.Owner, &v.MigrationID, &v.Generation, &v.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
func (t *Tx) PrepareMigration(v MigrationState, items []MigrationItem) error {
	return t.atomic(func() error {
		if v.ID == "" || v.SourceFingerprint == "" || !json.Valid(v.Snapshot) || v.Phase != "captured" || v.Revision != 1 {
			return fmt.Errorf("invalid migration capture")
		}
		old, err := t.GetMigration(v.ID)
		if err == nil {
			if old.SourceFingerprint != v.SourceFingerprint {
				return ErrIdempotencyConflict
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		authority, err := t.GetMigrationAuthority()
		if errors.Is(err, ErrNotFound) {
			_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_authority(id,owner,generation,migration_id,revision) VALUES('runtime','legacy',1,?,1)`, v.ID)
			authority = MigrationAuthority{Owner: "legacy", Generation: 1, MigrationID: v.ID, Revision: 1}
		}
		if err != nil {
			return err
		}
		if authority.Owner != "legacy" || authority.MigrationID != v.ID {
			return fmt.Errorf("another authority already owns execution")
		}
		v.AuthorityGeneration = authority.Generation
		if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_migrations(id,source_fingerprint,phase,revision,authority_generation,snapshot) VALUES(?,?,?,?,?,?)`, v.ID, v.SourceFingerprint, v.Phase, v.Revision, v.AuthorityGeneration, string(v.Snapshot)); err != nil {
			return err
		}
		for _, item := range items {
			if item.MigrationID != v.ID || item.Key == "" || item.UnitKey == "" || !json.Valid(item.Payload) {
				return fmt.Errorf("invalid migration item")
			}
			if _, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_migration_records(migration_id,record_key,unit_key,kind,payload,state,revision) VALUES(?,?,?,?,?,'pending',1)`, v.ID, item.Key, item.UnitKey, item.Kind, string(item.Payload)); err != nil {
				return err
			}
		}
		return nil
	})
}
func (t *Tx) UpdateMigration(v MigrationState, expected int64) error {
	old, err := t.GetMigration(v.ID)
	if err != nil {
		return err
	}
	if old.SourceFingerprint != v.SourceFingerprint || old.AuthorityGeneration != v.AuthorityGeneration || v.Revision != expected+1 {
		return ErrConflict
	}
	allowed := old.Phase == v.Phase || (old.Phase == "captured" && v.Phase == "quiescing") || (old.Phase == "quiescing" && v.Phase == "imported") || (old.Phase == "imported" && v.Phase == "authority_committed") || (old.Phase == "authority_committed" && v.Phase == "finalized")
	if !allowed {
		return fmt.Errorf("invalid migration phase transition")
	}
	if v.Phase != "captured" && len(v.Fence) == 0 {
		return fmt.Errorf("migration phase requires persisted fence evidence")
	}
	if len(old.Fence) > 0 && string(old.Fence) != string(v.Fence) {
		return ErrIdempotencyConflict
	}
	if v.Phase == "imported" {
		counts, e := t.MigrationCounts(v.ID)
		if e != nil {
			return e
		}
		if counts["pending"] > 0 {
			return fmt.Errorf("pending migration items remain")
		}
	}
	if v.Phase == "authority_committed" || v.Phase == "finalized" {
		authority, e := t.GetMigrationAuthority()
		if e != nil {
			return e
		}
		if authority.Owner != "team" || authority.MigrationID != v.ID {
			return fmt.Errorf("authority registry has not committed")
		}
	}
	var fence any
	if len(v.Fence) > 0 {
		if !json.Valid(v.Fence) {
			return fmt.Errorf("invalid fence evidence")
		}
		fence = string(v.Fence)
	}
	result, err := t.tx.ExecContext(t.ctx, `UPDATE work_migrations SET phase=?,revision=?,fence=? WHERE id=? AND revision=?`, v.Phase, v.Revision, fence, v.ID, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (t *Tx) GetMigrationItem(id, key string) (MigrationItem, error) {
	var raw string
	var v MigrationItem
	err := t.tx.QueryRowContext(t.ctx, `SELECT migration_id,record_key,unit_key,kind,payload,state,reason,scope_id,context_id,owner,revision FROM work_migration_records WHERE migration_id=? AND record_key=?`, id, key).Scan(&v.MigrationID, &v.Key, &v.UnitKey, &v.Kind, &raw, &v.State, &v.Reason, &v.ScopeID, &v.ContextID, &v.Owner, &v.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	v.Payload = []byte(raw)
	return v, err
}
func (t *Tx) ListMigrationItems(id, state string, limit int) ([]MigrationItem, error) {
	if limit <= 0 || limit > 256 {
		limit = 128
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT record_key FROM work_migration_records WHERE migration_id=? AND state=? ORDER BY record_key LIMIT ?`, id, state, limit)
	if err != nil {
		return nil, err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, key)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]MigrationItem, 0, len(keys))
	for _, key := range keys {
		item, e := t.GetMigrationItem(id, key)
		if e != nil {
			return nil, e
		}
		result = append(result, item)
	}
	return result, nil
}
func (t *Tx) UpdateMigrationItem(v MigrationItem, expected int64) error {
	if v.Revision != expected+1 {
		return ErrConflict
	}
	old, err := t.GetMigrationItem(v.MigrationID, v.Key)
	if err != nil {
		return err
	}
	if old.State == "imported" || (v.State != "imported" && v.State != "quarantined") || (old.Owner != "" && v.Owner != old.Owner) {
		return ErrConflict
	}
	result, err := t.tx.ExecContext(t.ctx, `UPDATE work_migration_records SET state=?,reason=?,scope_id=?,context_id=?,owner=?,revision=? WHERE migration_id=? AND record_key=? AND revision=?`, v.State, v.Reason, v.ScopeID, v.ContextID, v.Owner, v.Revision, v.MigrationID, v.Key, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (t *Tx) MigrationCounts(id string) (map[string]int, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT state,count(*) FROM work_migration_records WHERE migration_id=? GROUP BY state`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var state string
		var count int
		if err = rows.Scan(&state, &count); err != nil {
			return nil, err
		}
		result[state] = count
	}
	return result, rows.Err()
}
func (t *Tx) CommitMigrationAuthority(id string, expected int64) error {
	return t.atomic(func() error {
		migration, err := t.GetMigration(id)
		if err != nil {
			return err
		}
		authority, err := t.GetMigrationAuthority()
		if err != nil {
			return err
		}
		if authority.Owner == "team" && authority.MigrationID == id {
			return nil
		}
		if migration.Phase != "imported" || authority.Owner != "legacy" || authority.Generation != expected || authority.MigrationID != id {
			return ErrConflict
		}
		if _, err = t.tx.ExecContext(t.ctx, `UPDATE work_authority SET owner='team',generation=generation+1,revision=revision+1 WHERE id='runtime' AND generation=? AND owner='legacy'`, expected); err != nil {
			return err
		}
		before := migration.Revision
		migration.Revision++
		migration.Phase = "authority_committed"
		return t.UpdateMigration(migration, before)
	})
}
