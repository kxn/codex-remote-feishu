package workstore

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const SchemaPersonalMigrationSealV1 = `CREATE TABLE IF NOT EXISTS personal_migration_seals(scope_id TEXT NOT NULL REFERENCES personal_migration_fences(scope_id),migration_id TEXT NOT NULL,fence_revision INTEGER NOT NULL,source_hash TEXT NOT NULL,semantic_hash TEXT NOT NULL,proof_hash TEXT NOT NULL,manifest BLOB NOT NULL,evidence BLOB NOT NULL,created_at TEXT NOT NULL,PRIMARY KEY(scope_id,migration_id));`

type PersonalMigrationSeal struct {
	ScopeID, MigrationID                string
	FenceRevision                       int64
	SourceHash, SemanticHash, ProofHash string
	Manifest, Evidence                  []byte
	CreatedAt                           time.Time
}

// StorePersonalMigrationSeal stores bytes only after the caller has checked
// actual runtime proofs. Readers must independently revalidate the manifest,
// source state and neutral coverage; this record alone is not authority.
func (t *Tx) StorePersonalMigrationSeal(v PersonalMigrationSeal) error {
	f, err := t.GetPersonalMigrationFence(v.ScopeID)
	if err != nil {
		return err
	}
	if f.State != "draining" || f.MigrationID != v.MigrationID || f.Revision != v.FenceRevision || v.CreatedAt.IsZero() || len(v.Manifest) == 0 || len(v.Evidence) == 0 {
		return ErrConflict
	}
	h := sha256.Sum256(v.Manifest)
	if hex.EncodeToString(h[:]) != v.SourceHash {
		return fmt.Errorf("source manifest hash mismatch")
	}
	result, err := t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_seals(scope_id,migration_id,fence_revision,source_hash,semantic_hash,proof_hash,manifest,evidence,created_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(scope_id,migration_id) DO NOTHING`, v.ScopeID, v.MigrationID, v.FenceRevision, v.SourceHash, v.SemanticHash, v.ProofHash, v.Manifest, v.Evidence, v.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	old, err := t.GetPersonalMigrationSeal(v.ScopeID, v.MigrationID)
	if err != nil {
		return err
	}
	if old.SourceHash != v.SourceHash || old.SemanticHash != v.SemanticHash || old.ProofHash != v.ProofHash || old.FenceRevision != v.FenceRevision || !bytes.Equal(old.Manifest, v.Manifest) || !bytes.Equal(old.Evidence, v.Evidence) {
		return ErrConflict
	}
	return nil
}
func (t *Tx) GetPersonalMigrationSeal(scopeID, migrationID string) (PersonalMigrationSeal, error) {
	var v PersonalMigrationSeal
	var at string
	err := t.tx.QueryRowContext(t.ctx, `SELECT scope_id,migration_id,fence_revision,source_hash,semantic_hash,proof_hash,manifest,evidence,created_at FROM personal_migration_seals WHERE scope_id=? AND migration_id=?`, scopeID, migrationID).Scan(&v.ScopeID, &v.MigrationID, &v.FenceRevision, &v.SourceHash, &v.SemanticHash, &v.ProofHash, &v.Manifest, &v.Evidence, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
	return v, err
}
