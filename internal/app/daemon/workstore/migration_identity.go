package workstore

import (
	"database/sql"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) VerifyMigrationIdentity(identity work.ExternalIdentity, evidenceRef string) error {
	existing, err := t.GetExternalIdentity(identity.IdentityScope)
	if err != nil {
		return err
	}
	if existing.PrincipalID != identity.PrincipalID {
		return ErrIdentityConflict
	}
	observations, err := t.ListIngressObservations(evidenceRef)
	if err != nil {
		return err
	}
	for _, observation := range observations {
		if observation.Envelope.SenderType != "user" {
			continue
		}
		evidence := work.IdentityEvidence{Tenant: observation.Envelope.Tenant, AppID: observation.Envelope.AppID, Generation: observation.Gateway.BotGeneration, UnionDomain: observation.Gateway.UnionDomain, IDs: observation.Envelope.SenderIDs}
		for _, alias := range identityAliases(evidence) {
			if alias == identity.IdentityScope {
				return nil
			}
		}
	}
	return ErrScopeDenied
}
func (t *Tx) GetMigrationUnit(id, unit string) (MigrationUnit, error) {
	var v MigrationUnit
	err := t.tx.QueryRowContext(t.ctx, `SELECT migration_id,unit_key,principal_id,role_id,scope_id,context_id FROM work_migration_units WHERE migration_id=? AND unit_key=?`, id, unit).Scan(&v.MigrationID, &v.UnitKey, &v.PrincipalID, &v.RoleID, &v.ScopeID, &v.ContextID)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, err
}
func (t *Tx) PutMigrationUnit(v MigrationUnit) error {
	old, err := t.GetMigrationUnit(v.MigrationID, v.UnitKey)
	if err == nil {
		if old != v {
			return ErrIdentityConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	scope, err := t.GetScope(v.ScopeID)
	if err != nil {
		return err
	}
	context, err := t.GetContext(v.ContextID)
	if err != nil {
		return err
	}
	if scope.Kind != work.ScopePersonal || scope.PrincipalID != v.PrincipalID || scope.RoleID != v.RoleID || context.ScopeID != v.ScopeID || context.RoleID != v.RoleID {
		return ErrScopeDenied
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_migration_units(migration_id,unit_key,principal_id,role_id,scope_id,context_id) VALUES(?,?,?,?,?,?)`, v.MigrationID, v.UnitKey, v.PrincipalID, v.RoleID, v.ScopeID, v.ContextID)
	return err
}
func (t *Tx) RegisterMigrationNativeOwner(nativeKey string, unit MigrationUnit, workerID string) error {
	if err := t.AssertMigrationNativeOwner(nativeKey, unit, workerID); err != nil {
		return err
	}
	var principal, role, worker string
	err := t.tx.QueryRowContext(t.ctx, `SELECT principal_id,role_id,worker_id FROM work_migration_native_owners WHERE native_key=?`, nativeKey).Scan(&principal, &role, &worker)
	if err == nil {
		if principal != unit.PrincipalID || role != unit.RoleID || worker != workerID {
			return ErrIdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = t.tx.ExecContext(t.ctx, `INSERT INTO work_migration_native_owners(native_key,migration_id,unit_key,principal_id,role_id,worker_id) VALUES(?,?,?,?,?,?)`, nativeKey, unit.MigrationID, unit.UnitKey, unit.PrincipalID, unit.RoleID, workerID)
	return err
}

func (t *Tx) AssertMigrationNativeOwner(nativeKey string, unit MigrationUnit, workerID string) error {
	var principal, role, worker string
	err := t.tx.QueryRowContext(t.ctx, `SELECT principal_id,role_id,worker_id FROM work_migration_native_owners WHERE native_key=?`, nativeKey).Scan(&principal, &role, &worker)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if principal != unit.PrincipalID || role != unit.RoleID || worker != workerID {
		return ErrIdentityConflict
	}
	return nil
}
