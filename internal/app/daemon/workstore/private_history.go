package workstore

import "github.com/kxn/codex-remote-feishu/internal/core/work"

// ListClaimedMigrationHistory exposes only already authenticated import units
// owned by this exact private principal+role+context. Quarantine is not a list.
func (t *Tx) ListClaimedMigrationHistory(scopeID, contextID string) ([]MigrationItem, error) {
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return nil, err
	}
	wc, err := t.GetContext(contextID)
	if err != nil {
		return nil, err
	}
	if scope.Kind != work.ScopePersonal || wc.ScopeID != scope.ID || wc.RoleID != scope.RoleID {
		return nil, ErrScopeDenied
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT r.migration_id,r.record_key FROM work_migration_records r JOIN work_migration_units u ON u.migration_id=r.migration_id AND u.unit_key=r.unit_key WHERE r.scope_id=? AND r.context_id=? AND r.state='imported' AND u.principal_id=? AND u.role_id=? AND u.scope_id=? AND u.context_id=? ORDER BY r.record_key LIMIT 256`, scopeID, contextID, scope.PrincipalID, scope.RoleID, scopeID, contextID)
	if err != nil {
		return nil, err
	}
	type key struct{ migration, id string }
	var keys []key
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.migration, &k.id); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var result []MigrationItem
	for _, key := range keys {
		item, e := t.GetMigrationItem(key.migration, key.id)
		if e != nil {
			return nil, e
		}
		result = append(result, item)
	}
	return result, nil
}
