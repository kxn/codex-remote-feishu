package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// GroupOwnerObservations lists real historical SDK observations for startup
// owner-witness import. Every candidate is independently revalidated.
func (t *Tx) GroupOwnerObservations(limit, offset int) ([]work.IngressObservation, error) {
	if limit < 1 || limit > 1000 || offset < 0 {
		return nil, ErrConflict
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT payload FROM ingress_observations WHERE json_extract(payload,'$.envelope.chat_type')='group' ORDER BY app_id,generation,event_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []work.IngressObservation
	for rows.Next() {
		var raw []byte
		var o work.IngressObservation
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (t *Tx) GroupOwnerObservation(app, event string, generation int64) (work.IngressRecord, work.IngressObservation, error) {
	var record work.IngressRecord
	var obs work.IngressObservation
	var id string
	var raw []byte
	err := t.tx.QueryRowContext(t.ctx, `SELECT ingress_id,payload FROM ingress_observations WHERE app_id=? AND generation=? AND event_id=?`, app, generation, event).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return record, obs, ErrNotFound
	}
	if err != nil {
		return record, obs, err
	}
	if err = json.Unmarshal(raw, &obs); err != nil {
		return record, obs, err
	}
	record, err = t.GetIngress(id)
	if err != nil {
		return record, obs, err
	}
	if record.Envelope.ChatType != "group" || obs.Envelope.ChatType != "group" {
		return work.IngressRecord{}, work.IngressObservation{}, ErrScopeDenied
	}
	return record, obs, nil
}
