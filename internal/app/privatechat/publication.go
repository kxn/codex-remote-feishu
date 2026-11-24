package privatechat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

const publicationSchema = `
 CREATE TABLE IF NOT EXISTS private_publications (effect_id TEXT PRIMARY KEY REFERENCES private_effect(id), plan BLOB NOT NULL, plan_hash TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS private_publication_ops (effect_id TEXT NOT NULL REFERENCES private_publications(effect_id), ordinal INTEGER NOT NULL, body BLOB NOT NULL, state TEXT NOT NULL CHECK(state IN ('prepared','sending','confirmed','unknown')), receipt BLOB, PRIMARY KEY(effect_id,ordinal));
 UPDATE private_publication_ops SET state='unknown' WHERE state='sending';
`

type PublicationResult struct {
	Ordinal   int
	Operation json.RawMessage
	Receipt   json.RawMessage
}
type PublicationReceipt struct {
	Version            int
	Kind, Reason       string `json:",omitempty"`
	EffectID, PlanHash string
	CompletedAt        time.Time
	Results            []PublicationResult
	Plan               json.RawMessage
}
type PublicationOperation struct {
	Ordinal int
	Body    json.RawMessage
	State   string
	Receipt json.RawMessage
}
type Publication struct {
	EffectState string

	EffectID, PlanHash string
	Plan               json.RawMessage
	Operations         []PublicationOperation
}

func publicationEffect(ctx context.Context, tx *sql.Tx, o Owner, generation int64, id string) (string, error) {
	if !validOwner(o) || generation < 1 || id == "" {
		return "", ErrJournalState
	}
	var state, lane string
	err := tx.QueryRowContext(ctx, `SELECT state,lane FROM private_effect WHERE id=? AND tenant=? AND app_id=? AND gateway=? AND surface=? AND chat=? AND user_id=? AND generation=?`, id, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID, generation).Scan(&state, &lane)
	if err != nil {
		return "", err
	}
	if lane != "interactive" {
		return "", ErrJournalState
	}
	return state, nil
}

// PreparePublication freezes actual rendered operations before any platform
// writes. A restart reads this plan instead of rerendering current core state.
func (j *Journal) PreparePublication(ctx context.Context, o Owner, generation int64, id string, plan json.RawMessage, ops []json.RawMessage) error {
	if !json.Valid(plan) {
		return ErrJournalState
	}
	for _, op := range ops {
		if !json.Valid(op) {
			return ErrJournalState
		}
	}
	identity, err := json.Marshal(struct {
		Plan       json.RawMessage
		Operations []json.RawMessage
	}{plan, ops})
	if err != nil {
		return err
	}
	h := sha256.Sum256(identity)
	hash := hex.EncodeToString(h[:])
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := publicationEffect(ctx, tx, o, generation, id)
	if err != nil {
		return err
	}
	var saved string
	err = tx.QueryRowContext(ctx, `SELECT plan_hash FROM private_publications WHERE effect_id=?`, id).Scan(&saved)
	if err == nil {
		if saved != hash {
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
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_publications(effect_id,plan,plan_hash) VALUES(?,?,?)`, id, []byte(plan), hash); err != nil {
		return err
	}
	for i, op := range ops {
		if _, err = tx.ExecContext(ctx, `INSERT INTO private_publication_ops(effect_id,ordinal,body,state) VALUES(?,?,?,'prepared')`, id, i, []byte(op)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (j *Journal) LoadPublication(ctx context.Context, o Owner, generation int64, id string) (Publication, error) {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return Publication{}, err
	}
	defer tx.Rollback()
	effectState, err := publicationEffect(ctx, tx, o, generation, id)
	if err != nil {
		return Publication{}, err
	}
	p, err := readPublication(ctx, tx, id)
	if err != nil {
		return p, err
	}
	p.EffectState = effectState
	return p, tx.Commit()
}
func readPublication(ctx context.Context, tx *sql.Tx, id string) (Publication, error) {
	p := Publication{EffectID: id}
	if err := tx.QueryRowContext(ctx, `SELECT plan,plan_hash FROM private_publications WHERE effect_id=?`, id).Scan(&p.Plan, &p.PlanHash); err != nil {
		return p, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT ordinal,body,state,receipt FROM private_publication_ops WHERE effect_id=? ORDER BY ordinal`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var op PublicationOperation
		var receipt []byte
		if err = rows.Scan(&op.Ordinal, &op.Body, &op.State, &receipt); err != nil {
			return p, err
		}
		op.Receipt = receipt
		p.Operations = append(p.Operations, op)
	}
	return p, rows.Err()
}

func (j *Journal) BeginPublicationOperation(ctx context.Context, o Owner, generation int64, id string, ordinal int) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := publicationEffect(ctx, tx, o, generation, id)
	if err != nil {
		return err
	}
	if state != "sending" {
		return ErrJournalState
	}
	r, err := tx.ExecContext(ctx, `UPDATE private_publication_ops SET state='sending' WHERE effect_id=? AND ordinal=? AND state='prepared' AND NOT EXISTS(SELECT 1 FROM private_publication_ops prior WHERE prior.effect_id=? AND prior.ordinal<? AND prior.state<>'confirmed')`, id, ordinal, id, ordinal)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrJournalState
	}
	return tx.Commit()
}

// ResumePublication authorizes only the unstarted tail of a frozen publication.
// The generic effect journal conservatively marks in-flight effects unknown on
// restart; this more precise operation journal can prove that no call is lost.
func (j *Journal) ResumePublication(ctx context.Context, o Owner, generation int64, id string) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	state, err := publicationEffect(ctx, tx, o, generation, id)
	if err != nil {
		return err
	}
	if state != "sending" && state != "unknown" {
		return ErrJournalState
	}
	var plans, uncertain int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM private_publications WHERE effect_id=?`, id).Scan(&plans); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM private_publication_ops WHERE effect_id=? AND state NOT IN ('prepared','confirmed')`, id).Scan(&uncertain); err != nil {
		return err
	}
	if plans != 1 || uncertain != 0 {
		return ErrJournalState
	}
	if _, err = tx.ExecContext(ctx, `UPDATE private_effect SET state='sending' WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// PublicationResultVerifier is a trusted platform adapter. It validates the
// actual successful call result against the frozen operation, not caller JSON.
type PublicationResultVerifier interface {
	VerifyPublicationResult(context.Context, Owner, int64, string, int, json.RawMessage, json.RawMessage) error
}

func (j *Journal) CompletePublicationOperation(ctx context.Context, o Owner, generation int64, id string, ordinal int, outcome string, receipt json.RawMessage, v PublicationResultVerifier) error {
	if (outcome != "confirmed" && outcome != "unknown") || !json.Valid(receipt) || string(receipt) == "null" {
		return ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = publicationEffect(ctx, tx, o, generation, id); err != nil {
		return err
	}
	var body, old []byte
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT body,state,receipt FROM private_publication_ops WHERE effect_id=? AND ordinal=?`, id, ordinal).Scan(&body, &state, &old); err != nil {
		return err
	}
	if state == outcome && string(old) == string(receipt) {
		return tx.Commit()
	}
	if state != "sending" && !(state == "unknown" && outcome == "confirmed") {
		return ErrJournalState
	}
	if outcome == "confirmed" {
		if v == nil {
			return ErrJournalState
		}
		if err = v.VerifyPublicationResult(ctx, o, generation, id, ordinal, body, receipt); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE private_publication_ops SET state=?,receipt=? WHERE effect_id=? AND ordinal=?`, outcome, []byte(receipt), id, ordinal); err != nil {
		return err
	}
	return tx.Commit()
}

// FinishPublication derives the effect completion solely from verified,
// persisted operation results. A partially sent batch is never replayed whole.
func (j *Journal) FinishPublication(ctx context.Context, o Owner, generation int64, id string, at time.Time) (PublicationReceipt, error) {
	var receipt PublicationReceipt
	if at.IsZero() {
		return receipt, ErrJournalState
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return receipt, err
	}
	defer tx.Rollback()
	state, err := publicationEffect(ctx, tx, o, generation, id)
	if err != nil {
		return receipt, err
	}
	if state == "confirmed" {
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT receipt FROM private_effect WHERE id=?`, id).Scan(&raw)
		if err == nil {
			err = json.Unmarshal(raw, &receipt)
		}
		return receipt, err
	}
	if state != "sending" && state != "unknown" {
		return receipt, ErrJournalState
	}
	p, err := readPublication(ctx, tx, id)
	if err != nil {
		return receipt, err
	}
	receipt = PublicationReceipt{Version: 1, EffectID: id, PlanHash: p.PlanHash, Plan: p.Plan, CompletedAt: at}
	for _, op := range p.Operations {
		if op.State != "confirmed" {
			return PublicationReceipt{}, ErrJournalState
		}
		receipt.Results = append(receipt.Results, PublicationResult{op.Ordinal, op.Body, op.Receipt})
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return PublicationReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE private_effect SET state='confirmed',receipt=? WHERE id=?`, raw, id); err != nil {
		return PublicationReceipt{}, err
	}
	return receipt, tx.Commit()
}
