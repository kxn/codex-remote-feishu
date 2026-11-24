package workstore

import "database/sql"

// PersonalMigrationComponentProofIDs returns only proofs already referenced by
// this scope's preserved components. The caller must verify them with ingress.
func (t *Tx) PersonalMigrationComponentProofIDs(scope string) ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT intake_proof_id FROM personal_migration_execution_holds WHERE scope_id=? UNION SELECT intake_proof_id FROM personal_migration_publication_holds WHERE scope_id=? UNION SELECT intake_proof_id FROM personal_migration_reaction_holds WHERE scope_id=? UNION SELECT intake_proof_id FROM personal_migration_control_holds WHERE scope_id=?`, scope, scope, scope, scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// PersonalMigrationComponentProof preserves the proof originally committed
// with an existing component. Its caller verifies every referenced proof first.
func (t *Tx) PersonalMigrationComponentProof(scope, kind, id, current string) (string, bool, error) {
	var query string
	switch kind {
	case "execution":
		query = `SELECT intake_proof_id FROM personal_migration_execution_holds WHERE scope_id=? AND worker_id=?`
	case "publication":
		query = `SELECT intake_proof_id FROM personal_migration_publication_holds WHERE scope_id=? AND outbox_id=?`
	case "reaction":
		query = `SELECT intake_proof_id FROM personal_migration_reaction_holds WHERE scope_id=? AND reaction_id=?`
	case "control":
		query = `SELECT intake_proof_id FROM personal_migration_control_holds WHERE scope_id=? AND operation_id=?`
	default:
		return "", false, ErrConflict
	}
	var proof string
	err := t.tx.QueryRowContext(t.ctx, query, scope, id).Scan(&proof)
	if err == sql.ErrNoRows {
		return current, false, nil
	}
	return proof, err == nil, err
}
