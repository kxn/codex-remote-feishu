package workstore

import (
	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// PersonalMigrationWitnessCandidates enumerates recorded SDK observations in
// source acceptance order. Callers must independently verify every candidate.
func (t *Tx) PersonalMigrationWitnessCandidates(scopeID string, after int64, limit int) ([]work.IngressObservation, int64, error) {
	if scopeID == "" || after < 0 || limit < 1 || limit > 1000 {
		return nil, after, ErrScopeDenied
	}
	rows, err := t.tx.QueryContext(t.ctx, `SELECT o.rowid,o.payload FROM raw_ingress r JOIN ingress_observations o ON o.ingress_id=r.id WHERE json_extract(r.payload,'$.route.scope_id')=? AND o.rowid>? ORDER BY o.rowid LIMIT ?`, scopeID, after, limit)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	var out []work.IngressObservation
	next := after
	for rows.Next() {
		var seq int64
		var raw []byte
		if err = rows.Scan(&seq, &raw); err != nil {
			return nil, after, err
		}
		var o work.IngressObservation
		if err = json.Unmarshal(raw, &o); err != nil {
			return nil, after, err
		}
		out = append(out, o)
		next = seq
	}
	return out, next, rows.Err()
}
