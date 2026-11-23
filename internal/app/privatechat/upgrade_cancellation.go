package privatechat

import (
	"context"
	"database/sql"
	"errors"
)

const upgradeCancellationSchema = `CREATE TABLE IF NOT EXISTS private_upgrade_cancellations (tenant TEXT NOT NULL,app_id TEXT NOT NULL,gateway TEXT NOT NULL,surface TEXT NOT NULL,chat TEXT NOT NULL,user_id TEXT NOT NULL,generation INTEGER NOT NULL,flow_id TEXT NOT NULL,cancel_effect TEXT NOT NULL REFERENCES private_effect(id),PRIMARY KEY(tenant,app_id,gateway,surface,chat,user_id,generation,flow_id));`

var ErrUpgradeCancelled = errors.New("private upgrade cancelled before execution")

// RecordUpgradeCancellation persists the verified owner's cancellation before
// any signal or UI update. Replays must identify the same committed effect.
func (j *Journal) RecordUpgradeCancellation(ctx context.Context, o Owner, generation int64, flowID, effectID string) error {
	if !validOwner(o) || generation < 1 || flowID == "" || effectID == "" {
		return ErrJournalState
	}
	tx, e := j.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state IN ('sending','unknown','confirmed') AND json_extract(body,'$.event.DaemonCommand.Kind')='upgrade.owner_flow' AND json_extract(body,'$.event.DaemonCommand.PickerID')=? AND json_extract(body,'$.event.DaemonCommand.OptionID')='cancel'`, effectID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, flowID).Scan(&n)
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrJournalConflict
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO private_upgrade_cancellations(tenant,app_id,gateway,surface,chat,user_id,generation,flow_id,cancel_effect) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, flowID, effectID)
	if e != nil {
		return e
	}
	var saved string
	e = tx.QueryRowContext(ctx, `SELECT cancel_effect FROM private_upgrade_cancellations WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND flow_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, flowID).Scan(&saved)
	if e != nil {
		return e
	}
	if saved != effectID {
		return ErrJournalConflict
	}
	return tx.Commit()
}
func (j *Journal) UpgradeCancelled(ctx context.Context, o Owner, generation int64, flowID string) (bool, error) {
	var n int
	e := j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_upgrade_cancellations WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND flow_id=?`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, flowID).Scan(&n)
	return n != 0, e
}

// MarkUpgradeExecutionSending atomically competes with cancellation. A
// cancellation committed first prevents any external upgrade operation.
func (j *Journal) MarkUpgradeExecutionSending(ctx context.Context, o Owner, generation int64, id, flowID string) error {
	result, e := j.db.ExecContext(ctx, `UPDATE private_effect SET state='sending' WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND state='prepared' AND NOT EXISTS(SELECT 1 FROM private_upgrade_cancellations c WHERE c.tenant=? AND c.app_id=? AND c.gateway=? AND c.surface=? AND c.chat=? AND c.user_id=? AND c.generation=? AND c.flow_id=?)`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, flowID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n == 1 {
		return nil
	}
	cancelled, e := j.UpgradeCancelled(ctx, o, generation, flowID)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if cancelled {
		return ErrUpgradeCancelled
	}
	return ErrJournalState
}
