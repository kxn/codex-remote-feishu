package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// RecordStoppedPreparedUnsent is called only after the host has joined its
// send workers and fenced this exact carrier. Prepared means MarkSending has
// never committed; sending and unknown are never treated as unsent.
func (l *NativeLease) RecordStoppedPreparedUnsent(ctx context.Context, effectID string) (bool, error) {
	var recorded bool
	err := l.with(ctx, func() error {
		tx, e := l.journal.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var body []byte
		var status, lane string
		o := l.surface
		e = tx.QueryRowContext(ctx, `SELECT body,state,lane FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, effectID, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, l.generation).Scan(&body, &status, &lane)
		if e != nil {
			return e
		}
		if status != "prepared" || lane != "background" {
			return nil
		}
		event, _, target, e := DecodeStoredEffectTarget(body)
		if e != nil {
			return e
		}
		if target == nil || *target != l.native.InstanceID || event.Command == nil || event.Command.Kind != agentproto.CommandPromptSend || event.Command.CommandID == "" {
			return nil
		}
		dispatchID := effectID + ":native"
		d, dispatchState, _, e := readDispatch(ctx, tx, dispatchID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			if d.Owner != o || d.Generation != l.generation || d.Session.CarrierID != l.native.CarrierIncarnation || d.CommandID != event.Command.CommandID {
				return ErrJournalConflict
			}
			if dispatchState != "prepared" {
				return nil
			}
			var raw []byte
			var storedHash string
			if e = tx.QueryRowContext(ctx, `SELECT payload,payload_hash FROM native_commands WHERE dispatch_id=?`, dispatchID).Scan(&raw, &storedHash); e != nil {
				return e
			}
			hash := sha256.Sum256(raw)
			var command agentproto.Command
			if hex.EncodeToString(hash[:]) != storedHash || json.Unmarshal(raw, &command) != nil || command.NativeBinding == nil || command.NativeBinding.Owner != l.native {
				return ErrJournalConflict
			}
			command.NativeBinding = nil
			if !reflect.DeepEqual(command, *event.Command) {
				return ErrJournalConflict
			}
			if _, e = tx.ExecContext(ctx, `UPDATE dispatch SET state='failed' WHERE id=? AND state='prepared'`, dispatchID); e != nil {
				return e
			}
		} else {
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM native_commands WHERE command_id=?`, event.Command.CommandID).Scan(&count); e != nil {
				return e
			}
			if count != 0 {
				return ErrJournalConflict
			}
		}
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM native_events WHERE incarnation=? AND command_id=?`, l.native.CarrierIncarnation, event.Command.CommandID).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return ErrJournalConflict
		}
		receipt, _ := json.Marshal(KnownUnsentReceipt{Version: 1, EffectID: effectID, CommandID: event.Command.CommandID, InstanceID: l.native.InstanceID, Reason: "shutdown_prepared_unstarted"})
		if _, e = tx.ExecContext(ctx, `INSERT INTO private_local_receipts(effect_id,receipt) VALUES(?,?)`, effectID, receipt); e != nil {
			return e
		}
		result, e := tx.ExecContext(ctx, `UPDATE private_effect SET state='confirmed',receipt=? WHERE id=? AND state='prepared'`, receipt, effectID)
		if e != nil {
			return e
		}
		changed, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if changed != 1 {
			return ErrJournalConflict
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		recorded = true
		return nil
	})
	return recorded, err
}

type ShutdownKnownUnsentReducer struct{}

func (ShutdownKnownUnsentReducer) ReduceHostReceipt(app *App, effect Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	event, _, target, err := DecodeStoredEffectTarget(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt KnownUnsentReceipt
	if json.Unmarshal(raw, &receipt) != nil || receipt.Version != 1 || receipt.Reason != "shutdown_prepared_unstarted" || receipt.EffectID != effect.ID || event.Command == nil || event.Command.Kind != agentproto.CommandPromptSend || event.Command.CommandID != receipt.CommandID || target == nil || *target != receipt.InstanceID {
		return nil, ErrJournalConflict
	}
	if err := app.Service().RestorePrivateNativeProjection(event.SurfaceSessionID, receipt.InstanceID, receipt.CommandID, event.Command.Target.ThreadID, event.Command.Target.TurnID); err != nil {
		return nil, err
	}
	events := app.Service().HandleKnownUnsent(event.SurfaceSessionID, receipt.CommandID, nil)
	filtered := events[:0]
	for _, e := range events {
		if e.Notice == nil || e.Notice.Code != "native_command_not_sent" {
			filtered = append(filtered, e)
		}
	}
	return filtered, nil
}
