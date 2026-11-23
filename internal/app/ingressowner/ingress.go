package ingressowner

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Admit first commits the immutable platform facts. A duplicate returns the
// first source generation and frozen owner, never a newly resolved navigation.
// Unresolved events remain durable and can be retried through Resolve.
func (r *Router) Admit(ctx context.Context, e Event, resolver Resolver) (Record, bool, error) {
	if err := e.validate(); err != nil {
		return Record{}, false, err
	}
	unlock := r.acquire(e.Key)
	defer unlock()
	raw, err := facts(e)
	if err != nil {
		return Record{}, false, err
	}
	historical, herr := r.getHistorical(ctx, e.Key)
	if herr == nil {
		savedFacts, e2 := facts(historical.Event)
		if e2 != nil {
			return Record{}, false, e2
		}
		if !bytes.Equal(savedFacts, raw) {
			return Record{}, false, ErrConflict
		}
		return historical, true, nil
	}
	if !errors.Is(herr, sql.ErrNoRows) {
		return Record{}, false, herr
	}
	source, err := json.Marshal(e.Source)
	if err != nil {
		return Record{}, false, err
	}
	result, err := r.db.ExecContext(ctx, `INSERT INTO events(tenant,app_id,event_id,facts,source,state,revision) VALUES(?,?,?,?,?,'unresolved',1) ON CONFLICT DO NOTHING`, e.Key.Tenant, e.Key.AppID, e.Key.EventID, raw, source)
	if err != nil {
		return Record{}, false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Record{}, false, err
	}
	saved, err := r.get(ctx, e.Key)
	if err != nil {
		return Record{}, false, err
	}
	savedFacts, err := facts(saved.Event)
	if err != nil {
		return Record{}, false, err
	}
	if !bytes.Equal(savedFacts, raw) {
		return Record{}, false, ErrConflict
	}
	if saved.State != Unresolved {
		return saved, n == 0, nil
	}
	if resolver == nil {
		return saved, n == 0, nil
	}
	saved, err = r.resolveLocked(ctx, saved, resolver)
	return saved, n == 0, err
}

// Resolve retries only previously unresolved events, using the originally
// captured SDK source, never the latest caller's gateway generation.
func (r *Router) Resolve(ctx context.Context, k Key, resolver Resolver) (Record, error) {
	if resolver == nil {
		return Record{}, ErrInvalid
	}
	unlock := r.acquire(k)
	defer unlock()
	saved, err := r.get(ctx, k)
	if err != nil {
		return Record{}, err
	}
	if saved.State != Unresolved {
		return saved, nil
	}
	return r.resolveLocked(ctx, saved, resolver)
}
func (r *Router) resolveLocked(ctx context.Context, saved Record, resolver Resolver) (Record, error) {
	e := saved.Event
	if e.Kind == Card && (e.CardRef == "" || (e.CardKind != "team_view" && e.CardKind != "private_view" && e.CardKind != "team_setup_ingress")) {
		return r.expireLocked(ctx, saved)
	}
	var target *PlatformMessage
	var private *PrivateBinding
	var err error
	switch e.Kind {
	case Card, Reaction, Recall:
		target, err = r.lookupMessage(ctx, e)
	case Menu:
		private, err = r.lookupPrivate(ctx, e)
	}
	if errors.Is(err, ErrUnresolved) {
		return saved, nil
	}
	if err != nil {
		return saved, err
	}
	if target != nil && (target.GatewayID != e.Source.GatewayID || target.BindingGeneration != e.Source.BindingGeneration) {
		if e.Kind == Card {
			return r.expireLocked(ctx, saved)
		}
		return saved, nil
	}
	if e.Kind == Card {
		current, err := r.privateCardReferenceCurrent(ctx, e, target)
		if err != nil {
			return saved, err
		}
		if !current {
			return r.expireLocked(ctx, saved)
		}
	}
	owner, err := resolver.Resolve(ctx, e, target)
	if errors.Is(err, ErrUnresolved) {
		return saved, nil
	}
	if err != nil {
		return saved, err
	}
	if err = checkResolved(e, owner, target); err != nil {
		return saved, err
	}
	// A historical personal team scope may own p2p only when a trusted source
	// witness was imported. All other p2p remains private by construction.
	if e.Kind == Message && e.ChatType == "p2p" && owner.Application == Team {
		if e.Operator.IDType != "open_id" {
			return saved, ErrConflict
		}
		imported, lookupErr := r.LookupImportedOwner(ctx, e.Key.Tenant, e.Key.AppID, e.Source.GatewayID, e.Source.BindingGeneration, e.ChatID, e.Operator.ID)
		if lookupErr != nil || imported != owner {
			return saved, ErrConflict
		}
	}
	if private != nil && owner != private.Owner {
		return saved, ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return saved, err
	}
	defer tx.Rollback()
	// A platform receipt update during authorization invalidates that snapshot.
	if target != nil {
		if e.ChatID == "" {
			var count int
			if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM messages WHERE tenant=? AND app_id=? AND message_id=?`, e.Key.Tenant, e.Key.AppID, e.MessageID).Scan(&count); err != nil {
				return saved, err
			}
			if count != 1 {
				return saved, ErrConflict
			}
		}
		var current []byte
		err = tx.QueryRowContext(ctx, `SELECT payload FROM messages WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=?`, target.Tenant, target.AppID, target.ChatID, target.MessageID).Scan(&current)
		if err != nil {
			return saved, err
		}
		if e.Kind == Card {
			valid, checkErr := privateCardReferenceCurrent(ctx, tx, e, target)
			if checkErr != nil {
				return saved, checkErr
			}
			if !valid {
				if err = tx.Rollback(); err != nil {
					return saved, err
				}
				return r.expireLocked(ctx, saved)
			}
		}
		expected, _ := json.Marshal(target)
		if !bytes.Equal(current, expected) {
			return saved, ErrConflict
		}
	}
	if e.Kind == Message {
		message := PlatformMessage{Tenant: e.Key.Tenant, AppID: e.Key.AppID, GatewayID: e.Source.GatewayID, BindingGeneration: e.Source.BindingGeneration, ChatID: e.ChatID, MessageID: e.MessageID, Owner: owner, Version: 1, Lifecycle: "inbound"}
		raw, _ := json.Marshal(message)
		_, err = tx.ExecContext(ctx, `INSERT INTO messages(tenant,app_id,chat_id,message_id,payload) VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, message.Tenant, message.AppID, message.ChatID, message.MessageID, raw)
		if err != nil {
			return saved, err
		}
		var current []byte
		if err = tx.QueryRowContext(ctx, `SELECT payload FROM messages WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=?`, message.Tenant, message.AppID, message.ChatID, message.MessageID).Scan(&current); err != nil {
			return saved, err
		}
		if !bytes.Equal(current, raw) {
			return saved, ErrConflict
		}
	}
	encoded, _ := json.Marshal(owner)
	var route []byte
	if target != nil || private != nil {
		route, err = json.Marshal(RouteSnapshot{Message: target, Private: private})
		if err != nil {
			return saved, err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE events SET owner=?,route=?,state='pending',revision=revision+1 WHERE seq=? AND state='unresolved' AND revision=?`, encoded, route, saved.Sequence, saved.Revision)
	if err != nil {
		return saved, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return saved, err
	}
	if n != 1 {
		return saved, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return saved, err
	}
	return r.get(ctx, e.Key)
}

// expireLocked records a callback that cannot prove the exact current card
// version. It is retained for audit and must not dispatch to either application.
func (r *Router) expireLocked(ctx context.Context, v Record) (Record, error) {
	if v.State != Unresolved && v.State != Pending {
		return v, ErrConflict
	}
	res, err := r.db.ExecContext(ctx, `UPDATE events SET state='expired',revision=revision+1 WHERE seq=? AND state=? AND revision=?`, v.Sequence, string(v.State), v.Revision)
	if err != nil {
		return v, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return v, err
	}
	if n != 1 {
		return v, ErrConflict
	}
	return r.get(ctx, v.Event.Key)
}

// BeforeAdmissionError is returned only when the application proves it has not
// attempted any durable admission; uncertain errors must never use this type.
type BeforeAdmissionError struct{ Err error }

func (e *BeforeAdmissionError) Error() string { return e.Err.Error() }
func (e *BeforeAdmissionError) Unwrap() error { return e.Err }

// Deliver runs the application admission outside the SQLite transaction. The
// application must persistently deduplicate by Event.Key before returning.
func (r *Router) Deliver(ctx context.Context, k Key, app Admitter) (Record, error) {
	return r.deliver(ctx, k, app, false)
}

// ReconcileBeforeDrain retries only a source admission that was already invoked
// before the gate closed; no new SDK event may use this path.
func (r *Router) ReconcileBeforeDrain(ctx context.Context, k Key, app Admitter) (Record, error) {
	return r.deliver(ctx, k, app, true)
}
func (r *Router) deliver(ctx context.Context, k Key, app Admitter, reconcile bool) (Record, error) {
	if app == nil {
		return Record{}, ErrInvalid
	}
	unlock := r.acquire(k)
	defer unlock()
	saved, err := r.get(ctx, k)
	if err != nil {
		return saved, err
	}
	if saved.State == Accepted {
		return saved, nil
	}
	if saved.State != Pending || saved.Owner == nil {
		return saved, ErrUnresolved
	}
	releaseOwner := r.acquireOwner(*saved.Owner)
	defer releaseOwner()
	// Re-read after acquiring the shared owner fence: migration may have started
	// between the initial event lookup and this lock.
	saved, err = r.get(ctx, k)
	if err != nil {
		return saved, err
	}
	if saved.State == Accepted {
		return saved, nil
	}
	if saved.State != Pending || saved.Owner == nil {
		return saved, ErrUnresolved
	}
	if err = r.allowOwnerDelivery(ctx, saved, reconcile); err != nil {
		return saved, err
	}
	if saved.Event.Kind == Card && saved.Route != nil && saved.Route.Message != nil {
		var attempted int
		if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM delivery_attempts WHERE seq=?`, saved.Sequence).Scan(&attempted); err != nil {
			return saved, err
		}
		// A previously attempted admission may already have committed in its
		// application. Reconcile that exact intent rather than labelling it
		// unexecuted; its obsolete HTTP response is suppressed below.
		if attempted == 0 {
			target, lookupErr := r.lookupMessage(ctx, saved.Event)
			if lookupErr != nil {
				return saved, lookupErr
			}
			current, checkErr := r.privateCardReferenceCurrent(ctx, saved.Event, target)
			if checkErr != nil {
				return saved, checkErr
			}
			if !current || target.Version != saved.Route.Message.Version {
				return r.expireLocked(ctx, saved)
			}
		}
	}
	res, err := r.db.ExecContext(ctx, `UPDATE events SET state='delivering',revision=revision+1 WHERE seq=? AND state='pending' AND revision=?`, saved.Sequence, saved.Revision)
	if err != nil {
		return saved, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return saved, err
	}
	if n != 1 {
		return saved, ErrConflict
	}
	saved.State = Delivering
	saved.Revision++
	if _, err = r.db.ExecContext(ctx, `INSERT INTO delivery_attempts(seq,state) VALUES(?,'unknown') ON CONFLICT(seq) DO UPDATE SET state='unknown'`, saved.Sequence); err != nil {
		return saved, err
	}
	receipt, err := app.Admit(ctx, saved)
	if err != nil {
		var before *BeforeAdmissionError
		if errors.As(err, &before) {
			if _, e := r.db.ExecContext(context.WithoutCancel(ctx), `DELETE FROM delivery_attempts WHERE seq=?`, saved.Sequence); e != nil {
				return saved, errors.Join(err, e)
			}
		}
		_, rollbackErr := r.db.ExecContext(context.WithoutCancel(ctx), `UPDATE events SET state='pending',revision=revision+1 WHERE seq=? AND state='delivering' AND revision=?`, saved.Sequence, saved.Revision)
		if rollbackErr != nil {
			return saved, errors.Join(err, rollbackErr)
		}
		return saved, err
	}
	expectedHash, hashErr := AdmissionHash(saved)
	if hashErr == nil && (receipt.Key != saved.Event.Key || receipt.Owner != deliveryOwner(saved) || receipt.AdmissionID == "" || receipt.PayloadHash != expectedHash || (len(receipt.Response) > 0 && !json.Valid(receipt.Response))) {
		hashErr = ErrInvalid
	}
	if hashErr != nil {
		_, resetErr := r.db.ExecContext(context.WithoutCancel(ctx), `UPDATE events SET state='pending',revision=revision+1 WHERE seq=? AND state='delivering' AND revision=?`, saved.Sequence, saved.Revision)
		return saved, errors.Join(fmt.Errorf("%w: invalid application admission receipt", hashErr), resetErr)
	}
	commitCtx := context.WithoutCancel(ctx)
	tx, err := r.db.BeginTx(commitCtx, nil)
	if err != nil {
		return saved, err
	}
	defer tx.Rollback()
	if err = recordCallbackReplacement(commitCtx, tx, saved, &receipt); err != nil {
		return saved, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return saved, err
	}
	res, err = tx.ExecContext(commitCtx, `UPDATE events SET state='accepted',response=?,receipt=?,revision=revision+1 WHERE seq=? AND state='delivering' AND revision=?`, []byte(receipt.Response), encoded, saved.Sequence, saved.Revision)
	if err != nil {
		return saved, err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return saved, err
	}
	if n != 1 {
		return saved, ErrConflict
	}
	if _, err = tx.ExecContext(commitCtx, `UPDATE delivery_attempts SET state='confirmed' WHERE seq=?`, saved.Sequence); err != nil {
		return saved, err
	}
	if err = tx.Commit(); err != nil {
		return saved, err
	}
	return r.get(commitCtx, k)
}

// Outstanding returns unresolved or retryable events in persistent source order.
func (r *Router) Outstanding(ctx context.Context, after int64, limit int) ([]Record, error) {
	if after < 0 || limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT tenant,app_id,event_id FROM events WHERE seq>? AND state IN ('unresolved','pending') ORDER BY seq LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	var keys []Key
	for rows.Next() {
		var k Key
		if err = rows.Scan(&k.Tenant, &k.AppID, &k.EventID); err != nil {
			rows.Close()
			return nil, err
		}
		keys = append(keys, k)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	out := make([]Record, 0, len(keys))
	for _, k := range keys {
		v, e := r.get(ctx, k)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if e == nil {
			out = append(out, v)
		}
	}
	return out, nil
}
