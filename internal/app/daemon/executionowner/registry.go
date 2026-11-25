// Package executionowner persists application ownership of native carriers.
// It does not interpret application state or inspect native processes.
package executionowner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

type Application string

const (
	Private Application = "private"
	Team    Application = "team"
)

type Owner struct {
	Application Application
	ID          string
	Epoch       int64
}

// Carrier is frozen before native session creation. Namespace identifies the
// backend account/runtime namespace; it must not be inferred from a chat.
type Carrier struct {
	InstanceID, Incarnation, Backend, Namespace string
	Generation                                  int64
}
type Registration struct {
	Carrier Carrier
	Owner   Owner
}

// StopVerifier is a trusted integration port. Implementations must verify a
// durable, exact-carrier proof of quiescence; accepting user assertions is unsafe.
type StopVerifier interface {
	VerifyStop(context.Context, Registration, string) error
}

var ErrNotFound = errors.New("execution owner not found")
var ErrConflict = errors.New("execution owner conflict")

type Registry struct {
	db       *sql.DB
	verifier StopVerifier
}

func Open(path string, verifier StopVerifier) (*Registry, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("registry path must be absolute")
	}
	if verifier == nil {
		return nil, fmt.Errorf("trusted stop verifier required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(FULL)&_txlock=immediate"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}
	tx, err := db.Begin()
	if err != nil {
		db.Close()
		return nil, err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		tx.Rollback()
		db.Close()
		return nil, err
	}
	if version > 1 {
		tx.Rollback()
		db.Close()
		return nil, fmt.Errorf("execution owner schema %d is newer than supported 1", version)
	}
	if version == 0 {
		var tables int
		if err = tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil || tables != 0 {
			tx.Rollback()
			db.Close()
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("unversioned execution owner database is not empty")
		}
	}
	schema := `CREATE TABLE IF NOT EXISTS carriers (
 instance_id TEXT NOT NULL UNIQUE, incarnation TEXT PRIMARY KEY, backend TEXT NOT NULL, namespace TEXT NOT NULL,
 generation INTEGER NOT NULL CHECK(generation > 0), app TEXT NOT NULL CHECK(app IN ('private','team')),
 owner_id TEXT NOT NULL, epoch INTEGER NOT NULL CHECK(epoch > 0), stopped INTEGER NOT NULL DEFAULT 0);
 CREATE TABLE IF NOT EXISTS sessions (
 backend TEXT NOT NULL, namespace TEXT NOT NULL, session_id TEXT NOT NULL,
 incarnation TEXT NOT NULL, epoch INTEGER NOT NULL, app TEXT NOT NULL, owner_id TEXT NOT NULL, active INTEGER NOT NULL,
 PRIMARY KEY(backend,namespace,session_id));
 CREATE TABLE IF NOT EXISTS session_reservations (backend TEXT NOT NULL, namespace TEXT NOT NULL, session_id TEXT NOT NULL, incarnation TEXT NOT NULL, epoch INTEGER NOT NULL, app TEXT NOT NULL, owner_id TEXT NOT NULL, PRIMARY KEY(backend,namespace,session_id));
 CREATE TABLE IF NOT EXISTS session_transfers (backend TEXT NOT NULL, namespace TEXT NOT NULL, session_id TEXT NOT NULL, incarnation TEXT NOT NULL, old_epoch INTEGER NOT NULL, new_app TEXT NOT NULL, new_owner TEXT NOT NULL, new_epoch INTEGER NOT NULL, PRIMARY KEY(backend,namespace,session_id,incarnation,old_epoch));
 CREATE TABLE IF NOT EXISTS stop_receipts (
 incarnation TEXT NOT NULL, epoch INTEGER NOT NULL, evidence_ref TEXT NOT NULL,
 PRIMARY KEY(incarnation,epoch));
 CREATE TABLE IF NOT EXISTS transfers (
 incarnation TEXT NOT NULL, old_app TEXT NOT NULL, old_owner TEXT NOT NULL, old_epoch INTEGER NOT NULL,
 new_app TEXT NOT NULL, new_owner TEXT NOT NULL, new_epoch INTEGER NOT NULL, evidence_ref TEXT NOT NULL,
 PRIMARY KEY(incarnation,old_epoch));` + maintenanceSchema
	if _, err = tx.Exec(schema); err != nil {
		tx.Rollback()
		db.Close()
		return nil, err
	}
	if _, err = tx.Exec("PRAGMA user_version=1"); err != nil {
		tx.Rollback()
		db.Close()
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		db.Close()
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		db.Close()
		return nil, err
	}
	return &Registry{db: db, verifier: verifier}, nil
}
func (r *Registry) Close() error { return r.db.Close() }
func canonical(s string) bool    { return s != "" && s == strings.TrimSpace(s) }
func (c Carrier) validate() error {
	if !canonical(c.InstanceID) || !canonical(c.Incarnation) || !canonical(c.Backend) || !canonical(c.Namespace) || c.Generation <= 0 {
		return fmt.Errorf("incomplete carrier identity")
	}
	return nil
}
func (o Owner) validate() error {
	if (o.Application != Private && o.Application != Team) || !canonical(o.ID) || o.Epoch <= 0 {
		return fmt.Errorf("invalid application owner")
	}
	return nil
}
func (v Registration) validate() error {
	if err := v.Carrier.validate(); err != nil {
		return err
	}
	return v.Owner.validate()
}

// Register permits replay only of the identical live registration.
func (r *Registry) Register(ctx context.Context, v Registration) error {
	if err := v.validate(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, o := v.Carrier, v.Owner
	var existing Carrier
	var owner Owner
	var stopped int
	err = tx.QueryRowContext(ctx, `SELECT instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch,stopped FROM carriers WHERE incarnation=?`, c.Incarnation).Scan(&existing.InstanceID, &existing.Incarnation, &existing.Backend, &existing.Namespace, &existing.Generation, &owner.Application, &owner.ID, &owner.Epoch, &stopped)
	if err == nil {
		if existing != c || owner != o || stopped != 0 {
			return ErrConflict
		}
		return tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err = checkMaintenanceTx(ctx, tx, c.Backend); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO carriers(instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

// Resolve requires the epoch frozen by the authenticated connection. Stopped
// carriers and stale events are rejected, even after ownership changes.
func (r *Registry) Resolve(ctx context.Context, c Carrier, epoch int64) (Owner, error) {
	if err := c.validate(); err != nil {
		return Owner{}, err
	}
	if epoch <= 0 {
		return Owner{}, fmt.Errorf("epoch required")
	}
	var stored Carrier
	var o Owner
	var stopped int
	err := r.db.QueryRowContext(ctx, `SELECT instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch,stopped FROM carriers WHERE incarnation=?`, c.Incarnation).Scan(&stored.InstanceID, &stored.Incarnation, &stored.Backend, &stored.Namespace, &stored.Generation, &o.Application, &o.ID, &o.Epoch, &stopped)
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, ErrNotFound
	}
	if err != nil {
		return Owner{}, err
	}
	if stored != c || o.Epoch != epoch || stopped != 0 {
		return Owner{}, ErrConflict
	}
	return o, nil
}

// BindSession claims a confirmed native session for a live carrier. A session
// can be rebound only after the previous carrier has verified stop evidence.
func (r *Registry) BindSession(ctx context.Context, v Registration, session string) error {
	if err := v.validate(); err != nil {
		return err
	}
	if !canonical(session) {
		return fmt.Errorf("session ID required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = bindSessionTx(ctx, tx, v, session); err != nil {
		return err
	}
	return tx.Commit()
}

func bindSessionTx(ctx context.Context, tx *sql.Tx, v Registration, session string) error {
	if !canonical(session) {
		return fmt.Errorf("session ID required")
	}
	var err error
	c, o := v.Carrier, v.Owner
	var reservation int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM session_reservations WHERE backend=? AND namespace=? AND session_id=? AND NOT (incarnation=? AND epoch=? AND app=? AND owner_id=?)`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID).Scan(&reservation); err != nil {
		return err
	}
	if reservation != 0 {
		return ErrConflict
	}
	var count int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM carriers WHERE instance_id=? AND incarnation=? AND backend=? AND namespace=? AND generation=? AND app=? AND owner_id=? AND epoch=? AND stopped=0`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO sessions(backend,namespace,session_id,incarnation,epoch,app,owner_id,active) VALUES(?,?,?,?,?,?,?,1) ON CONFLICT(backend,namespace,session_id) DO UPDATE SET incarnation=excluded.incarnation,epoch=excluded.epoch,app=excluded.app,owner_id=excluded.owner_id,active=1 WHERE (sessions.incarnation=excluded.incarnation AND sessions.epoch=excluded.epoch AND sessions.app=excluded.app AND sessions.owner_id=excluded.owner_id) OR (sessions.active=0 AND ((sessions.app=excluded.app AND sessions.owner_id=excluded.owner_id AND sessions.epoch=excluded.epoch) OR EXISTS (SELECT 1 FROM transfers WHERE incarnation=sessions.incarnation AND old_epoch=sessions.epoch AND new_app=excluded.app AND new_owner=excluded.owner_id AND new_epoch=excluded.epoch) OR EXISTS (SELECT 1 FROM backend_maintenance_session_transfers t WHERE t.backend=sessions.backend AND t.namespace=sessions.namespace AND t.session_id=sessions.session_id AND t.source_incarnation=sessions.incarnation AND t.source_epoch=sessions.epoch AND t.new_incarnation=excluded.incarnation AND t.new_app=excluded.app AND t.new_owner=excluded.owner_id AND t.new_epoch=excluded.epoch)))`, c.Backend, c.Namespace, session, c.Incarnation, o.Epoch, o.Application, o.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

// ResolveSession verifies both the exact connection lease and session binding.
func (r *Registry) ResolveSession(ctx context.Context, c Carrier, epoch int64, session string) (Owner, error) {
	if !canonical(session) {
		return Owner{}, fmt.Errorf("session ID required")
	}
	if err := c.validate(); err != nil {
		return Owner{}, err
	}
	if epoch <= 0 {
		return Owner{}, fmt.Errorf("epoch required")
	}
	var o Owner
	err := r.db.QueryRowContext(ctx, `SELECT c.app,c.owner_id,c.epoch FROM carriers c JOIN sessions s ON s.incarnation=c.incarnation AND s.epoch=c.epoch WHERE c.instance_id=? AND c.incarnation=? AND c.backend=? AND c.namespace=? AND c.generation=? AND c.epoch=? AND c.stopped=0 AND s.backend=? AND s.namespace=? AND s.session_id=? AND s.active=1`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, epoch, c.Backend, c.Namespace, session).Scan(&o.Application, &o.ID, &o.Epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return Owner{}, ErrConflict
	}
	return o, err
}

// Retire releases the live session claim only after the trusted verifier has
// checked a durable proof. It never activates a new owner or carrier.
func (r *Registry) Retire(ctx context.Context, v Registration, evidence string) error {
	return r.finish(ctx, v, nil, evidence)
}

// Transfer fences the old epoch and records its successor atomically. The old
// carrier stays stopped; the successor must launch a fresh carrier incarnation.
func (r *Registry) Transfer(ctx context.Context, v Registration, next Owner, evidence string) error {
	if err := next.validate(); err != nil {
		return err
	}
	if next.Epoch != v.Owner.Epoch+1 {
		return fmt.Errorf("transfer requires next epoch")
	}
	return r.finish(ctx, v, &next, evidence)
}
func (r *Registry) finish(ctx context.Context, v Registration, next *Owner, evidence string) error {
	if err := v.validate(); err != nil {
		return err
	}
	if !canonical(evidence) {
		return fmt.Errorf("stop evidence reference required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	done, err := replayed(ctx, tx, v, next, evidence)
	if err != nil {
		return err
	}
	if done {
		return tx.Commit()
	}
	// Verify under the same inter-process write fence as lease operations.
	// The verifier must perform only local evidence/OS inspection.
	if err := r.verifier.VerifyStop(ctx, v, evidence); err != nil {
		return fmt.Errorf("verify stop: %w", err)
	}
	c, o := v.Carrier, v.Owner
	result, err := tx.ExecContext(ctx, `UPDATE carriers SET stopped=1 WHERE instance_id=? AND incarnation=? AND backend=? AND namespace=? AND generation=? AND app=? AND owner_id=? AND epoch=? AND stopped=0`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE sessions SET active=0 WHERE incarnation=? AND epoch=? AND active=1`, c.Incarnation, o.Epoch); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM session_reservations WHERE incarnation=? AND epoch=?`, c.Incarnation, o.Epoch); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO stop_receipts(incarnation,epoch,evidence_ref) VALUES(?,?,?)`, c.Incarnation, o.Epoch, evidence); err != nil {
		return err
	}
	if next != nil {
		if _, err = tx.ExecContext(ctx, `INSERT INTO session_transfers SELECT backend,namespace,session_id,incarnation,epoch,?,?,? FROM sessions WHERE incarnation=? AND epoch=?`, next.Application, next.ID, next.Epoch, c.Incarnation, o.Epoch); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO transfers(incarnation,old_app,old_owner,old_epoch,new_app,new_owner,new_epoch,evidence_ref) VALUES(?,?,?,?,?,?,?,?)`, c.Incarnation, o.Application, o.ID, o.Epoch, next.Application, next.ID, next.Epoch, evidence)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// replayed checks exact durable operation identity; a different operation cannot
// be mistaken for a successful replay.
type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func replayed(ctx context.Context, query rowQueryer, v Registration, next *Owner, evidence string) (bool, error) {
	var ref string
	err := query.QueryRowContext(ctx, `SELECT evidence_ref FROM stop_receipts WHERE incarnation=? AND epoch=?`, v.Carrier.Incarnation, v.Owner.Epoch).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if ref != evidence {
		return false, ErrConflict
	}
	var stored Carrier
	var o Owner
	var stopped int
	err = query.QueryRowContext(ctx, `SELECT instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch,stopped FROM carriers WHERE incarnation=?`, v.Carrier.Incarnation).Scan(&stored.InstanceID, &stored.Incarnation, &stored.Backend, &stored.Namespace, &stored.Generation, &o.Application, &o.ID, &o.Epoch, &stopped)
	if err != nil {
		return false, err
	}
	if stored != v.Carrier || o != v.Owner || stopped != 1 {
		return false, ErrConflict
	}
	var app Application
	var id string
	var epoch int64
	var transferRef string
	err = query.QueryRowContext(ctx, `SELECT new_app,new_owner,new_epoch,evidence_ref FROM transfers WHERE incarnation=? AND old_epoch=?`, v.Carrier.Incarnation, v.Owner.Epoch).Scan(&app, &id, &epoch, &transferRef)
	if errors.Is(err, sql.ErrNoRows) {
		if next == nil {
			return true, nil
		}
		return false, ErrConflict
	}
	if err != nil {
		return false, err
	}
	if next == nil || *next != (Owner{app, id, epoch}) || transferRef != evidence {
		return false, ErrConflict
	}
	return true, nil
}

// WithLease serializes a local application transaction with retirement and
// transfer across registry connections and processes. The callback must not use
// this registry or perform network I/O; it may commit only local durable facts.
func (r *Registry) WithLease(ctx context.Context, v Registration, commit func() error) error {
	if commit == nil {
		return fmt.Errorf("lease callback required")
	}
	return r.withLeaseTx(ctx, v, func(_ context.Context, _ *sql.Tx) error { return commit() })
}

func (r *Registry) withLeaseTx(ctx context.Context, v Registration, commit func(context.Context, *sql.Tx) error) error {
	if err := v.validate(); err != nil {
		return err
	}
	if commit == nil {
		return fmt.Errorf("lease callback required")
	}
	fence, err := r.beginLocalFence(ctx)
	if err != nil {
		return err
	}
	defer fence.close()
	tx := fence.tx
	c, o := v.Carrier, v.Owner
	var count int
	err = tx.QueryRowContext(fence.ctx, `SELECT count(*) FROM carriers WHERE instance_id=? AND incarnation=? AND backend=? AND namespace=? AND generation=? AND app=? AND owner_id=? AND epoch=? AND stopped=0`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch).Scan(&count)
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	if err = fence.enter(); err != nil {
		return err
	}
	if err = commit(fence.ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

// SessionOwner reads an exact native-history ownership fact. Unknown means the
// registry has no ownership claim; callers must separately consult historical
// team inventory before treating a session as an ordinary local history.
func (r *Registry) SessionOwner(ctx context.Context, backend, namespace, session string) (Owner, bool, error) {
	var owner Owner
	if r == nil || !canonical(backend) || !canonical(namespace) || !canonical(session) {
		return owner, false, ErrConflict
	}
	err := r.db.QueryRowContext(ctx, `SELECT app,owner_id,epoch FROM sessions WHERE backend=? AND namespace=? AND session_id=?`, backend, namespace, session).Scan(&owner.Application, &owner.ID, &owner.Epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return owner, false, nil
	}
	if err != nil {
		return owner, false, err
	}
	return owner, true, nil
}

// SessionAccess distinguishes ordinary unregistered history from a managed
// session, and honors only an explicit durable transfer for a stopped source.
func (r *Registry) SessionAccess(ctx context.Context, backend, namespace, session string, owner Owner) (known, allowed bool, err error) {
	if r == nil || !canonical(backend) || !canonical(namespace) || !canonical(session) || owner.validate() != nil {
		return false, false, ErrConflict
	}
	var count int
	err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM sessions s WHERE s.backend=? AND s.namespace=? AND s.session_id=? AND ((s.app=? AND s.owner_id=? AND s.epoch=? AND NOT EXISTS(SELECT 1 FROM transfers revoked WHERE revoked.incarnation=s.incarnation AND revoked.old_epoch=s.epoch)) OR (s.active=0 AND EXISTS(SELECT 1 FROM transfers t WHERE t.incarnation=s.incarnation AND t.old_epoch=s.epoch AND t.new_app=? AND t.new_owner=? AND t.new_epoch=?)))`, backend, namespace, session, owner.Application, owner.ID, owner.Epoch, owner.Application, owner.ID, owner.Epoch).Scan(&count)
	if err != nil {
		return false, false, err
	}
	if count == 1 {
		return true, true, nil
	}
	_, known, err = r.SessionOwner(ctx, backend, namespace, session)
	if err != nil || known {
		return known, false, err
	}
	var app Application
	var id string
	var epoch int64
	err = r.db.QueryRowContext(ctx, `SELECT app,owner_id,epoch FROM session_reservations WHERE backend=? AND namespace=? AND session_id=?`, backend, namespace, session).Scan(&app, &id, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return true, Owner{Application: app, ID: id, Epoch: epoch} == owner, nil
}

// ReserveSession serializes a requested resume before native I/O. Reservation
// is not evidence that the native session exists and is never returned as a
// session binding; BindSession still requires a verified native event.
func (r *Registry) ReserveSession(ctx context.Context, reg Registration, session string) error {
	if !canonical(session) {
		return ErrConflict
	}
	return r.withLeaseTx(ctx, reg, func(local context.Context, tx *sql.Tx) error { return reserveSessionTx(local, tx, reg, session) })
}
func reserveSessionTx(ctx context.Context, tx *sql.Tx, reg Registration, session string) error {
	if err := checkMaintenanceTx(ctx, tx, reg.Carrier.Backend); err != nil {
		return err
	}
	return reserveSessionTxUnchecked(ctx, tx, reg, session)
}

// reserveSessionTxUnchecked is for the maintenance recovery transaction after
// it has verified the frozen old-to-new carrier mapping and exact session.
func reserveSessionTxUnchecked(ctx context.Context, tx *sql.Tx, reg Registration, session string) error {
	var known, allowed int
	if err := tx.QueryRowContext(ctx, `SELECT count(*),coalesce(max(CASE WHEN (s.app=? AND s.owner_id=? AND s.epoch=? AND (s.active=0 OR s.incarnation=?) AND NOT EXISTS(SELECT 1 FROM transfers revoked WHERE revoked.incarnation=s.incarnation AND revoked.old_epoch=s.epoch)) OR (s.active=0 AND EXISTS(SELECT 1 FROM transfers t WHERE t.incarnation=s.incarnation AND t.old_epoch=s.epoch AND t.new_app=? AND t.new_owner=? AND t.new_epoch=?)) THEN 1 ELSE 0 END),0) FROM sessions s WHERE s.backend=? AND s.namespace=? AND s.session_id=?`, reg.Owner.Application, reg.Owner.ID, reg.Owner.Epoch, reg.Carrier.Incarnation, reg.Owner.Application, reg.Owner.ID, reg.Owner.Epoch, reg.Carrier.Backend, reg.Carrier.Namespace, session).Scan(&known, &allowed); err != nil {
		return err
	}
	if known != 0 && allowed == 0 {
		return ErrConflict
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO session_reservations(backend,namespace,session_id,incarnation,epoch,app,owner_id) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, reg.Carrier.Backend, reg.Carrier.Namespace, session, reg.Carrier.Incarnation, reg.Owner.Epoch, reg.Owner.Application, reg.Owner.ID)
	if err != nil {
		return err
	}
	var incarnation, app, id string
	var epoch int64
	if err = tx.QueryRowContext(ctx, `SELECT incarnation,epoch,app,owner_id FROM session_reservations WHERE backend=? AND namespace=? AND session_id=?`, reg.Carrier.Backend, reg.Carrier.Namespace, session).Scan(&incarnation, &epoch, &app, &id); err != nil {
		return err
	}
	if incarnation != reg.Carrier.Incarnation || epoch != reg.Owner.Epoch || app != string(reg.Owner.Application) || id != reg.Owner.ID {
		return ErrConflict
	}
	return nil
}

// HistoryClaim is a durable managed or reserved native history identity.
type HistoryClaim struct{ Backend, Namespace, Session string }

func (r *Registry) HistoryClaims(ctx context.Context) ([]HistoryClaim, error) {
	if r == nil {
		return nil, ErrConflict
	}
	rows, err := r.db.QueryContext(ctx, `SELECT backend,namespace,session_id FROM sessions UNION SELECT backend,namespace,session_id FROM session_reservations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []HistoryClaim
	for rows.Next() {
		var c HistoryClaim
		if err = rows.Scan(&c.Backend, &c.Namespace, &c.Session); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SessionTransferred reports an exact durable session transfer to the owner.
func (r *Registry) SessionTransferred(ctx context.Context, backend, namespace, session string, owner Owner) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM session_transfers WHERE backend=? AND namespace=? AND session_id=? AND new_app=? AND new_owner=? AND new_epoch=?`, backend, namespace, session, owner.Application, owner.ID, owner.Epoch).Scan(&n)
	return n > 0, err
}
