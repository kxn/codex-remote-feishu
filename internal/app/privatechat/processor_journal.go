package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// processorIngress reads the immutable input and its applied marker, retaining
// source ordering within the input lane. Controls and menu actions can pass a
// held input; their lane admission and classification are trusted host duties.
func (j *Journal) processorIngress(ctx context.Context, o Owner, generation, seq int64) (JournalEntry, bool, int64, error) {
	entry := JournalEntry{Sequence: seq, Ingress: Ingress{Owner: o, Generation: generation}}
	var sources []byte
	err := j.db.QueryRowContext(ctx, `SELECT event_id,lane,payload,sources FROM ingress WHERE seq=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, seq, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&entry.Ingress.EventID, &entry.Ingress.Lane, &entry.Ingress.Payload, &sources)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, false, 0, ErrJournalState
	}
	if err != nil {
		return entry, false, 0, err
	}
	if err = json.Unmarshal(sources, &entry.Ingress.Sources); err != nil {
		return entry, false, 0, err
	}
	var revision int64
	err = j.db.QueryRowContext(ctx, `SELECT revision FROM private_applied WHERE seq=?`, seq).Scan(&revision)
	if err == nil {
		return entry, true, revision, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return entry, false, 0, err
	}
	if entry.Ingress.Lane == "input" {
		var earlier int
		if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM ingress i WHERE i.tenant=? AND i.app_id=? AND i.gateway=? AND i.surface=? AND i.chat=? AND i.user_id=? AND i.generation=? AND i.lane='input' AND i.seq<? AND NOT EXISTS(SELECT 1 FROM private_applied a WHERE a.seq=i.seq)`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, seq).Scan(&earlier); err != nil {
			return entry, false, 0, err
		}
		if earlier != 0 {
			return entry, false, 0, ErrJournalState
		}
	}
	return entry, false, 0, nil
}
func (j *Journal) processorEffects(ctx context.Context, o Owner, generation, revision int64) ([]EffectRecord, error) {
	rows, err := j.db.QueryContext(ctx, `SELECT id,revision,ordinal,lane,body,state,receipt FROM private_effect WHERE tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=? AND revision=? ORDER BY ordinal`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation, revision)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffectRecord
	for rows.Next() {
		var e EffectRecord
		var receipt []byte
		if err = rows.Scan(&e.Effect.ID, &e.Revision, &e.Ordinal, &e.Effect.Lane, &e.Effect.Body, &e.State, &receipt); err != nil {
			return nil, err
		}
		e.Receipt = receipt
		out = append(out, e)
	}
	return out, rows.Err()
}
