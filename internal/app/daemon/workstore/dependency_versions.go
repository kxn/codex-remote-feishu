package workstore

import "time"

// Revision/assignment changes invalidate only the live satisfaction. The prior
// fixed conclusion remains auditable and cannot satisfy a changed child.
func (t *Tx) invalidateDependentConclusions(childID string, requirementsRevision, assignmentGeneration int64, at time.Time) error {
	if at.IsZero() {
		at = time.Now().UTC()
	}
	if _, err := t.tx.ExecContext(t.ctx, "INSERT INTO dependency_satisfaction_history(parent_id,child_id,condition,child_requirements_revision,child_assignment_generation,source_ref,superseded_at) SELECT parent_id,child_id,condition,?,?,satisfied_by,? FROM dependencies WHERE child_id=? AND satisfied_by IS NOT NULL", requirementsRevision, assignmentGeneration, at.UTC().Format(time.RFC3339Nano), childID); err != nil {
		return err
	}
	_, err := t.tx.ExecContext(t.ctx, "UPDATE dependencies SET satisfied_by=NULL WHERE child_id=? AND satisfied_by IS NOT NULL", childID)
	return err
}
