package ingressowner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

var ErrOwnerDraining = errors.New("ingress owner migration is draining")
var ErrAdmissionUncertain = errors.New("source application admission requires reconciliation")

type DrainProof struct {
	Owner             Owner  `json:"owner"`
	MigrationID       string `json:"migration_id"`
	Revision          int64  `json:"revision"`
	AcceptedWatermark int64  `json:"accepted_watermark"`
	BindingDigest     string `json:"binding_digest,omitempty"`
	CoverageDigest    string `json:"coverage_digest,omitempty"`
	Digest            string `json:"digest"`
}
type ImportProof struct {
	Drain              DrainProof `json:"drain"`
	SourceSnapshotHash string     `json:"source_snapshot_hash"`
	Target             Owner      `json:"target"`
	TransactionID      string     `json:"transaction_id"`
}

// MigrationVerifier checks the committed destination import in its own trusted
// store. Caller-supplied hashes and IDs are not themselves proof.
type MigrationVerifier interface {
	VerifyImport(context.Context, ImportProof) error
}

// TargetReadyVerifier rechecks a durable target receipt against the exact
// import. It must not reenter this router.
type TargetReadyVerifier interface {
	VerifyTargetReady(context.Context, ImportProof, []byte) error
}

func (r *Router) SetTargetReadyVerifier(v TargetReadyVerifier) {
	r.mu.Lock()
	r.targetReadyVerifier = v
	r.mu.Unlock()
}

// ActivateMigrationTarget opens delivery only after the target has durably
// released its migration holds and a trusted verifier rereads that fact.
func (r *Router) ActivateMigrationTarget(ctx context.Context, p ImportProof, proof []byte) error {
	if len(proof) == 0 {
		return ErrInvalid
	}
	return r.WithCommittedOwnerMigration(ctx, p, func() error {
		var existingID string
		var existingProof []byte
		err := r.db.QueryRowContext(ctx, `SELECT migration_id,proof FROM migration_target_ready WHERE owner=?`, ownerJSON(p.Drain.Owner)).Scan(&existingID, &existingProof)
		if err == nil {
			if existingID == p.Drain.MigrationID && string(existingProof) == string(proof) {
				return nil
			}
			return ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		r.mu.Lock()
		verifier := r.targetReadyVerifier
		r.mu.Unlock()
		if verifier == nil {
			return ErrInvalid
		}
		if err := verifier.VerifyTargetReady(ctx, p, proof); err != nil {
			return err
		}
		_, err = r.db.ExecContext(ctx, `INSERT INTO migration_target_ready(owner,migration_id,proof) VALUES(?,?,?) ON CONFLICT(owner) DO NOTHING`, ownerJSON(p.Drain.Owner), p.Drain.MigrationID, proof)
		if err != nil {
			return err
		}
		var id string
		var stored []byte
		if err = r.db.QueryRowContext(ctx, `SELECT migration_id,proof FROM migration_target_ready WHERE owner=?`, ownerJSON(p.Drain.Owner)).Scan(&id, &stored); err != nil {
			return err
		}
		if id != p.Drain.MigrationID || string(stored) != string(proof) {
			return ErrConflict
		}
		return nil
	})
}

func (r *Router) verifyTargetReady(ctx context.Context, owner Owner) error {
	var bound int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM owner_scope_bindings WHERE owner=?`, ownerJSON(owner)).Scan(&bound); err != nil {
		return err
	}
	if bound == 0 {
		return nil
	}
	var ready int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM migration_target_ready r JOIN owner_migrations m ON m.owner=r.owner AND m.migration_id=r.migration_id WHERE r.owner=? AND m.state='transferred'`, ownerJSON(owner)).Scan(&ready); err != nil {
		return err
	}
	if ready != 1 {
		return ErrOwnerDraining
	}
	return nil
}

func (r *Router) SetMigrationVerifier(v MigrationVerifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.migrationVerifier = v
}
func ownerJSON(o Owner) []byte { b, _ := json.Marshal(o); return b }
func drainDigest(p DrainProof) string {
	p.Digest = ""
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// BeginOwnerMigration persists the fence before waiting for all prior source
// admissions to exit. Unknown outcomes must be reconciled with the old source.
func (r *Router) BeginOwnerMigration(ctx context.Context, owner Owner, id string) (DrainProof, error) {
	var proof DrainProof
	if r == nil || !owner.valid() || id == "" {
		return proof, ErrInvalid
	}
	raw := ownerJSON(owner)
	_, err := r.db.ExecContext(ctx, `INSERT INTO owner_migrations(owner,migration_id,state,revision,watermark) VALUES(?,?,'draining',1,-1) ON CONFLICT(owner) DO NOTHING`, raw, id)
	if err != nil {
		return proof, err
	}
	unlock := r.acquireOwner(owner)
	defer unlock()
	var state, storedID string
	var rev, watermark int64
	if err = r.db.QueryRowContext(ctx, `SELECT migration_id,state,revision,watermark FROM owner_migrations WHERE owner=?`, raw).Scan(&storedID, &state, &rev, &watermark); err != nil {
		return proof, err
	}
	if storedID != id || state != "draining" {
		return proof, ErrConflict
	}
	if _, err = r.db.ExecContext(ctx, `UPDATE delivery_attempts SET state='confirmed' WHERE state='unknown' AND seq IN (SELECT seq FROM events WHERE owner=? AND state='accepted')`, raw); err != nil {
		return proof, err
	}
	var uncertain int
	if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM events e LEFT JOIN delivery_attempts a ON a.seq=e.seq WHERE e.owner=? AND (e.state='delivering' OR a.state='unknown')`, raw).Scan(&uncertain); err != nil {
		return proof, err
	}
	if uncertain != 0 {
		return proof, ErrAdmissionUncertain
	}
	var current int64
	if err = r.db.QueryRowContext(ctx, `SELECT coalesce(max(seq),0) FROM events WHERE owner=? AND state='accepted'`, raw).Scan(&current); err != nil {
		return proof, err
	}
	if watermark < 0 {
		if _, err = r.db.ExecContext(ctx, `UPDATE owner_migrations SET watermark=?,revision=revision+1 WHERE owner=? AND state='draining' AND watermark=-1`, current, raw); err != nil {
			return proof, err
		}
		watermark = current
		rev++
	} else if current > watermark {
		return proof, ErrConflict
	}
	proof = DrainProof{Owner: owner, MigrationID: id, Revision: rev, AcceptedWatermark: watermark}
	proof.Digest = drainDigest(proof)
	return proof, nil
}
func (r *Router) VerifyOwnerMigrationDrain(ctx context.Context, owner Owner, id string, proof DrainProof) error {
	if r == nil || !owner.valid() || id == "" || proof.Owner != owner || proof.MigrationID != id || proof.Digest != drainDigest(proof) {
		return ErrInvalid
	}
	unlock := r.acquireOwner(owner)
	defer unlock()
	return r.verifyDrainLocked(ctx, proof)
}
func (r *Router) verifyDrainLocked(ctx context.Context, p DrainProof) error {
	var id, state string
	var rev, watermark int64
	if err := r.db.QueryRowContext(ctx, `SELECT migration_id,state,revision,watermark FROM owner_migrations WHERE owner=?`, ownerJSON(p.Owner)).Scan(&id, &state, &rev, &watermark); err != nil {
		return err
	}
	if id != p.MigrationID || state != "draining" || rev != p.Revision || watermark != p.AcceptedWatermark || watermark < 0 {
		return ErrConflict
	}
	if p.BindingDigest != "" {
		if err := r.verifyOwnerScopeLocked(ctx, p.Owner, p.BindingDigest); err != nil {
			return err
		}
	}
	if p.CoverageDigest != "" {
		if err := r.verifyBoundCoverageLocked(ctx, p.Owner, p.CoverageDigest); err != nil {
			return err
		}
	}
	var uncertain int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM events e LEFT JOIN delivery_attempts a ON a.seq=e.seq WHERE e.owner=? AND (e.state='delivering' OR a.state='unknown' OR (e.state='accepted' AND e.seq>?))`, ownerJSON(p.Owner), watermark).Scan(&uncertain); err != nil {
		return err
	}
	if uncertain != 0 {
		return ErrAdmissionUncertain
	}
	return nil
}
func deliveryOwner(record Record) Owner {
	if record.DeliveryTarget != nil {
		return *record.DeliveryTarget
	}
	return *record.Owner
}
func (r *Router) allowOwnerDelivery(ctx context.Context, record Record, reconcile bool) error {
	var state string
	err := r.db.QueryRowContext(ctx, `SELECT state FROM owner_migrations WHERE owner=?`, ownerJSON(*record.Owner)).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if state == "draining" && reconcile {
		var status string
		if err = r.db.QueryRowContext(ctx, `SELECT state FROM delivery_attempts WHERE seq=?`, record.Sequence).Scan(&status); err == nil && status == "unknown" {
			return nil
		}
		return ErrOwnerDraining
	}
	if state == "draining" {
		return ErrOwnerDraining
	}
	if state == "transferred" && record.DeliveryTarget != nil {
		return r.verifyTargetReady(ctx, *record.Owner)
	}
	return ErrOwnerDraining
}

// CommitOwnerMigration fences the source and records the verified target in one
// transaction. Individual pending events still require explicit mapping.
func (r *Router) CommitOwnerMigration(ctx context.Context, p ImportProof) error {
	if r == nil || p.Drain.Digest != drainDigest(p.Drain) || !p.Target.valid() || p.Target.Epoch <= p.Drain.Owner.Epoch || p.TransactionID == "" || p.SourceSnapshotHash == "" {
		return ErrInvalid
	}
	unlock := r.acquireOwner(p.Drain.Owner)
	defer unlock()
	var existingState string
	var existingReceipt []byte
	if err := r.db.QueryRowContext(ctx, `SELECT state,import_receipt FROM owner_migrations WHERE owner=?`, ownerJSON(p.Drain.Owner)).Scan(&existingState, &existingReceipt); err != nil {
		return err
	}
	if existingState == "transferred" {
		expected, _ := json.Marshal(p)
		if string(existingReceipt) == string(expected) {
			return nil
		}
		return ErrConflict
	}
	if err := r.verifyDrainLocked(ctx, p.Drain); err != nil {
		return err
	}
	var bound int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM owner_scope_bindings WHERE owner=?`, ownerJSON(p.Drain.Owner)).Scan(&bound); err != nil {
		return err
	}
	if bound != 0 {
		if p.Drain.BindingDigest == "" || p.Drain.CoverageDigest == "" {
			return ErrConflict
		}
		if err := r.verifyHistoricalDrainLocked(ctx, p.Drain.Owner); err != nil {
			return err
		}
	}
	r.mu.Lock()
	verifier := r.migrationVerifier
	r.mu.Unlock()
	if verifier == nil {
		return ErrInvalid
	}
	if err := verifier.VerifyImport(ctx, p); err != nil {
		return err
	}
	target, _ := json.Marshal(p.Target)
	receipt, _ := json.Marshal(p)
	result, err := r.db.ExecContext(ctx, `UPDATE owner_migrations SET state='transferred',target=?,import_receipt=?,revision=revision+1 WHERE owner=? AND state='draining' AND migration_id=? AND revision=? AND watermark=?`, target, receipt, ownerJSON(p.Drain.Owner), p.Drain.MigrationID, p.Drain.Revision, p.Drain.AcceptedWatermark)
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

// VerifyCommittedOwnerMigration verifies the exact committed import receipt
// while holding the source owner lock. Verifiers called here must not reenter
// the router; coverage and source/target facts are checked again on each read.
func (r *Router) VerifyCommittedOwnerMigration(ctx context.Context, p ImportProof) error {
	return r.WithCommittedOwnerMigration(ctx, p, func() error { return nil })
}

// WithCommittedOwnerMigration keeps the source-owner fence across the target's
// local durable commit. The callback must not perform network I/O or reenter
// the router.
func (r *Router) WithCommittedOwnerMigration(ctx context.Context, p ImportProof, commit func() error) error {
	if r == nil || p.Drain.Digest != drainDigest(p.Drain) || !p.Target.valid() || p.Target.Epoch <= p.Drain.Owner.Epoch || p.TransactionID == "" || p.SourceSnapshotHash == "" || p.Drain.BindingDigest == "" || p.Drain.CoverageDigest == "" {
		return ErrInvalid
	}
	if commit == nil {
		return ErrInvalid
	}
	unlock := r.acquireOwner(p.Drain.Owner)
	defer unlock()
	var state string
	var raw []byte
	if err := r.db.QueryRowContext(ctx, `SELECT state,import_receipt FROM owner_migrations WHERE owner=?`, ownerJSON(p.Drain.Owner)).Scan(&state, &raw); err != nil {
		return err
	}
	expected, err := json.Marshal(p)
	if err != nil || state != "transferred" || string(raw) != string(expected) {
		return ErrConflict
	}
	if err = r.verifyOwnerScopeLocked(ctx, p.Drain.Owner, p.Drain.BindingDigest); err != nil {
		return err
	}
	if err = r.verifyBoundCoverageLocked(ctx, p.Drain.Owner, p.Drain.CoverageDigest); err != nil {
		return err
	}
	r.mu.Lock()
	verifier := r.migrationVerifier
	r.mu.Unlock()
	if verifier == nil {
		return ErrInvalid
	}
	if err = verifier.VerifyImport(ctx, p); err != nil {
		return err
	}
	return commit()
}

// MapPendingDelivery preserves the frozen event owner. Cards and observations
// need separate verified target-view mapping and deliberately remain held.
func (r *Router) MapPendingDelivery(ctx context.Context, k Key, migrationID string) error {
	unlock := r.acquire(k)
	defer unlock()
	record, err := r.get(ctx, k)
	if err != nil {
		return err
	}
	if record.State != Pending || record.Owner == nil || record.Event.Kind != Message {
		return ErrConflict
	}
	release := r.acquireOwner(*record.Owner)
	defer release()
	var state, id string
	var target []byte
	if err = r.db.QueryRowContext(ctx, `SELECT state,migration_id,target FROM owner_migrations WHERE owner=?`, ownerJSON(*record.Owner)).Scan(&state, &id, &target); err != nil {
		return err
	}
	if state != "transferred" || id != migrationID {
		return ErrConflict
	}
	if err = r.verifyTargetReady(ctx, *record.Owner); err != nil {
		return err
	}
	var parsed Owner
	if json.Unmarshal(target, &parsed) != nil || !parsed.valid() {
		return ErrConflict
	}
	if parsed.Application == Private && record.Event.ChatType != "p2p" {
		return ErrConflict
	}
	_, err = r.db.ExecContext(ctx, `INSERT INTO delivery_targets(seq,migration_id,target) VALUES(?,?,?) ON CONFLICT(seq) DO NOTHING`, record.Sequence, id, target)
	if err != nil {
		return err
	}
	var oldID string
	var oldTarget []byte
	if err = r.db.QueryRowContext(ctx, `SELECT migration_id,target FROM delivery_targets WHERE seq=?`, record.Sequence).Scan(&oldID, &oldTarget); err != nil {
		return err
	}
	if oldID != id || string(oldTarget) != string(target) {
		return ErrConflict
	}
	return nil
}

// CommittedMigration exposes only a verified import's durable routing target;
// callers still must record an explicit mapping for each pending source event.
func (r *Router) CommittedMigration(ctx context.Context, owner Owner) (string, Owner, error) {
	var target Owner
	var id, state string
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT migration_id,state,target FROM owner_migrations WHERE owner=?`, ownerJSON(owner)).Scan(&id, &state, &raw)
	if err != nil {
		return "", target, err
	}
	if state != "transferred" {
		return "", target, ErrOwnerDraining
	}
	if err := r.verifyTargetReady(ctx, owner); err != nil {
		return "", target, err
	}
	if json.Unmarshal(raw, &target) != nil || !target.valid() {
		return "", Owner{}, ErrConflict
	}
	return id, target, nil
}

// verifyHistoricalDrainLocked is called only with the source owner lock held.
// It avoids recursively acquiring that lock through the public verifier.
func (r *Router) verifyHistoricalDrainLocked(ctx context.Context, owner Owner) error {
	var id, state string
	var revision, watermark int64
	if err := r.db.QueryRowContext(ctx, `SELECT migration_id,state,revision,watermark FROM owner_migrations WHERE owner=?`, ownerJSON(owner)).Scan(&id, &state, &revision, &watermark); err != nil {
		return err
	}
	if state != "draining" || watermark < 0 {
		return ErrConflict
	}
	var raw []byte
	var bindingDigest string
	if err := r.db.QueryRowContext(ctx, `SELECT binding,digest FROM owner_scope_bindings WHERE owner=?`, ownerJSON(owner)).Scan(&raw, &bindingDigest); err != nil {
		return err
	}
	var binding OwnerScopeBinding
	if json.Unmarshal(raw, &binding) != nil || binding.Owner != owner {
		return ErrConflict
	}
	coverage, err := r.coverageForScope(ctx, binding)
	if err != nil {
		return err
	}
	proof := DrainProof{Owner: owner, MigrationID: id, Revision: revision, AcceptedWatermark: watermark, BindingDigest: bindingDigest, CoverageDigest: coverage}
	proof.Digest = drainDigest(proof)
	return r.verifyDrainLocked(ctx, proof)
}
