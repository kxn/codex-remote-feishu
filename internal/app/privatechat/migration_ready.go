package privatechat

import (
	"context"
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// MigrationReadyReceipt is derived from the committed target journal, never a
// caller assertion that recovery was completed.
type MigrationReadyReceipt struct {
	Import                           MigrationImportReceipt
	CheckpointRevision, Cursor       int64
	CheckpointHash, CheckpointDigest string
}

func (j *Journal) MigrationReadyReceipt(ctx context.Context, req MigrationImportRequest) (MigrationReadyReceipt, error) {
	var out MigrationReadyReceipt
	unlockReferences := j.LockReferenceMutation()
	defer unlockReferences()
	receipt, err := j.MigrationReceipt(ctx, req.Owner, req.Generation, req.ImportID)
	if err != nil {
		return out, err
	}
	j.processorMu.Lock()
	defer j.processorMu.Unlock()
	if j.processor != nil {
		j.processor.mu.Lock()
		defer j.processor.mu.Unlock()
		if j.processor.poisoned {
			return out, ErrProcessorPoisoned
		}
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	o := req.Owner
	var raw []byte
	var cp Checkpoint
	cp.Owner = o
	cp.Generation = req.Generation
	err = tx.QueryRowContext(ctx, `SELECT revision,cursor,data,digest FROM private_checkpoint WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, req.Generation).Scan(&cp.Revision, &cp.Cursor, &raw, &out.CheckpointDigest)
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal(raw, &cp.Core); err != nil {
		return out, err
	}
	if j.processor != nil {
		if loaded, ok := j.processor.owners[req.Owner]; ok && (loaded.generation != req.Generation || loaded.revision != cp.Revision || loaded.cursor != cp.Cursor) {
			return out, ErrJournalConflict
		}
	}
	if !cp.Core.MigrationRecoveryReleased || cp.Core.ActiveQueueItemID != "" || cp.Core.AutoWhip.PendingReason != "" || cp.Core.AutoContinue.Episode != nil {
		return out, ErrRecoveryHeld
	}
	var plans int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_resume WHERE import_id=?`, req.ImportID).Scan(&plans); err != nil {
		return out, err
	}
	if plans > 0 && len(cp.Core.Holds) == 0 {
		return out, ErrRecoveryHeld
	}
	if len(cp.Core.Holds) != 0 {
		if len(cp.Core.Holds) != 1 || cp.Core.Holds[0].Kind != "headless_launch" {
			return out, ErrRecoveryHeld
		}
		var planRaw, effectRaw []byte
		var planHash, effectState string
		err = tx.QueryRowContext(ctx, `SELECT r.plan,r.plan_hash,e.body,e.state FROM private_migration_resume r JOIN private_effect e ON e.id=r.effect_id WHERE r.import_id=? AND r.effect_id<>''`, req.ImportID).Scan(&planRaw, &planHash, &effectRaw, &effectState)
		if err != nil {
			return out, err
		}
		var plan MigrationResumeIntent
		var launch state.HeadlessLaunchRecord
		if migrationSHA(planRaw) != planHash || json.Unmarshal(planRaw, &plan) != nil || json.Unmarshal(cp.Core.Holds[0].Frozen, &launch) != nil || effectState != "prepared" {
			return out, ErrJournalConflict
		}
		event, _, e := DecodeStoredEffect(effectRaw)
		if e != nil {
			return out, e
		}
		cmd := event.DaemonCommand
		if cmd == nil || cmd.Kind != control.DaemonCommandStartHeadless || cmd.InstanceID != launch.InstanceID || cmd.ThreadID != plan.ThreadID || launch.ThreadID != plan.ThreadID || cmd.ThreadCWD != plan.CWD || launch.ThreadCWD != plan.CWD || string(cmd.Backend) != plan.Backend {
			return out, ErrJournalConflict
		}
	}
	if err = verifyAppliedMigrationSources(ctx, tx, cp); err != nil {
		return out, err
	}
	var unresolved int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM dispatch WHERE tenant=?1 AND app_id=?2 AND gateway=?3 AND surface=?4 AND chat=?5 AND user_id=?6 AND generation=?7 AND state IN ('prepared','sending','unknown'))+(SELECT count(*) FROM private_effect WHERE tenant=?1 AND app_id=?2 AND gateway=?3 AND surface=?4 AND chat=?5 AND user_id=?6 AND generation=?7 AND state IN ('sending','unknown'))`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, req.Generation).Scan(&unresolved)
	if err != nil {
		return out, err
	}
	if unresolved != 0 {
		return out, ErrRecoveryHeld
	}
	validator := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, nil)
	if err = validator.RestorePrivateCheckpoint(cp.Core); err != nil {
		return out, err
	}
	out.Import = receipt
	out.CheckpointRevision = cp.Revision
	out.Cursor = cp.Cursor
	out.CheckpointHash = migrationSHA(raw)
	if err = tx.Commit(); err != nil {
		return MigrationReadyReceipt{}, err
	}
	return out, nil
}
