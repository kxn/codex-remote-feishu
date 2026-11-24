package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type MigrationRecoverySummary struct{ UnconfirmedReplies, ClaimedInputs, OtherInputs, SuspendedInputs, UnresolvedExecutions, UnresolvedControls int }

func (j *Journal) MigrationRecoverySummary(ctx context.Context, o Owner, generation int64) (MigrationRecoverySummary, error) {
	var s MigrationRecoverySummary
	if !validOwner(o) || generation < 1 {
		return s, ErrJournalState
	}
	err := j.db.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=?1 AND i.app_id=?2 AND i.gateway=?3 AND i.surface=?4 AND i.chat=?5 AND i.user_id=?6 AND i.generation=?7 AND r.source_table='personal_migration_publication_holds'),(SELECT count(*) FROM private_migration_input_holds h JOIN private_migration_imports i ON i.import_id=h.import_id WHERE i.tenant=?1 AND i.app_id=?2 AND i.gateway=?3 AND i.surface=?4 AND i.chat=?5 AND i.user_id=?6 AND i.generation=?7 AND h.reason='claimed_input_requires_native_reconciliation' AND NOT EXISTS (SELECT 1 FROM private_migration_recovered_inputs x WHERE x.import_id=h.import_id AND x.inbox_id=h.inbox_id AND x.payload_hash=h.payload_hash)),(SELECT count(*) FROM private_migration_input_holds h JOIN private_migration_imports i ON i.import_id=h.import_id WHERE i.tenant=?1 AND i.app_id=?2 AND i.gateway=?3 AND i.surface=?4 AND i.chat=?5 AND i.user_id=?6 AND i.generation=?7 AND h.reason NOT IN ('claimed_input_requires_native_reconciliation','internal_component_nonreplayable') AND NOT EXISTS (SELECT 1 FROM private_migration_recovered_inputs x WHERE x.import_id=h.import_id AND x.inbox_id=h.inbox_id AND x.payload_hash=h.payload_hash))`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&s.UnconfirmedReplies, &s.ClaimedInputs, &s.OtherInputs)
	if err != nil {
		return s, err
	}
	err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.source_table='personal_migration_execution_holds' AND (coalesce(json_extract(r.payload,'$.evidence.NativeCommandsIdle'),0)=0 OR coalesce(json_extract(r.payload,'$.evidence.InputsIdle'),0)=0 OR coalesce(json_extract(r.payload,'$.evidence.ToolsIdle'),0)=0 OR coalesce(json_extract(r.payload,'$.evidence.GoalsIdle'),0)=0 OR coalesce(json_extract(r.payload,'$.evidence.TasksIdle'),0)=0 OR coalesce(json_extract(r.payload,'$.evidence.TurnsTerminal'),0)=0 OR coalesce(json_extract(r.payload,'$.evidence.UncertaintiesEmpty'),0)=0)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&s.UnresolvedExecutions)
	if err != nil {
		return s, err
	}
	err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_records r JOIN private_migration_imports i ON i.import_id=r.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND r.source_table='personal_migration_control_holds'`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&s.UnresolvedControls)
	if err != nil {
		return s, err
	}
	var count int
	var raw []byte
	var hash string
	err = j.db.QueryRowContext(ctx, `SELECT q.item_count,q.payload,q.payload_hash FROM private_migration_suspended_queue q JOIN private_migration_imports i ON i.import_id=q.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&count, &raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var cp orchestrator.PrivateCheckpoint
	if migrationSHA(raw) != hash || json.Unmarshal(raw, &cp) != nil || len(cp.QueuedQueueItemIDs) != count {
		return s, ErrJournalConflict
	}
	var recovered int
	err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovered_inputs x JOIN private_migration_imports i ON i.import_id=x.import_id JOIN private_migration_suspended_queue q ON q.import_id=i.import_id WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND x.payload_hash=q.payload_hash AND x.inbox_id LIKE 'queue:%'`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&recovered)
	if err != nil {
		return s, err
	}
	if recovered > count {
		return s, ErrJournalConflict
	}
	s.SuspendedInputs = count - recovered
	return s, nil
}
