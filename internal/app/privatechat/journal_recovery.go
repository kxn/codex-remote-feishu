package privatechat

import (
	"context"
	"encoding/json"
)

// RecoveryDispatch contains the frozen command and original source events.
// The evidence identifies the native request/event used by the verifier.
type RecoveryDispatch struct {
	Dispatch Dispatch
	State    string
	Inputs   []JournalEntry
	Receipt  DispatchReceipt
}

// ListDispatches returns a stable page ordered by dispatch ID for the exact
// owner epoch. Callers must inspect unknown/sending before enabling dispatch.
func (j *Journal) ListDispatches(ctx context.Context, o Owner, generation int64, after string, limit int) ([]RecoveryDispatch, error) {
	if !validOwner(o) || generation < 1 || limit < 1 || limit > 1000 {
		return nil, ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM dispatch WHERE gateway=? AND tenant=? AND app_id=? AND surface=? AND chat=? AND user_id=? AND generation=? AND id>? ORDER BY id LIMIT ?`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, generation, after, limit)
	if err != nil {
		return nil, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	var out []RecoveryDispatch
	for _, id := range ids {
		d, state, evidence, e := readDispatch(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		r := RecoveryDispatch{Dispatch: d, State: state, Receipt: DispatchReceipt{CommandID: d.CommandID, Session: d.Session, Evidence: evidence}}
		if e = tx.QueryRowContext(ctx, `SELECT request_id,event_sequence FROM dispatch WHERE id=?`, id).Scan(&r.Receipt.RequestID, &r.Receipt.EventSequence); e != nil {
			return nil, e
		}
		inputRows, e := tx.QueryContext(ctx, `SELECT ingress.seq,ingress.event_id,ingress.lane,ingress.payload,ingress.sources FROM ingress JOIN dispatch_input ON dispatch_input.seq=ingress.seq WHERE dispatch_input.dispatch_id=? ORDER BY ingress.seq`, id)
		if e != nil {
			return nil, e
		}
		for inputRows.Next() {
			entry := JournalEntry{Ingress: Ingress{Owner: o, Generation: generation}}
			var sources []byte
			if e = inputRows.Scan(&entry.Sequence, &entry.Ingress.EventID, &entry.Ingress.Lane, &entry.Ingress.Payload, &sources); e != nil {
				inputRows.Close()
				return nil, e
			}
			if e = json.Unmarshal(sources, &entry.Ingress.Sources); e != nil {
				inputRows.Close()
				return nil, e
			}
			r.Inputs = append(r.Inputs, entry)
		}
		e = inputRows.Err()
		inputRows.Close()
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
