package workstore

import (
	"fmt"
	"strings"
)

// Every source table must have an explicit disposition. Newly introduced
// tables fail capture until their ownership and recovery role are reviewed.
func (t *Tx) personalExportTablePolicy() ([]PersonalExportTablePolicy, error) {
	included := map[string]bool{"content": true}
	for _, q := range personalExportQueries {
		included[q.table] = true
	}
	refOnly := words("profile_model_catalogs work_migrations work_authority work_legacy_observations personal_migration_seals")
	excluded := words("agreement_versions agreements authority bootstrap_adoptions bootstrap_leader_selections channel_bindings decision_requests decisions deliveries dependencies dependency_satisfaction_history evidence ingress_selection_notices membership_checks practice_adoptions practices private_project_identity_observations private_project_shares project_homes project_members project_principal_access project_role_descriptions projects provision_steps publication_grants resource_collections resource_versions resources review_failure_receipts review_results scheduler_cursors task_conversations task_occupancy task_requirements task_topic_reservations tasks team_dispatch_problems team_members team_ownership team_publication_budget teams trusted_practices work_migration_resource_fences")
	rows, err := t.tx.QueryContext(t.ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []PersonalExportTablePolicy
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		item := PersonalExportTablePolicy{Table: name}
		switch {
		case refOnly[name]:
			item.Disposition, item.Reason = "ref_only", "shared authority, account catalog or unscoped evidence needs external ownership verification"
		case included[name]:
			item.Disposition, item.Reason = "included", "explicit personal scope or exact owned relation"
		case excluded[name]:
			item.Disposition, item.Reason = "excluded", "project, shared authority or team scheduler state is not personal execution ownership"
		default:
			return nil, fmt.Errorf("personal export table %q has no reviewed disposition", name)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
func words(s string) map[string]bool {
	result := map[string]bool{}
	for _, w := range strings.Fields(s) {
		result[w] = true
	}
	return result
}
