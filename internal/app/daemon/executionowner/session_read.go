package executionowner

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const readIntentSchema = `CREATE TABLE IF NOT EXISTS session_read_intents (
 backend TEXT NOT NULL, namespace TEXT NOT NULL, session_id TEXT NOT NULL,
 incarnation TEXT NOT NULL, epoch INTEGER NOT NULL, app TEXT NOT NULL, owner_id TEXT NOT NULL,
 operation_id TEXT NOT NULL, expires_ns INTEGER NOT NULL,
 PRIMARY KEY(backend,namespace,session_id,incarnation,epoch,operation_id));`

// ErrReadIntentExpired means the original read may already have been sent;
// it must not be converted into a known-unsent receipt or restarted.
var ErrReadIntentExpired = errors.New("session read intent expired")

func checkForeignReadIntents(ctx context.Context, tx *sql.Tx, reg Registration, session string) error {
	c, o := reg.Carrier, reg.Owner
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM session_read_intents i JOIN carriers carrier ON carrier.incarnation=i.incarnation AND carrier.epoch=i.epoch WHERE i.backend=? AND i.namespace=? AND i.session_id=? AND i.expires_ns>? AND carrier.stopped=0 AND NOT (i.incarnation=? AND i.epoch=? AND i.app=? AND i.owner_id=?)`, c.Backend, c.Namespace, session, time.Now().UnixNano(), c.Incarnation, o.Epoch, o.Application, o.ID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrConflict
	}
	return nil
}

// BeginSessionRead freezes an operation-specific bounded read authority. Replays
// retain the original expiry and cannot extend an unknown in-flight operation.
func (l *Lease) BeginSessionRead(reg Registration, session, operation string, expires time.Time) error {
	if !canonical(operation) {
		return ErrConflict
	}
	if !expires.After(time.Now()) {
		return ErrReadIntentExpired
	}
	if l == nil {
		return ErrConflict
	}
	l.mu.Lock()
	if !l.open || l.registration != reg || l.tx == nil {
		l.mu.Unlock()
		return ErrConflict
	}
	c, o := reg.Carrier, reg.Owner
	var prior int64
	err := l.tx.QueryRowContext(l.ctx, `SELECT expires_ns FROM session_read_intents WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND epoch=? AND app=? AND owner_id=? AND operation_id=?`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID, operation).Scan(&prior)
	l.mu.Unlock()
	if err == nil && prior <= time.Now().UnixNano() {
		return ErrReadIntentExpired
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err := l.CheckSessionRead(reg, session); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil {
		return ErrConflict
	}
	if err := checkForeignReadIntents(l.ctx, l.tx, reg, session); err != nil {
		return err
	}
	_, err = l.tx.ExecContext(l.ctx, `INSERT INTO session_read_intents(backend,namespace,session_id,incarnation,epoch,app,owner_id,operation_id,expires_ns) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID, operation, expires.UnixNano())
	if err != nil {
		return err
	}
	var frozen int64
	if err = l.tx.QueryRowContext(l.ctx, `SELECT expires_ns FROM session_read_intents WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND epoch=? AND app=? AND owner_id=? AND operation_id=?`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID, operation).Scan(&frozen); err != nil {
		return err
	}
	if frozen <= time.Now().UnixNano() {
		return ErrReadIntentExpired
	}
	return nil
}

// FinishSessionRead only releases the matching operation, never a write reservation.
func (l *Lease) FinishSessionRead(reg Registration, session, operation string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil || !canonical(session) || !canonical(operation) {
		return ErrConflict
	}
	c, o := reg.Carrier, reg.Owner
	_, err := l.tx.ExecContext(l.ctx, `DELETE FROM session_read_intents WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND epoch=? AND app=? AND owner_id=? AND operation_id=?`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID, operation)
	return err
}

// ExpiredSessionRead returns the original deadline only once it has elapsed.
// The row remains as evidence for late-result revalidation and retirement.
func (l *Lease) ExpiredSessionRead(reg Registration, session, operation string) (int64, error) {
	expiry, err := l.SessionReadDeadline(reg, session, operation)
	if err != nil || expiry == 0 || expiry > time.Now().UnixNano() {
		return 0, err
	}
	return expiry, nil
}

func (l *Lease) SessionReadDeadline(reg Registration, session, operation string) (int64, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil || !canonical(session) || !canonical(operation) {
		return 0, ErrConflict
	}
	c, o := reg.Carrier, reg.Owner
	var expiry int64
	err := l.tx.QueryRowContext(l.ctx, `SELECT expires_ns FROM session_read_intents WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND epoch=? AND app=? AND owner_id=? AND operation_id=?`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID, operation).Scan(&expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return expiry, nil
}

// WithRetiredSessionRead permits only local projection under an existing exact
// retirement proof; it grants no send authority to the stopped carrier.
func (r *Registry) WithRetiredSessionRead(ctx context.Context, reg Registration, evidence, session, operation string, fn func(bool) error) error {
	if fn == nil {
		return ErrConflict
	}
	return r.withRetiredRead(ctx, reg, evidence, session, operation, func(l *Lease) error {
		if session == "" {
			return fn(true)
		}
		err := l.CheckSessionRead(reg, session)
		if err != nil && !errors.Is(err, ErrConflict) {
			return err
		}
		if err = fn(err == nil); err != nil {
			return err
		}
		return l.FinishSessionRead(reg, session, operation)
	})
}

// WithRetiredReadExpiration verifies the same stop proof but retains the row
// until a local observation receipt has actually been recorded.
func (r *Registry) WithRetiredReadExpiration(ctx context.Context, reg Registration, evidence, session, operation string, deadline int64, fn func(int64) error) error {
	if fn == nil {
		return ErrConflict
	}
	return r.withRetiredRead(ctx, reg, evidence, session, operation, func(l *Lease) error {
		var expires int64
		var err error
		if session != "" {
			expires, err = l.SessionReadDeadline(reg, session, operation)
		}
		if err != nil {
			return err
		}
		if expires != 0 && expires != deadline {
			return ErrConflict
		}
		if deadline <= 0 || deadline > time.Now().UnixNano() {
			return nil
		}
		if err = fn(deadline); err != nil {
			return err
		}
		if session != "" {
			return l.FinishSessionRead(reg, session, operation)
		}
		return nil
	})
}

func (r *Registry) withRetiredRead(ctx context.Context, reg Registration, evidence, session, operation string, fn func(*Lease) error) error {
	if reg.validate() != nil || !canonical(evidence) || (session != "" && !canonical(session)) || !canonical(operation) || fn == nil {
		return ErrConflict
	}
	f, err := r.beginLocalFence(ctx)
	if err != nil {
		return err
	}
	defer f.close()
	c, o := reg.Carrier, reg.Owner
	var n int
	err = f.tx.QueryRowContext(f.ctx, `SELECT count(*) FROM carriers c JOIN stop_receipts s ON s.incarnation=c.incarnation AND s.epoch=c.epoch WHERE c.instance_id=? AND c.incarnation=? AND c.backend=? AND c.namespace=? AND c.generation=? AND c.app=? AND c.owner_id=? AND c.epoch=? AND c.stopped=1 AND s.evidence_ref=? AND NOT EXISTS(SELECT 1 FROM transfers t WHERE t.incarnation=c.incarnation AND t.old_epoch=c.epoch)`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch, evidence).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if err = f.enter(); err != nil {
		return err
	}
	l := &Lease{registration: reg, open: true, ctx: f.ctx, tx: f.tx}
	if err = fn(l); err != nil {
		return err
	}
	return f.tx.Commit()
}

// CheckSessionRead checks foreign ownership under the caller's existing
// registry fence without reserving an unmanaged historical session.
func (l *Lease) CheckSessionRead(reg Registration, session string) error {
	if l == nil || !canonical(session) {
		return ErrConflict
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil {
		return ErrConflict
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	if err := checkForeignReadIntents(l.ctx, l.tx, reg, session); err != nil {
		return err
	}
	c, o := reg.Carrier, reg.Owner
	var app, id string
	var epoch int64
	var active int
	err := l.tx.QueryRowContext(l.ctx, `SELECT app,owner_id,epoch,active FROM sessions WHERE backend=? AND namespace=? AND session_id=?`, c.Backend, c.Namespace, session).Scan(&app, &id, &epoch, &active)
	if err == nil {
		var allowed int
		err = l.tx.QueryRowContext(l.ctx, `SELECT count(*) FROM sessions s WHERE s.backend=? AND s.namespace=? AND s.session_id=? AND ((s.app=? AND s.owner_id=? AND s.epoch=? AND NOT EXISTS(SELECT 1 FROM transfers revoked WHERE revoked.incarnation=s.incarnation AND revoked.old_epoch=s.epoch)) OR (s.active=0 AND EXISTS(SELECT 1 FROM transfers t WHERE t.incarnation=s.incarnation AND t.old_epoch=s.epoch AND t.new_app=? AND t.new_owner=? AND t.new_epoch=?)))`, c.Backend, c.Namespace, session, o.Application, o.ID, o.Epoch, o.Application, o.ID, o.Epoch).Scan(&allowed)
		if err != nil {
			return err
		}
		if allowed != 1 {
			return ErrConflict
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	err = l.tx.QueryRowContext(l.ctx, `SELECT app,owner_id,epoch FROM session_reservations WHERE backend=? AND namespace=? AND session_id=?`, c.Backend, c.Namespace, session).Scan(&app, &id, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if app != string(o.Application) || id != o.ID || epoch != o.Epoch {
		return ErrConflict
	}
	return nil
}

// CheckSessionControl allows an interrupt/response only for this active
// carrier's exact bound session without applying the business maintenance gate.
func (l *Lease) CheckSessionControl(reg Registration, session string) error {
	if l == nil || !canonical(session) {
		return ErrConflict
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open || l.registration != reg || l.tx == nil {
		return ErrConflict
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	c, o := reg.Carrier, reg.Owner
	var n int
	err := l.tx.QueryRowContext(l.ctx, `SELECT count(*) FROM sessions WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND app=? AND owner_id=? AND epoch=? AND active=1`, c.Backend, c.Namespace, session, c.Incarnation, o.Application, o.ID, o.Epoch).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
