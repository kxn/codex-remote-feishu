package workstore

import (
	"encoding/json"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// HistoricalSessionCarriers returns carriers with a committed native turn or
// current worker session matching the exact native ID. Callers must validate
// each frozen launch's backend and history namespace separately.
func (t *Tx) HistoricalSessionCarriers(session string) ([]CarrierRegistration, error) {
	if session == "" {
		return nil, ErrConflict
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT DISTINCT c.payload FROM carrier_registrations c LEFT JOIN native_turns n ON n.carrier_incarnation=c.incarnation LEFT JOIN workers w ON w.id=c.worker_id WHERE n.session_id=? OR (json_extract(w.payload,'$.native_session_id')=? AND json_extract(w.payload,'$.carrier_incarnation')=c.incarnation)`, session, session)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CarrierRegistration
	for rows.Next() {
		var raw []byte
		var c CarrierRegistration
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		if c.Backend != work.BackendCodex && c.Backend != work.BackendClaude && c.Backend != work.BackendOpenCode {
			return nil, ErrConflict
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (t *Tx) HistoricalSessionIDs() ([]string, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT session_id FROM native_turns WHERE session_id<>'' UNION SELECT json_extract(payload,'$.native_session_id') FROM workers WHERE coalesce(json_extract(payload,'$.native_session_id'),'')<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
