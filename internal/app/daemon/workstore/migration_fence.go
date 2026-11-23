package workstore

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPersonalMigrationFenceV1 = `CREATE TABLE IF NOT EXISTS personal_migration_fences(scope_id TEXT PRIMARY KEY REFERENCES scopes(id), migration_id TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('prepared','draining')), revision INTEGER NOT NULL CHECK(revision>0), created_at TEXT NOT NULL, updated_at TEXT NOT NULL);`

var ErrPersonalMigrationFenced = errors.New("personal owner migration is in progress")

type PersonalMigrationFence struct {
	ScopeID     string
	MigrationID string
	State       string
	Revision    int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// BeginPersonalMigrationFence blocks new owner work in the same transaction
// that persists its epoch. A replay must identify the exact migration.
func (t *Tx) BeginPersonalMigrationFence(scopeID, migrationID string, now time.Time) (PersonalMigrationFence, error) {
	var zero PersonalMigrationFence
	if migrationID == "" || now.IsZero() {
		return zero, fmt.Errorf("migration identity and timestamp required")
	}
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return zero, err
	}
	if scope.Kind != work.ScopePersonal {
		return zero, ErrScopeDenied
	}
	old, err := t.GetPersonalMigrationFence(scopeID)
	if err == nil {
		if old.MigrationID != migrationID {
			return zero, ErrConflict
		}
		return old, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return zero, err
	}
	now = now.UTC()
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO personal_migration_fences(scope_id,migration_id,state,revision,created_at,updated_at) VALUES(?,?,'prepared',1,?,?)`, scopeID, migrationID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return zero, err
	}
	return PersonalMigrationFence{ScopeID: scopeID, MigrationID: migrationID, State: "prepared", Revision: 1, CreatedAt: now, UpdatedAt: now}, nil
}
func (t *Tx) GetPersonalMigrationFence(scopeID string) (PersonalMigrationFence, error) {
	var v PersonalMigrationFence
	var created, updated string
	err := t.tx.QueryRowContext(t.ctx, `SELECT scope_id,migration_id,state,revision,created_at,updated_at FROM personal_migration_fences WHERE scope_id=?`, scopeID).Scan(&v.ScopeID, &v.MigrationID, &v.State, &v.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if v.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return v, err
	}
	v.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return v, err
}
func (t *Tx) StartPersonalMigrationDrain(scopeID, migrationID string, expected int64, now time.Time) (PersonalMigrationFence, error) {
	var zero PersonalMigrationFence
	if migrationID == "" || expected < 1 || now.IsZero() {
		return zero, fmt.Errorf("migration identity, revision and timestamp required")
	}
	res, err := t.tx.ExecContext(t.ctx, `UPDATE personal_migration_fences SET state='draining',revision=revision+1,updated_at=? WHERE scope_id=? AND migration_id=? AND state='prepared' AND revision=?`, now.UTC().Format(time.RFC3339Nano), scopeID, migrationID, expected)
	if err != nil {
		return zero, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return zero, err
	}
	if n != 1 {
		return zero, ErrConflict
	}
	return t.GetPersonalMigrationFence(scopeID)
}
func (t *Tx) AssertPersonalMigrationOpen(scopeID string) error {
	_, err := t.GetPersonalMigrationFence(scopeID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return ErrPersonalMigrationFenced
}

// PersonalSourceForPrivateChat uses only previously frozen SDK ingress to
// distinguish a private project view from a group callback.
func (t *Tx) PersonalSourceForPrivateChat(principalID, roleID, tenant, chat string) (string, error) {
	var scope string
	err := t.tx.QueryRowContext(t.ctx, `SELECT s.id FROM scopes s WHERE s.kind='personal' AND s.principal_id=? AND s.role_id=? AND EXISTS (SELECT 1 FROM raw_ingress r WHERE json_extract(r.payload,'$.route.scope_id')=s.id AND json_extract(r.payload,'$.envelope.tenant')=? AND json_extract(r.payload,'$.envelope.chat_id')=? AND json_extract(r.payload,'$.envelope.chat_type')='p2p') LIMIT 1`, principalID, roleID, tenant, chat).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return scope, err
}
