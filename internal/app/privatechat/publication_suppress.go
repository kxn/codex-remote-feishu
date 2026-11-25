package privatechat

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

// SuppressPreparedPublication records a local decision not to send. The frozen
// operations remain prepared, and no platform receipt is invented.
func (j *Journal) SuppressPreparedPublication(ctx context.Context, o Owner, generation int64, id, reason string, at time.Time) error {
	if (reason != "stale_model_view" && reason != "obsolete_recovery_input") || at.IsZero() {
		return ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := publicationEffect(ctx, tx, o, generation, id)
	if err != nil {
		return err
	}
	if state == "confirmed" {
		var raw []byte
		var existing PublicationReceipt
		if err = tx.QueryRowContext(ctx, `SELECT receipt FROM private_effect WHERE id=?`, id).Scan(&raw); err != nil {
			return err
		}
		if json.Unmarshal(raw, &existing) != nil || existing.Version != 2 || existing.Kind != "suppressed" || existing.Reason != reason {
			return ErrJournalConflict
		}
		return tx.Commit()
	}
	if state != "prepared" && state != "sending" && !(reason == "obsolete_recovery_input" && state == "unknown") {
		return ErrJournalState
	}
	if reason == "obsolete_recovery_input" {
		var body []byte
		if err = tx.QueryRowContext(ctx, `SELECT body FROM private_effect WHERE id=?`, id).Scan(&body); err != nil {
			return err
		}
		event, e := DecodeEffectEvent(body)
		if e != nil || event.TimelineText == nil || event.TimelineText.Type != control.TimelineTextRecoveryInputWaiting || event.SourceMessageID == "" || event.TimelineText.ReplyToMessageID != event.SourceMessageID {
			return ErrJournalConflict
		}
	}
	pub, err := readPublication(ctx, tx, id)
	if err != nil {
		return err
	}
	for _, op := range pub.Operations {
		if op.State != "prepared" {
			return ErrJournalState
		}
	}
	receipt := PublicationReceipt{Version: 2, Kind: "suppressed", Reason: reason, EffectID: id, PlanHash: pub.PlanHash, Plan: pub.Plan, CompletedAt: at}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE private_effect SET state='confirmed',receipt=? WHERE id=?`, raw, id); err != nil {
		return err
	}
	return tx.Commit()
}
