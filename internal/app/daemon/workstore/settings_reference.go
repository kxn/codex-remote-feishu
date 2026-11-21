package workstore

import (
	"database/sql"
	"errors"
	"fmt"
)

const SchemaSettingsReferenceV1 = `
CREATE TABLE IF NOT EXISTS role_settings_references (
 role_id TEXT PRIMARY KEY REFERENCES roles(id),
 source_hash TEXT NOT NULL,
 settings_revision INTEGER NOT NULL CHECK(settings_revision>0)
);
`

// SyncRoleSettingsReference tracks changes to the existing global Bot settings.
// It stores no second editable provider/profile definition and no credentials.
func (t *Tx) SyncRoleSettingsReference(roleID, sourceHash string) error {
	if sourceHash == "" {
		return fmt.Errorf("settings source fingerprint required")
	}
	role, err := t.GetRole(roleID)
	if err != nil {
		return err
	}
	var oldHash string
	err = t.tx.QueryRowContext(t.ctx, `SELECT source_hash FROM role_settings_references WHERE role_id=?`, roleID).Scan(&oldHash)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if oldHash == sourceHash {
		return nil
	}
	if oldHash != "" {
		expected := role.Revision
		role.Revision++
		role.SettingsRevision++
		if err = t.PutRole(role, expected); err != nil {
			return err
		}
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO role_settings_references(role_id,source_hash,settings_revision) VALUES(?,?,?) ON CONFLICT(role_id) DO UPDATE SET source_hash=excluded.source_hash,settings_revision=excluded.settings_revision`, roleID, sourceHash, role.SettingsRevision)
	return err
}

func (t *Tx) CheckRoleSettingsReference(roleID, sourceHash string, revision int64) error {
	var hash string
	var rev int64
	err := t.tx.QueryRowContext(t.ctx, `SELECT source_hash,settings_revision FROM role_settings_references WHERE role_id=?`, roleID).Scan(&hash, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if hash != sourceHash || rev != revision {
		return ErrConflict
	}
	return nil
}

// CheckContextPreferencesRevision treats a context without overrides as revision zero.
func (t *Tx) CheckContextPreferencesRevision(contextID string, expected int64) error {
	preferences, err := t.GetContextPreferences(contextID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if preferences.Revision != expected {
		return ErrConflict
	}
	return nil
}
