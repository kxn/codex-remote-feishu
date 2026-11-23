package workstore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// PersonalMigrationDrainStatus reports facts only. An empty set is not a
// cutover receipt: native carrier and transport quiescence need their own
// durable, trusted proof from the host.
type PersonalMigrationDrainStatus struct {
	Fence                         PersonalMigrationFence
	UnsettledAttempts             int64
	UnsettledAdmissions           int64
	UnsettledOperations           int64
	UnsettledOutbox               int64
	UnsettledPrivateCommands      int64
	UnsettledMenuDestinations     int64
	UnsettledPreviews             int64
	UnsettledReactions            int64
	OccupiedCarriers              int64
	OccupiedResources             int64
	OccupiedControllers           int64
	HeldExecutionWorkers          int64
	HeldExecutionAttempts         int64
	NativeTransportProofAvailable bool
}

func (t *Tx) PersonalMigrationStatus(scopeID string) (PersonalMigrationDrainStatus, error) {
	var status PersonalMigrationDrainStatus
	var err error
	if status.Fence, err = t.GetPersonalMigrationFence(scopeID); err != nil {
		return status, err
	}
	if err = t.validatePersonalReadArchives(scopeID); err != nil {
		return status, err
	}
	if err = t.validatePersonalPublicationHolds(scopeID); err != nil {
		return status, err
	}
	if err = t.validatePersonalReactionHolds(scopeID); err != nil {
		return status, err
	}
	if err = t.validatePersonalExecutionHolds(scopeID); err != nil {
		return status, err
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload,payload_hash FROM personal_migration_preserved WHERE scope_id=?`, scopeID)
	if err != nil {
		return status, err
	}
	for rows.Next() {
		var payload, hash string
		if err = rows.Scan(&payload, &hash); err != nil {
			rows.Close()
			return status, err
		}
		sum := sha256.Sum256([]byte(payload))
		if hash != hex.EncodeToString(sum[:]) {
			rows.Close()
			return status, fmt.Errorf("migration preserved record integrity mismatch")
		}
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return status, err
	}
	rows.Close()
	queries := []struct {
		sql    string
		target *int64
	}{
		{`SELECT count(*) FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=? AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload`, &status.HeldExecutionWorkers},
		{`SELECT count(*) FROM attempts a JOIN personal_migration_execution_holds h ON h.worker_id=a.worker_id JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE a.scope_id=? AND a.state IN ('prepared','dispatch_pending','running','reconciling') AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload`, &status.HeldExecutionAttempts},
		{`SELECT count(*) FROM attempts a WHERE a.scope_id=? AND json_extract(a.payload,'$.state') IN ('prepared','dispatch_pending','running','reconciling') AND NOT EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=w.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND h.worker_id=a.worker_id)`, &status.UnsettledAttempts},
		{`SELECT count(*) FROM attempt_input_admissions i JOIN attempts a ON a.id=i.attempt_id WHERE a.scope_id=? AND json_extract(i.payload,'$.state') IN ('sending','unknown','quiesced_unknown') AND NOT EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=w.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND h.worker_id=a.worker_id)`, &status.UnsettledAdmissions},
		{`SELECT count(*) FROM operations o WHERE scope_id=? AND json_extract(payload,'$.state') IN ('prepared','sending','retryable','unknown') AND NOT (json_extract(payload,'$.state')='prepared' AND EXISTS (SELECT 1 FROM personal_migration_preserved p WHERE p.scope_id=o.scope_id AND p.kind='operation' AND p.record_id=o.id AND p.record_revision=o.revision AND p.payload=o.payload)) AND NOT (o.state='unknown' AND o.kind IN ('runtime.inspect','model.list') AND EXISTS (SELECT 1 FROM personal_migration_read_archives a WHERE a.scope_id=o.scope_id AND a.operation_id=o.id AND a.revision=o.revision AND a.operation_payload=o.payload AND ((a.command_payload='null' AND a.process_payload='null' AND NOT EXISTS (SELECT 1 FROM native_commands n WHERE n.command_id=o.id)) OR EXISTS (SELECT 1 FROM native_commands n JOIN runtime_carrier_processes p ON p.incarnation=n.incarnation WHERE n.command_id=o.id AND n.payload=a.command_payload AND p.payload=a.process_payload)))) AND NOT EXISTS (SELECT 1 FROM personal_migration_publication_holds h JOIN outbox b ON b.id=h.outbox_id WHERE h.scope_id=o.scope_id AND h.operation_id=o.id AND h.operation_revision=o.revision AND h.operation_payload=o.payload AND h.outbox_revision=b.revision AND h.outbox_payload=b.payload) AND NOT EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=o.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND EXISTS (SELECT 1 FROM native_commands n WHERE n.command_id=o.id AND n.incarnation=json_extract(w.payload,'$.carrier_incarnation')))`, &status.UnsettledOperations},
		{`SELECT count(*) FROM outbox o WHERE scope_id=? AND json_extract(payload,'$.state') IN ('pending','sending','retryable','unknown') AND NOT (json_extract(payload,'$.state')='pending' AND EXISTS (SELECT 1 FROM personal_migration_preserved p JOIN operations op ON op.id=o.operation_id JOIN personal_migration_preserved q ON q.scope_id=op.scope_id AND q.kind='operation' AND q.record_id=op.id AND q.record_revision=op.revision AND q.payload=op.payload WHERE p.scope_id=o.scope_id AND p.kind='outbox' AND p.record_id=o.id AND p.record_revision=o.revision AND p.payload=o.payload AND json_extract(op.payload,'$.state')='prepared')) AND NOT EXISTS (SELECT 1 FROM personal_migration_publication_holds h JOIN operations p ON p.id=h.operation_id WHERE h.scope_id=o.scope_id AND h.outbox_id=o.id AND h.outbox_revision=o.revision AND h.outbox_payload=o.payload AND h.operation_revision=p.revision AND h.operation_payload=p.payload)`, &status.UnsettledOutbox},
		{`SELECT count(*) FROM private_commands WHERE scope_id=? AND state IN ('sending','unknown')`, &status.UnsettledPrivateCommands},
		{`SELECT count(*) FROM team_private_reactions r WHERE json_extract(r.payload,'$.scope_id')=? AND coalesce(json_extract(r.payload,'$.in_flight_action'),'')<>'' AND NOT EXISTS (SELECT 1 FROM personal_migration_reaction_holds h WHERE h.scope_id=json_extract(r.payload,'$.scope_id') AND h.reaction_id=r.id AND h.reaction_revision=json_extract(r.payload,'$.revision') AND h.reaction_payload=r.payload)`, &status.UnsettledReactions},
		{`SELECT count(*) FROM menu_interactions WHERE json_extract(payload,'$.scope_id')=? AND json_extract(payload,'$.destination_state') IN ('sending','unknown')`, &status.UnsettledMenuDestinations},
		{`SELECT count(*) FROM private_execution_display d WHERE json_extract(d.payload,'$.Binding.scopeId')=? AND json_extract(d.payload,'$.PreviewTerminal') IS NOT NULL AND NOT EXISTS (SELECT 1 FROM personal_migration_preserved p WHERE p.scope_id=json_extract(d.payload,'$.Binding.scopeId') AND p.kind='preview' AND p.record_id=d.id AND p.record_revision=d.revision AND p.payload=d.payload AND json_extract(d.payload,'$.PreviewState')='queued') AND NOT EXISTS (SELECT 1 FROM attempts a WHERE a.id=json_extract(d.payload,'$.Binding.attemptId') AND EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=w.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND h.worker_id=a.worker_id))`, &status.UnsettledPreviews},
		{`SELECT count(*) FROM carrier_occupancy c JOIN attempts a ON a.id=c.attempt_id WHERE a.scope_id=? AND NOT EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=w.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND h.worker_id=a.worker_id)`, &status.OccupiedCarriers},
		{`SELECT count(*) FROM resource_occupancy c JOIN attempts a ON a.id=c.attempt_id WHERE a.scope_id=? AND NOT EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=w.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND h.worker_id=a.worker_id)`, &status.OccupiedResources},
		{`SELECT count(*) FROM runtime_controller_occupancy c JOIN workers w ON w.id=c.worker_id WHERE w.scope_id=? AND NOT EXISTS (SELECT 1 FROM personal_migration_execution_holds h JOIN workers w ON w.id=h.worker_id JOIN runtime_carrier_processes p ON p.incarnation=json_extract(w.payload,'$.carrier_incarnation') WHERE h.scope_id=w.scope_id AND h.worker_revision=w.revision AND h.worker_payload=w.payload AND h.process_payload=p.payload AND h.worker_id=c.worker_id)`, &status.OccupiedControllers},
	}
	for _, q := range queries {
		if err = t.tx.QueryRowContext(t.ctx, q.sql, scopeID).Scan(q.target); err != nil {
			return PersonalMigrationDrainStatus{}, err
		}
	}
	return status, nil
}
