package executionowner

import (
	"context"
	"database/sql"
)

// BindRetiredReviewSession records an already-proven native child as inactive;
// it cannot create an execution lease or replace an existing owner.
func (r *Registry) BindRetiredReviewSession(ctx context.Context, reg Registration, evidence, session string) error {
	return r.withRetiredReviewSession(ctx, reg, evidence, session, true, func() error { return nil })
}

// WithRetiredReviewSession holds the retired ownership fence during a local
// projection, without granting the stopped carrier any send authority.
func (r *Registry) WithRetiredReviewSession(ctx context.Context, reg Registration, evidence, session string, fn func() error) error {
	return r.withRetiredReviewSession(ctx, reg, evidence, session, false, fn)
}

func (r *Registry) withRetiredReviewSession(ctx context.Context, reg Registration, evidence, session string, create bool, fn func() error) error {
	if reg.validate() != nil || !canonical(evidence) || !canonical(session) || fn == nil {
		return ErrConflict
	}
	f, err := r.beginLocalFence(ctx)
	if err != nil {
		return err
	}
	defer f.close()
	c, o := reg.Carrier, reg.Owner
	var count int
	err = f.tx.QueryRowContext(f.ctx, `SELECT count(*) FROM carriers c JOIN stop_receipts s ON s.incarnation=c.incarnation AND s.epoch=c.epoch WHERE c.instance_id=? AND c.incarnation=? AND c.backend=? AND c.namespace=? AND c.generation=? AND c.app=? AND c.owner_id=? AND c.epoch=? AND c.stopped=1 AND s.evidence_ref=? AND NOT EXISTS(SELECT 1 FROM transfers t WHERE t.incarnation=c.incarnation AND t.old_epoch=c.epoch)`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch, evidence).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	if create {
		_, err = f.tx.ExecContext(f.ctx, `INSERT INTO sessions(backend,namespace,session_id,incarnation,epoch,app,owner_id,active) VALUES(?,?,?,?,?,?,?,0) ON CONFLICT(backend,namespace,session_id) DO NOTHING`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID)
		if err != nil {
			return err
		}
	}
	err = f.tx.QueryRowContext(f.ctx, `SELECT count(*) FROM sessions WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND epoch=? AND app=? AND owner_id=? AND active=0 AND NOT EXISTS(SELECT 1 FROM session_reservations r WHERE r.backend=sessions.backend AND r.namespace=sessions.namespace AND r.session_id=sessions.session_id AND NOT (r.incarnation=? AND r.epoch=? AND r.app=? AND r.owner_id=?))`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID, c.Incarnation, o.Epoch, o.Application, o.ID).Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	if err = f.enter(); err != nil {
		return err
	}
	if err = fn(); err != nil {
		return err
	}
	return f.tx.Commit()
}
