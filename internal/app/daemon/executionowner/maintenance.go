package executionowner

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// maintenanceSchema is installed in the same transaction as the ownership
// registry schema. An active epoch fences new business sends/registrations.
const maintenanceSchema = `
CREATE TABLE IF NOT EXISTS backend_maintenance (backend TEXT PRIMARY KEY, maintenance_id TEXT NOT NULL UNIQUE, epoch INTEGER NOT NULL, phase TEXT NOT NULL, target_version TEXT NOT NULL, evidence_ref TEXT NOT NULL DEFAULT '', source_evidence TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS backend_maintenance_carriers (maintenance_id TEXT NOT NULL, incarnation TEXT NOT NULL, instance_id TEXT NOT NULL, backend TEXT NOT NULL, namespace TEXT NOT NULL, generation INTEGER NOT NULL, app TEXT NOT NULL, owner_id TEXT NOT NULL, owner_epoch INTEGER NOT NULL, rebuilt_incarnation TEXT NOT NULL DEFAULT '', disposition TEXT NOT NULL DEFAULT '', recovery_evidence TEXT NOT NULL DEFAULT '', PRIMARY KEY(maintenance_id,incarnation));
CREATE TABLE IF NOT EXISTS backend_maintenance_releases (maintenance_id TEXT PRIMARY KEY, evidence TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS backend_maintenance_unspawned (maintenance_id TEXT NOT NULL, incarnation TEXT NOT NULL, evidence TEXT NOT NULL, PRIMARY KEY(maintenance_id,incarnation));
CREATE TABLE IF NOT EXISTS backend_maintenance_session_transfers (maintenance_id TEXT NOT NULL, backend TEXT NOT NULL, namespace TEXT NOT NULL, session_id TEXT NOT NULL, source_incarnation TEXT NOT NULL, source_epoch INTEGER NOT NULL, new_incarnation TEXT NOT NULL, new_app TEXT NOT NULL, new_owner TEXT NOT NULL, new_epoch INTEGER NOT NULL, PRIMARY KEY(maintenance_id,backend,namespace,session_id));
`

var ErrMaintenanceActive = errors.New("native backend maintenance active")
var ErrMaintenanceNotQuiescent = errors.New("native backend is not quiescent")

type Maintenance struct {
	Backend, ID, Phase, TargetVersion, EvidenceRef string
	Epoch                                          int64
	CreatedAt                                      time.Time
}

type MaintenanceRecovery struct {
	Old         Registration
	Replacement *Registration
	Disposition string
	Evidence    string
}

func (r *Registry) WithMaintenancePause(ctx context.Context, m Maintenance, old Registration, commit func() error) error {
	if commit == nil || old.validate() != nil {
		return ErrConflict
	}
	fence, err := r.beginLocalFence(ctx)
	if err != nil {
		return err
	}
	defer fence.close()
	tx := fence.tx
	var n int
	err = tx.QueryRowContext(fence.ctx, `SELECT count(*) FROM backend_maintenance m JOIN backend_maintenance_carriers x ON x.maintenance_id=m.maintenance_id WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.target_version=? AND m.phase='draining' AND x.incarnation=? AND x.instance_id=? AND x.backend=? AND x.namespace=? AND x.generation=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=?`, m.Backend, m.ID, m.Epoch, m.TargetVersion, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if err = fence.enter(); err != nil {
		return err
	}
	if err = commit(); err != nil {
		return err
	}
	return tx.Commit()
}

// WithMaintenanceRecoveryPreparation holds the registry epoch and the exact
// stopped frozen carrier stable while another local journal commits its
// recovery intent. The callback must not re-enter the registry or do I/O.
func (r *Registry) WithMaintenanceRecoveryPreparation(ctx context.Context, m Maintenance, old Registration, commit func() error) error {
	if commit == nil || old.validate() != nil {
		return ErrConflict
	}
	fence, err := r.beginLocalFence(ctx)
	if err != nil {
		return err
	}
	defer fence.close()
	tx := fence.tx
	var n int
	err = tx.QueryRowContext(fence.ctx, `SELECT count(*) FROM backend_maintenance m JOIN backend_maintenance_carriers x ON x.maintenance_id=m.maintenance_id JOIN carriers c ON c.incarnation=x.incarnation JOIN stop_receipts s ON s.incarnation=c.incarnation AND s.epoch=c.epoch WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.target_version=? AND m.phase='restoring' AND x.incarnation=? AND x.instance_id=? AND x.backend=? AND x.namespace=? AND x.generation=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=? AND x.disposition='' AND c.stopped=1`, m.Backend, m.ID, m.Epoch, m.TargetVersion, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if err = fence.enter(); err != nil {
		return err
	}
	if err = commit(); err != nil {
		return err
	}
	return tx.Commit()
}

// WithReleasedRecovery binds dispatch resumption to the exact committed
// replacement and released epoch. The callback may only touch a local journal.
func (r *Registry) WithReleasedRecovery(ctx context.Context, m Maintenance, old Registration, newInstance string, commit func() error) error {
	if commit == nil || old.validate() != nil || !canonical(newInstance) {
		return ErrConflict
	}
	fence, err := r.beginLocalFence(ctx)
	if err != nil {
		return err
	}
	defer fence.close()
	tx := fence.tx
	var n int
	err = tx.QueryRowContext(fence.ctx, `SELECT count(*) FROM backend_maintenance m JOIN backend_maintenance_carriers x ON x.maintenance_id=m.maintenance_id JOIN carriers c ON c.incarnation=x.rebuilt_incarnation WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.target_version=? AND m.phase='released' AND x.incarnation=? AND x.instance_id=? AND x.backend=? AND x.namespace=? AND x.generation=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=? AND x.disposition='rebuilt' AND x.recovery_evidence<>'' AND c.instance_id=? AND c.stopped=0`, m.Backend, m.ID, m.Epoch, m.TargetVersion, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, newInstance).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if err = fence.enter(); err != nil {
		return err
	}
	if err = commit(); err != nil {
		return err
	}
	return tx.Commit()
}

// MaintenanceRecoveries reads the frozen old-to-replacement mapping. The
// replacement is a registered identity, not proof that its process is ready.
func (r *Registry) MaintenanceRecoveries(ctx context.Context, m Maintenance, after string, limit int) ([]MaintenanceRecovery, error) {
	if limit < 1 || limit > 128 {
		return nil, ErrConflict
	}
	rows, err := r.db.QueryContext(ctx, `SELECT x.instance_id,x.incarnation,x.backend,x.namespace,x.generation,x.app,x.owner_id,x.owner_epoch,x.disposition,x.recovery_evidence,coalesce(c.instance_id,''),coalesce(c.incarnation,''),coalesce(c.backend,''),coalesce(c.namespace,''),coalesce(c.generation,0),coalesce(c.app,''),coalesce(c.owner_id,''),coalesce(c.epoch,0) FROM backend_maintenance_carriers x JOIN backend_maintenance m ON m.maintenance_id=x.maintenance_id LEFT JOIN carriers c ON c.incarnation=x.rebuilt_incarnation WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.target_version=? AND m.phase IN ('restoring','released') AND x.incarnation>? ORDER BY x.incarnation LIMIT ?`, m.Backend, m.ID, m.Epoch, m.TargetVersion, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []MaintenanceRecovery
	for rows.Next() {
		var v MaintenanceRecovery
		var next Registration
		err = rows.Scan(&v.Old.Carrier.InstanceID, &v.Old.Carrier.Incarnation, &v.Old.Carrier.Backend, &v.Old.Carrier.Namespace, &v.Old.Carrier.Generation, &v.Old.Owner.Application, &v.Old.Owner.ID, &v.Old.Owner.Epoch, &v.Disposition, &v.Evidence, &next.Carrier.InstanceID, &next.Carrier.Incarnation, &next.Carrier.Backend, &next.Carrier.Namespace, &next.Carrier.Generation, &next.Owner.Application, &next.Owner.ID, &next.Owner.Epoch)
		if err != nil {
			return nil, err
		}
		if next.Carrier.Incarnation != "" {
			v.Replacement = &next
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

// checkMaintenanceTx is called only for new business admission, never for
// native event projection, stop, inspection, or exit proof.
func checkMaintenanceTx(ctx context.Context, tx *sql.Tx, backend string) error {
	var phase string
	err := tx.QueryRowContext(ctx, `SELECT phase FROM backend_maintenance WHERE backend=?`, backend).Scan(&phase)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if phase != "released" {
		return ErrMaintenanceActive
	}
	return nil
}

// BeginMaintenance closes admission and freezes every currently live carrier
// in the same registry transaction. Outbound drains and exact stop proof follow.
func (r *Registry) BeginMaintenance(ctx context.Context, backend, id, target string) (Maintenance, error) {
	var result Maintenance
	if !canonical(backend) || !canonical(id) || !canonical(target) {
		return result, ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	var existing Maintenance
	var at string
	err = tx.QueryRowContext(ctx, `SELECT backend,maintenance_id,epoch,phase,target_version,evidence_ref,created_at FROM backend_maintenance WHERE backend=?`, backend).Scan(&existing.Backend, &existing.ID, &existing.Epoch, &existing.Phase, &existing.TargetVersion, &existing.EvidenceRef, &at)
	if err == nil && existing.ID == id {
		if existing.TargetVersion != target {
			return result, ErrConflict
		}
		existing.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return result, err
		}
		return existing, tx.Commit()
	}
	if err == nil && existing.Phase != "released" {
		return result, ErrMaintenanceActive
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	epoch := existing.Epoch + 1
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO backend_maintenance(backend,maintenance_id,epoch,phase,target_version,evidence_ref,created_at) VALUES(?,?,?,'draining',?,'',?) ON CONFLICT(backend) DO UPDATE SET maintenance_id=excluded.maintenance_id,epoch=excluded.epoch,phase=excluded.phase,target_version=excluded.target_version,evidence_ref='',source_evidence='',created_at=excluded.created_at`, backend, id, epoch, target, now.Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO backend_maintenance_carriers(maintenance_id,incarnation,instance_id,backend,namespace,generation,app,owner_id,owner_epoch) SELECT ?,incarnation,instance_id,backend,namespace,generation,app,owner_id,epoch FROM carriers WHERE backend=? AND stopped=0`, id, backend)
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return Maintenance{Backend: backend, ID: id, Epoch: epoch, Phase: "draining", TargetVersion: target, CreatedAt: now}, nil
}

// MaintenanceCarriers returns exact frozen registrations; later registrations
// are rejected until the epoch has been released.
func (r *Registry) MaintenanceCarriers(ctx context.Context, backend, id string, after string, limit int) ([]Registration, error) {
	if !canonical(backend) || !canonical(id) || limit < 1 || limit > 128 {
		return nil, ErrConflict
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.instance_id,c.incarnation,c.backend,c.namespace,c.generation,c.app,c.owner_id,c.owner_epoch FROM backend_maintenance_carriers c JOIN backend_maintenance m ON m.maintenance_id=c.maintenance_id WHERE m.backend=? AND m.maintenance_id=? AND m.phase<>'released' AND c.incarnation>? ORDER BY c.incarnation LIMIT ?`, backend, id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Registration
	for rows.Next() {
		var v Registration
		if err = rows.Scan(&v.Carrier.InstanceID, &v.Carrier.Incarnation, &v.Carrier.Backend, &v.Carrier.Namespace, &v.Carrier.Generation, &v.Owner.Application, &v.Owner.ID, &v.Owner.Epoch); err != nil {
			return nil, err
		}
		if err = v.validate(); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// MarkMaintenanceQuiescent accepts only exact registry stop receipts for every
// frozen carrier, not a caller's assertion or a momentary activity snapshot.
type MaintenanceSourceVerifier interface {
	VerifyMaintenanceSources(context.Context, Maintenance, string) error
}

func (r *Registry) MarkMaintenanceQuiescent(ctx context.Context, m Maintenance, evidence string, verifier MaintenanceSourceVerifier) error {
	if verifier == nil || !canonical(evidence) {
		return ErrConflict
	}
	if err := verifier.VerifyMaintenanceSources(ctx, m, evidence); err != nil {
		return err
	}
	backend, id, epoch := m.Backend, m.ID, m.Epoch
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance_carriers m LEFT JOIN carriers c ON c.incarnation=m.incarnation LEFT JOIN stop_receipts s ON s.incarnation=m.incarnation AND s.epoch=m.owner_epoch WHERE m.maintenance_id=? AND (c.incarnation IS NULL OR c.stopped<>1 OR s.incarnation IS NULL)`, id).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return ErrMaintenanceNotQuiescent
	}
	result, err := tx.ExecContext(ctx, `UPDATE backend_maintenance SET phase='quiescent',source_evidence=? WHERE backend=? AND maintenance_id=? AND epoch=? AND phase IN ('draining','quiescent')`, evidence, backend, id, epoch)
	if err != nil {
		return err
	}
	n64, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n64 != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

// MarkMaintenanceInstalling is durable before the installer starts. A restart
// in this phase is unknown and may only be resolved using installed evidence.
func (r *Registry) MarkMaintenanceInstalling(ctx context.Context, backend, id string, epoch int64) error {
	result, err := r.db.ExecContext(ctx, `UPDATE backend_maintenance SET phase='installing' WHERE backend=? AND maintenance_id=? AND epoch=? AND phase='quiescent' AND source_evidence<>''`, backend, id, epoch)
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

type MaintenanceInstallVerifier interface {
	VerifyMaintenanceInstall(context.Context, Maintenance, string) error
}

// MaintenanceUnsentVerifier must prove the install owner durably remained
// before its start-intent boundary. A missing process or timeout is not proof.
type MaintenanceUnsentVerifier interface {
	WithMaintenanceInstallUnsent(context.Context, Maintenance, string, func() error) error
}

func (r *Registry) RecoverMaintenanceInstallUnsent(ctx context.Context, m Maintenance, evidence string, verifier MaintenanceUnsentVerifier) error {
	if verifier == nil || !canonical(evidence) {
		return ErrConflict
	}
	return verifier.WithMaintenanceInstallUnsent(ctx, m, evidence, func() error {
		return r.recoverMaintenanceInstallUnsent(ctx, m)
	})
}

func (r *Registry) recoverMaintenanceInstallUnsent(ctx context.Context, m Maintenance) error {
	result, err := r.db.ExecContext(ctx, `UPDATE backend_maintenance SET phase='quiescent' WHERE backend=? AND maintenance_id=? AND epoch=? AND target_version=? AND phase='installing' AND source_evidence<>''`, m.Backend, m.ID, m.Epoch, m.TargetVersion)
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

// MarkMaintenanceInstalled requires trusted exact-version evidence. It does
// not release the gate or assume any carrier has been rebuilt.
func (r *Registry) MarkMaintenanceInstalled(ctx context.Context, m Maintenance, evidence string, verifier MaintenanceInstallVerifier) error {
	if verifier == nil || !canonical(evidence) {
		return ErrConflict
	}
	if err := verifier.VerifyMaintenanceInstall(ctx, m, evidence); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE backend_maintenance SET phase='restoring',evidence_ref=? WHERE backend=? AND maintenance_id=? AND epoch=? AND target_version=? AND (phase IN ('installing','install_unknown') OR (phase='restoring' AND evidence_ref=?))`, evidence, m.Backend, m.ID, m.Epoch, m.TargetVersion, evidence)
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

func (r *Registry) MaintenanceStatus(ctx context.Context, backend string) (Maintenance, error) {
	var m Maintenance
	var at string
	err := r.db.QueryRowContext(ctx, `SELECT backend,maintenance_id,epoch,phase,target_version,evidence_ref,created_at FROM backend_maintenance WHERE backend=?`, backend).Scan(&m.Backend, &m.ID, &m.Epoch, &m.Phase, &m.TargetVersion, &m.EvidenceRef, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.CreatedAt, err = time.Parse(time.RFC3339Nano, at)
	return m, err
}

// RegisterMaintenanceRecovery allows only a replacement for one frozen,
// stopped carrier after the new binary has been verified. It cannot be used
// for fresh business admission or to change application ownership.
func (r *Registry) RegisterMaintenanceRecovery(ctx context.Context, m Maintenance, old Registration, next Registration) error {
	if err := old.validate(); err != nil {
		return err
	}
	if err := next.validate(); err != nil {
		return err
	}
	if !maintenanceRecoveryOwner(old.Owner, next.Owner) || old.Carrier.Backend != m.Backend || next.Carrier.Backend != m.Backend || old.Carrier.Namespace != next.Carrier.Namespace || old.Carrier.Incarnation == next.Carrier.Incarnation || next.Carrier.Generation <= old.Carrier.Generation {
		return ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance m JOIN backend_maintenance_carriers x ON x.maintenance_id=m.maintenance_id JOIN carriers c ON c.incarnation=x.incarnation WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.phase='restoring' AND x.incarnation=? AND x.instance_id=? AND x.backend=? AND x.namespace=? AND x.generation=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=? AND x.disposition='' AND c.stopped=1`, m.Backend, m.ID, m.Epoch, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if old.Owner.Epoch != next.Owner.Epoch {
		// The stop receipt and frozen plan authorize this one team controller
		// epoch transition; normal session admission remains unchanged.
		var stopEvidence string
		if err = tx.QueryRowContext(ctx, `SELECT evidence_ref FROM stop_receipts WHERE incarnation=? AND epoch=?`, old.Carrier.Incarnation, old.Owner.Epoch).Scan(&stopEvidence); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO transfers(incarnation,old_app,old_owner,old_epoch,new_app,new_owner,new_epoch,evidence_ref) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, old.Carrier.Incarnation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, next.Owner.Application, next.Owner.ID, next.Owner.Epoch, stopEvidence)
		if err != nil {
			return err
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM transfers WHERE incarnation=? AND old_app=? AND old_owner=? AND old_epoch=? AND new_app=? AND new_owner=? AND new_epoch=? AND evidence_ref=?`, old.Carrier.Incarnation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, next.Owner.Application, next.Owner.ID, next.Owner.Epoch, stopEvidence).Scan(&count); err != nil || count != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO session_transfers SELECT backend,namespace,session_id,incarnation,epoch,?,?,? FROM sessions WHERE incarnation=? AND epoch=? AND active=0 ON CONFLICT DO NOTHING`, next.Owner.Application, next.Owner.ID, next.Owner.Epoch, old.Carrier.Incarnation, old.Owner.Epoch)
		if err != nil {
			return err
		}
	}
	c, o := next.Carrier, next.Owner
	_, err = tx.ExecContext(ctx, `INSERT INTO carriers(instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch)
	if err != nil {
		return err
	}
	var got Registration
	var stopped int
	err = tx.QueryRowContext(ctx, `SELECT instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch,stopped FROM carriers WHERE incarnation=?`, c.Incarnation).Scan(&got.Carrier.InstanceID, &got.Carrier.Incarnation, &got.Carrier.Backend, &got.Carrier.Namespace, &got.Carrier.Generation, &got.Owner.Application, &got.Owner.ID, &got.Owner.Epoch, &stopped)
	if err != nil {
		return err
	}
	if got != next || stopped != 0 {
		return ErrConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE backend_maintenance_carriers SET rebuilt_incarnation=? WHERE maintenance_id=? AND incarnation=? AND disposition='' AND (rebuilt_incarnation='' OR rebuilt_incarnation=?)`, next.Carrier.Incarnation, m.ID, old.Carrier.Incarnation, next.Carrier.Incarnation)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrConflict
	}
	return tx.Commit()
}

func maintenanceRecoveryOwner(old, next Owner) bool {
	if old.Application == Private {
		return old == next
	}
	return old.Application == Team && next.Application == Team && old.ID == next.ID && next.Epoch == old.Epoch+1
}

type MaintenanceUnspawnedVerifier interface {
	WithMaintenanceUnspawned(context.Context, Maintenance, Registration, Registration, string, func() error) error
}

// RegisterMaintenanceUnspawned is only for an exact team launch proven never
// started and permanently superseded in the team journal. It does not create
// an invented carrier or stop receipt for the old launch.
func (r *Registry) RegisterMaintenanceUnspawned(ctx context.Context, m Maintenance, old, next Registration, evidence string, verifier MaintenanceUnspawnedVerifier) error {
	if verifier == nil || !canonical(evidence) || old.validate() != nil || next.validate() != nil || old.Owner.Application != Team || !maintenanceRecoveryOwner(old.Owner, next.Owner) || old.Carrier.Backend != m.Backend || next.Carrier.Backend != m.Backend || old.Carrier.Namespace != next.Carrier.Namespace || next.Carrier.Generation != old.Carrier.Generation+1 || old.Carrier.Incarnation == next.Carrier.Incarnation || old.Carrier.InstanceID == next.Carrier.InstanceID {
		return ErrConflict
	}
	return verifier.WithMaintenanceUnspawned(ctx, m, old, next, evidence, func() error {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance WHERE backend=? AND maintenance_id=? AND epoch=? AND target_version=? AND phase='restoring'`, m.Backend, m.ID, m.Epoch, m.TargetVersion).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM carriers WHERE incarnation=? OR instance_id=?`, old.Carrier.Incarnation, old.Carrier.InstanceID).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrConflict
		}
		c, o := next.Carrier, next.Owner
		if _, err = tx.ExecContext(ctx, `INSERT INTO carriers(instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch); err != nil {
			return err
		}
		var actual Registration
		var stopped int
		if err = tx.QueryRowContext(ctx, `SELECT instance_id,incarnation,backend,namespace,generation,app,owner_id,epoch,stopped FROM carriers WHERE incarnation=?`, c.Incarnation).Scan(&actual.Carrier.InstanceID, &actual.Carrier.Incarnation, &actual.Carrier.Backend, &actual.Carrier.Namespace, &actual.Carrier.Generation, &actual.Owner.Application, &actual.Owner.ID, &actual.Owner.Epoch, &stopped); err != nil {
			return err
		}
		if actual != next || stopped != 0 {
			return ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO backend_maintenance_carriers(maintenance_id,incarnation,instance_id,backend,namespace,generation,app,owner_id,owner_epoch,rebuilt_incarnation) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, m.ID, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, next.Carrier.Incarnation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO backend_maintenance_unspawned(maintenance_id,incarnation,evidence) VALUES(?,?,?) ON CONFLICT DO NOTHING`, m.ID, old.Carrier.Incarnation, evidence); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance_carriers x JOIN backend_maintenance_unspawned u ON u.maintenance_id=x.maintenance_id AND u.incarnation=x.incarnation WHERE x.maintenance_id=? AND x.incarnation=? AND x.instance_id=? AND x.backend=? AND x.namespace=? AND x.generation=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=? AND x.rebuilt_incarnation=? AND u.evidence=?`, m.ID, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, next.Carrier.Incarnation, evidence).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrConflict
		}
		return tx.Commit()
	})
}

// ReserveMaintenanceUnspawnedSession requires a previously stopped, exactly
// owned session; an unsent worker snapshot alone never establishes ownership.
func (r *Registry) ReserveMaintenanceUnspawnedSession(ctx context.Context, m Maintenance, old, next Registration, session, evidence string, verifier MaintenanceUnspawnedVerifier) error {
	if verifier == nil || !canonical(session) || !canonical(evidence) || old.validate() != nil || next.validate() != nil || !maintenanceRecoveryOwner(old.Owner, next.Owner) {
		return ErrConflict
	}
	return verifier.WithMaintenanceUnspawned(ctx, m, old, next, evidence, func() error {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		var n int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance m JOIN backend_maintenance_carriers x ON x.maintenance_id=m.maintenance_id JOIN backend_maintenance_unspawned u ON u.maintenance_id=x.maintenance_id AND u.incarnation=x.incarnation JOIN carriers c ON c.incarnation=x.rebuilt_incarnation WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.target_version=? AND m.phase='restoring' AND x.incarnation=? AND x.instance_id=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=? AND x.rebuilt_incarnation=? AND x.disposition='' AND u.evidence=? AND c.instance_id=? AND c.stopped=0`, m.Backend, m.ID, m.Epoch, m.TargetVersion, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, next.Carrier.Incarnation, evidence, next.Carrier.InstanceID).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		// The native hello may already have bound the session before a crash.
		// Replay must recognize that exact committed transfer without treating
		// the now-active replacement as the old stopped source.
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance_session_transfers t JOIN sessions s ON s.backend=t.backend AND s.namespace=t.namespace AND s.session_id=t.session_id WHERE t.maintenance_id=? AND t.backend=? AND t.namespace=? AND t.session_id=? AND t.new_incarnation=? AND t.new_app=? AND t.new_owner=? AND t.new_epoch=? AND s.incarnation=t.new_incarnation AND s.app=t.new_app AND s.owner_id=t.new_owner AND s.epoch=t.new_epoch AND s.active=1`, m.ID, next.Carrier.Backend, next.Carrier.Namespace, session, next.Carrier.Incarnation, next.Owner.Application, next.Owner.ID, next.Owner.Epoch).Scan(&n)
		if err != nil {
			return err
		}
		if n == 1 {
			return tx.Commit()
		}
		var source string
		var sourceEpoch int64
		err = tx.QueryRowContext(ctx, `SELECT s.incarnation,s.epoch FROM sessions s JOIN stop_receipts p ON p.incarnation=s.incarnation AND p.epoch=s.epoch JOIN carriers c ON c.incarnation=s.incarnation AND c.stopped=1 WHERE s.backend=? AND s.namespace=? AND s.session_id=? AND s.active=0 AND ((s.app=? AND s.owner_id=? AND s.epoch=?) OR EXISTS(SELECT 1 FROM session_transfers t WHERE t.backend=s.backend AND t.namespace=s.namespace AND t.session_id=s.session_id AND t.incarnation=s.incarnation AND t.old_epoch=s.epoch AND t.new_app=? AND t.new_owner=? AND t.new_epoch=?))`, old.Carrier.Backend, old.Carrier.Namespace, session, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, old.Owner.Application, old.Owner.ID, old.Owner.Epoch).Scan(&source, &sourceEpoch)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO backend_maintenance_session_transfers(maintenance_id,backend,namespace,session_id,source_incarnation,source_epoch,new_incarnation,new_app,new_owner,new_epoch) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, m.ID, old.Carrier.Backend, old.Carrier.Namespace, session, source, sourceEpoch, next.Carrier.Incarnation, next.Owner.Application, next.Owner.ID, next.Owner.Epoch)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance_session_transfers WHERE maintenance_id=? AND backend=? AND namespace=? AND session_id=? AND source_incarnation=? AND source_epoch=? AND new_incarnation=? AND new_app=? AND new_owner=? AND new_epoch=?`, m.ID, old.Carrier.Backend, old.Carrier.Namespace, session, source, sourceEpoch, next.Carrier.Incarnation, next.Owner.Application, next.Owner.ID, next.Owner.Epoch).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO session_reservations(backend,namespace,session_id,incarnation,epoch,app,owner_id) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, next.Carrier.Backend, next.Carrier.Namespace, session, next.Carrier.Incarnation, next.Owner.Epoch, next.Owner.Application, next.Owner.ID)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM session_reservations WHERE backend=? AND namespace=? AND session_id=? AND incarnation=? AND epoch=? AND app=? AND owner_id=?`, next.Carrier.Backend, next.Carrier.Namespace, session, next.Carrier.Incarnation, next.Owner.Epoch, next.Owner.Application, next.Owner.ID).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		return tx.Commit()
	})
}

type MaintenanceRecoveryVerifier interface {
	WithMaintenanceRecovery(context.Context, Maintenance, Registration, *Registration, string, func() error) error
}

// MarkMaintenanceRecovered requires trusted evidence of the rebuilt carrier's
// exact readiness, or of an intentional terminal retirement when next is nil.
func (r *Registry) MarkMaintenanceRecovered(ctx context.Context, m Maintenance, old Registration, next *Registration, evidence string, verifier MaintenanceRecoveryVerifier) error {
	if verifier == nil || !canonical(evidence) {
		return ErrConflict
	}
	if err := old.validate(); err != nil {
		return err
	}
	if next != nil {
		if err := next.validate(); err != nil {
			return err
		}
		if !maintenanceRecoveryOwner(old.Owner, next.Owner) || next.Carrier.Backend != old.Carrier.Backend || next.Carrier.Namespace != old.Carrier.Namespace || next.Carrier.Generation <= old.Carrier.Generation {
			return ErrConflict
		}
	}
	return verifier.WithMaintenanceRecovery(ctx, m, old, next, evidence, func() error {
		return r.markMaintenanceRecovered(ctx, m, old, next, evidence)
	})
}

// markMaintenanceRecovered runs only inside the trusted verifier's readiness
// lease. The lease must not span network or process I/O.
func (r *Registry) markMaintenanceRecovered(ctx context.Context, m Maintenance, old Registration, next *Registration, evidence string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rebuilt, disposition := "", "retired"
	if next != nil {
		rebuilt, disposition = next.Carrier.Incarnation, "rebuilt"
		var n int
		c, o := next.Carrier, next.Owner
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM carriers WHERE instance_id=? AND incarnation=? AND backend=? AND namespace=? AND generation=? AND app=? AND owner_id=? AND epoch=? AND stopped=0`, c.InstanceID, c.Incarnation, c.Backend, c.Namespace, c.Generation, o.Application, o.ID, o.Epoch).Scan(&n)
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
	}
	var candidate string
	if err = tx.QueryRowContext(ctx, `SELECT rebuilt_incarnation FROM backend_maintenance_carriers WHERE maintenance_id=? AND incarnation=?`, m.ID, old.Carrier.Incarnation).Scan(&candidate); err != nil {
		return err
	}
	if next != nil && candidate != next.Carrier.Incarnation {
		return ErrConflict
	}
	if next == nil && candidate != "" {
		var stopped int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM carriers c JOIN stop_receipts s ON s.incarnation=c.incarnation AND s.epoch=c.epoch WHERE c.incarnation=? AND c.stopped=1`, candidate).Scan(&stopped); err != nil {
			return err
		}
		if stopped != 1 {
			return ErrMaintenanceNotQuiescent
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE backend_maintenance_carriers SET rebuilt_incarnation=?,disposition=?,recovery_evidence=? WHERE maintenance_id=? AND incarnation=? AND instance_id=? AND backend=? AND namespace=? AND generation=? AND app=? AND owner_id=? AND owner_epoch=? AND (disposition='' OR (disposition=? AND rebuilt_incarnation=? AND recovery_evidence=?)) AND EXISTS (SELECT 1 FROM backend_maintenance WHERE backend=? AND maintenance_id=? AND epoch=? AND phase='restoring')`, rebuilt, disposition, evidence, m.ID, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, disposition, rebuilt, evidence, m.Backend, m.ID, m.Epoch)
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

// ReleaseMaintenance opens business admission only after every frozen carrier
// has an independently verified recovery or retirement disposition.
type MaintenanceReleaseVerifier interface {
	WithMaintenanceRelease(context.Context, Maintenance, string, func() error) error
}

func (r *Registry) ReleaseMaintenance(ctx context.Context, m Maintenance, evidence string, verifier MaintenanceReleaseVerifier) error {
	if verifier == nil || !canonical(evidence) {
		return ErrConflict
	}
	return verifier.WithMaintenanceRelease(ctx, m, evidence, func() error {
		return r.releaseMaintenance(ctx, m, evidence)
	})
}

func (r *Registry) releaseMaintenance(ctx context.Context, m Maintenance, evidence string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var phase, releaseEvidence string
	err = tx.QueryRowContext(ctx, `SELECT m.phase,coalesce(r.evidence,'') FROM backend_maintenance m LEFT JOIN backend_maintenance_releases r ON r.maintenance_id=m.maintenance_id WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.target_version=?`, m.Backend, m.ID, m.Epoch, m.TargetVersion).Scan(&phase, &releaseEvidence)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if phase == "released" {
		if releaseEvidence != evidence {
			return ErrConflict
		}
		return tx.Commit()
	}
	var n int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance_carriers x LEFT JOIN carriers c ON c.incarnation=x.rebuilt_incarnation WHERE x.maintenance_id=? AND (x.disposition='' OR (x.disposition='rebuilt' AND (c.incarnation IS NULL OR c.stopped<>0)))`, m.ID).Scan(&n)
	if err != nil {
		return err
	}
	if n != 0 {
		return ErrMaintenanceNotQuiescent
	}
	result, err := tx.ExecContext(ctx, `UPDATE backend_maintenance SET phase='released' WHERE backend=? AND maintenance_id=? AND epoch=? AND target_version=? AND evidence_ref<>'' AND phase='restoring'`, m.Backend, m.ID, m.Epoch, m.TargetVersion)
	if err != nil {
		return err
	}
	n64, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n64 != 1 {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO backend_maintenance_releases(maintenance_id,evidence) VALUES(?,?)`, m.ID, evidence); err != nil {
		return err
	}
	return tx.Commit()
}

// MaintenanceRestartVerifier confirms the exclusive daemon lifecycle has
// changed. Opening another registry connection alone is not restart evidence.
type MaintenanceRestartVerifier interface {
	VerifyMaintenanceRestart(context.Context, Maintenance, string) error
}

func (r *Registry) MarkMaintenanceInstallUnknown(ctx context.Context, m Maintenance, evidence string, verifier MaintenanceRestartVerifier) error {
	if verifier == nil || !canonical(evidence) {
		return ErrConflict
	}
	if err := verifier.VerifyMaintenanceRestart(ctx, m, evidence); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE backend_maintenance SET phase='install_unknown',evidence_ref=? WHERE backend=? AND maintenance_id=? AND epoch=? AND target_version=? AND phase='installing'`, evidence, m.Backend, m.ID, m.Epoch, m.TargetVersion)
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

// ReserveMaintenanceSession permits only an existing session owned by the
// exact frozen carrier to be resumed by its recorded replacement. Ordinary
// fresh session admission remains fenced until ReleaseMaintenance.
func (r *Registry) ReserveMaintenanceSession(ctx context.Context, m Maintenance, old, next Registration, session string) error {
	if !canonical(session) || old.validate() != nil || next.validate() != nil || !maintenanceRecoveryOwner(old.Owner, next.Owner) {
		return ErrConflict
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM backend_maintenance m JOIN backend_maintenance_carriers x ON x.maintenance_id=m.maintenance_id JOIN sessions s ON s.incarnation=x.incarnation AND s.epoch=x.owner_epoch AND s.backend=x.backend AND s.namespace=x.namespace AND s.app=x.app AND s.owner_id=x.owner_id WHERE m.backend=? AND m.maintenance_id=? AND m.epoch=? AND m.phase='restoring' AND x.incarnation=? AND x.instance_id=? AND x.backend=? AND x.namespace=? AND x.generation=? AND x.app=? AND x.owner_id=? AND x.owner_epoch=? AND x.rebuilt_incarnation=? AND x.disposition='' AND s.session_id=? AND s.active=0`, m.Backend, m.ID, m.Epoch, old.Carrier.Incarnation, old.Carrier.InstanceID, old.Carrier.Backend, old.Carrier.Namespace, old.Carrier.Generation, old.Owner.Application, old.Owner.ID, old.Owner.Epoch, next.Carrier.Incarnation, session).Scan(&n)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	if next.Carrier.Backend != old.Carrier.Backend || next.Carrier.Namespace != old.Carrier.Namespace || next.Carrier.Generation <= old.Carrier.Generation {
		return ErrConflict
	}
	if err = reserveSessionTxUnchecked(ctx, tx, next, session); err != nil {
		return err
	}
	return tx.Commit()
}
