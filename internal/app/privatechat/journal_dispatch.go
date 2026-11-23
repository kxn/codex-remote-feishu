package privatechat

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

type NativeSession struct {
	Backend, Namespace, SessionID, CarrierID string
	CarrierGeneration                        int64
}
type Dispatch struct {
	ID             string
	Lane           string
	Mode           string // start creates a session; resume targets SessionID; none is owner-only
	Owner          Owner
	Generation     int64
	Session        NativeSession
	CommandID      string
	InputSequences []int64
}
type DispatchReceipt struct {
	CommandID     string
	Session       NativeSession
	Evidence      json.RawMessage
	RequestID     string
	EventSequence int64
}

// PrepareDispatch freezes the exact native identity and ordered input cluster.
// Input sequences must identify the exact frozen source cluster in ascending
// order; the selected cluster need not be at the front of the queue.
func prepareDispatchTx(ctx context.Context, tx *sql.Tx, d Dispatch) error {
	return prepareDispatchWithCauseTx(ctx, tx, d, false)
}

// committedEffect permits effect-derived control/automatic commands with no SDK
// input, and selected message sources whose original SDK lane was control.
// Only the effect reader may use this after validating its durable provenance.
func prepareDispatchWithCauseTx(ctx context.Context, tx *sql.Tx, d Dispatch, committedEffect bool) error {
	s := d.Session
	if d.Lane == "" {
		d.Lane = "input"
	}
	if strings.TrimSpace(d.ID) == "" || !validOwner(d.Owner) || d.Generation < 1 || strings.TrimSpace(d.CommandID) == "" || strings.TrimSpace(s.Backend) == "" || strings.TrimSpace(s.Namespace) == "" || strings.TrimSpace(s.CarrierID) == "" || s.CarrierGeneration < 1 || (len(d.InputSequences) == 0 && d.Lane != "inspect" && !committedEffect) || (d.Mode != "start" && d.Mode != "resume" && !(committedEffect && d.Mode == "none")) || ((d.Mode == "start" || d.Mode == "none") && s.SessionID != "") || (d.Mode == "resume" && strings.TrimSpace(s.SessionID) == "") {
		return ErrJournalState
	}
	if d.Lane != "input" && d.Lane != "control" && d.Lane != "inspect" {
		return ErrJournalState
	}
	previous, _, _, err := readDispatch(ctx, tx, d.ID)
	if err == nil {
		if previous.Mode == "start" {
			previous.Session.SessionID = ""
		}
		if reflect.DeepEqual(previous, d) {
			return nil
		}
		return ErrJournalConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	o := d.Owner
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM dispatch WHERE gateway=? AND tenant=? AND app_id=? AND surface=? AND chat=? AND user_id=? AND generation=? AND lane=? AND state IN ('prepared','sending','unknown')`, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, d.Generation, d.Lane).Scan(&active); err != nil {
		return err
	}
	if active != 0 && d.Lane != "inspect" {
		return ErrJournalState
	}
	seqs := d.InputSequences
	for i, seq := range seqs {
		if seq <= 0 || i > 0 && seq <= seqs[i-1] {
			return ErrJournalState
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ingress WHERE seq=? AND gateway=? AND tenant=? AND app_id=? AND surface=? AND chat=? AND user_id=? AND generation=? AND (? OR lane=?) AND NOT EXISTS (SELECT 1 FROM dispatch_input JOIN dispatch ON dispatch.id=dispatch_input.dispatch_id WHERE dispatch_input.seq=ingress.seq AND dispatch.state<>'failed')`, seq, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, d.Generation, committedEffect, d.Lane).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrJournalState
		}
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'prepared')`, d.ID, o.GatewayID, o.TenantID, o.AppID, o.SurfaceID, o.ChatID, o.UserID, d.Generation, d.Lane, d.Mode, s.Backend, s.Namespace, s.SessionID, s.CarrierID, s.CarrierGeneration, d.CommandID)
	if err != nil {
		return err
	}
	for _, seq := range seqs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO dispatch_input(dispatch_id,seq) VALUES(?,?)`, d.ID, seq); err != nil {
			return err
		}
	}
	return nil
}

func readDispatch(ctx context.Context, tx *sql.Tx, id string) (Dispatch, string, []byte, error) {
	var d Dispatch
	var state string
	var evidence []byte
	d.ID = id
	err := tx.QueryRowContext(ctx, `SELECT gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state,evidence FROM dispatch WHERE id=?`, id).Scan(&d.Owner.GatewayID, &d.Owner.TenantID, &d.Owner.AppID, &d.Owner.SurfaceID, &d.Owner.ChatID, &d.Owner.UserID, &d.Generation, &d.Lane, &d.Mode, &d.Session.Backend, &d.Session.Namespace, &d.Session.SessionID, &d.Session.CarrierID, &d.Session.CarrierGeneration, &d.CommandID, &state, &evidence)
	if err != nil {
		return d, state, nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT seq FROM dispatch_input WHERE dispatch_id=? ORDER BY seq`, id)
	if err != nil {
		return d, state, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		if err = rows.Scan(&seq); err != nil {
			return d, state, nil, err
		}
		d.InputSequences = append(d.InputSequences, seq)
	}
	return d, state, evidence, rows.Err()
}

func (j *Journal) DispatchState(ctx context.Context, id string) (string, error) {
	var state string
	err := j.db.QueryRowContext(ctx, `SELECT state FROM dispatch WHERE id=?`, id).Scan(&state)
	return state, err
}
