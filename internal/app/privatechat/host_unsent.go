package privatechat

import (
	"context"
	"encoding/json"
)

// KnownUnsentReceipt is written only by the trusted daemon transport after it
// receives proof that its socket-send callback was never invoked.
type KnownUnsentReceipt struct {
	Version                                 int
	EffectID, CommandID, InstanceID, Reason string
}

// RecordKnownUnsent is a trusted host port, not an SDK or native event API. The
// caller must have an actual outbound BeforeSendError for this exact call.
// Dispatch failure and local effect receipt commit in the same transaction.
func (l *NativeLease) RecordKnownUnsent(ctx context.Context, effectID, dispatchID, reason string) error {
	if l == nil || effectID == "" || dispatchID != effectID+":native" || reason == "" {
		return ErrJournalState
	}
	return l.with(ctx, func() error {
		tx, err := l.journal.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		d, state, _, err := readDispatch(ctx, tx, dispatchID)
		if err != nil {
			return err
		}
		if d.Owner != l.surface || d.Generation != l.generation || d.Session.CarrierID != l.native.CarrierIncarnation || (state != "prepared" && state != "sending") {
			return ErrJournalConflict
		}
		var body []byte
		var effectState string
		err = tx.QueryRowContext(ctx, `SELECT body,state FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, effectID, l.surface.TenantID, l.surface.AppID, l.surface.GatewayID, l.surface.SurfaceID, l.surface.ChatID, l.surface.UserID, l.generation).Scan(&body, &effectState)
		if err != nil {
			return err
		}
		event, _, target, err := DecodeStoredEffectTarget(body)
		if err != nil {
			return err
		}
		if target == nil || *target != l.native.InstanceID || event.Command == nil || event.Command.CommandID != d.CommandID || (effectState != "prepared" && effectState != "sending") {
			return ErrJournalConflict
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND command_id=?`, l.native.CarrierIncarnation, d.CommandID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrJournalConflict
		}
		receipt, _ := json.Marshal(KnownUnsentReceipt{Version: 1, EffectID: effectID, CommandID: d.CommandID, InstanceID: l.native.InstanceID, Reason: reason})
		if _, err = tx.ExecContext(ctx, `UPDATE dispatch SET state='failed',evidence=? WHERE id=?`, receipt, dispatchID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_local_receipts(effect_id,receipt) VALUES(?,?)`, effectID, receipt); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE private_effect SET state='confirmed',receipt=? WHERE id=?`, receipt, effectID); err != nil {
			return err
		}
		return tx.Commit()
	})
}
