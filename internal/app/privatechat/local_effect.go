package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// PrepareLocalIntent freezes filesystem observation before the external write.
func (j *Journal) PrepareLocalIntent(ctx context.Context, o Owner, generation int64, id string, body json.RawMessage) error {
	if !validOwner(o) || generation < 1 || id == "" || !json.Valid(body) {
		return ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&state); err != nil {
		return err
	}
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM private_local_intents WHERE effect_id=?`, id).Scan(&old)
	if err == nil {
		if string(old) != string(body) {
			return ErrJournalConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if state != "prepared" {
		return ErrJournalState
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_local_intents(effect_id,body) VALUES(?,?)`, id, []byte(body)); err != nil {
		return err
	}
	return tx.Commit()
}

func (j *Journal) LoadLocalIntent(ctx context.Context, o Owner, generation int64, id string) (json.RawMessage, error) {
	if !validOwner(o) || generation < 1 || id == "" {
		return nil, ErrJournalState
	}
	var body []byte
	err := j.db.QueryRowContext(ctx, `SELECT i.body FROM private_local_intents i JOIN private_effect e ON e.id=i.effect_id WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&body)
	return body, err
}

// LoadEffect returns an exact owner-scoped effect for the host executor.
func (j *Journal) LoadEffect(ctx context.Context, o Owner, generation int64, id string) (EffectRecord, error) {
	var e EffectRecord
	e.Effect.ID = id
	if !validOwner(o) || generation < 1 || id == "" {
		return e, ErrJournalState
	}
	var receipt []byte
	err := j.db.QueryRowContext(ctx, `SELECT revision,ordinal,lane,body,state,receipt FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&e.Revision, &e.Ordinal, &e.Effect.Lane, &e.Effect.Body, &e.State, &receipt)
	e.Receipt = receipt
	return e, err
}

// IngressSourcesForEffect returns only the accepted source IDs which produced
// this effect's exact ingress revision; references are not producers.
func (j *Journal) IngressSourcesForEffect(ctx context.Context, o Owner, generation int64, id string) ([]string, error) {
	if !validOwner(o) || generation < 1 || id == "" {
		return nil, ErrJournalState
	}
	var raw []byte
	err := j.db.QueryRowContext(ctx, `SELECT i.sources FROM private_effect e JOIN private_applied a ON a.revision=e.revision JOIN ingress i ON i.seq=a.seq WHERE e.id=? AND e.tenant=? AND e.app_id=? AND e.gateway=? AND e.surface=? AND e.chat=? AND e.user_id=? AND e.generation=? AND i.tenant=e.tenant AND i.app_id=e.app_id AND i.gateway=e.gateway AND i.surface=e.surface AND i.chat=e.chat AND i.user_id=e.user_id AND i.generation=e.generation`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var sources []string
	if err = json.Unmarshal(raw, &sources); err != nil {
		return nil, err
	}
	return sources, nil
}

// ConfirmLocalEffect records an independently verified local result and its
// effect state in one transaction. Unknown effects require exact reconciliation.
func (j *Journal) ConfirmLocalEffect(ctx context.Context, o Owner, generation int64, id string, receipt json.RawMessage, verifier EffectReceiptVerifier) error {
	if !validOwner(o) || generation < 1 || id == "" || !json.Valid(receipt) || string(receipt) == "null" || verifier == nil {
		return ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var effect Effect
	effect.ID = id
	var state string
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT lane,body,state,receipt FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&effect.Lane, &effect.Body, &state, &old)
	if err != nil {
		return err
	}
	if state == "confirmed" && string(old) == string(receipt) {
		var saved []byte
		if e := tx.QueryRowContext(ctx, `SELECT receipt FROM private_local_receipts WHERE effect_id=?`, id).Scan(&saved); e != nil || string(saved) != string(receipt) {
			return ErrJournalConflict
		}
		return tx.Commit()
	}
	if effect.Lane != "background" || (state != "sending" && state != "unknown") {
		return ErrJournalState
	}
	if err = verifier.VerifyEffect(ctx, o, generation, effect, receipt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_local_receipts(effect_id,receipt) VALUES(?,?)`, id, []byte(receipt)); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE private_effect SET state='confirmed',receipt=? WHERE id=?`, []byte(receipt), id); err != nil {
		return err
	}
	return tx.Commit()
}
