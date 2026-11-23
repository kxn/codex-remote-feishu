package ingressowner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// GatewayCoverage binds an independently verified, drained gateway hook
// cutover to this persistent router. Runtime connection counters are not Bot
// identity generations; the verifier must establish real end-to-end coverage.
type GatewayCoverage struct {
	Tenant        string `json:"tenant"`
	AppID         string `json:"app_id"`
	GatewayID     string `json:"gateway_id"`
	BotGeneration int64  `json:"bot_generation"`
	RouterID      string `json:"router_id"`
	Epoch         int64  `json:"epoch"`
	CutoverID     string `json:"cutover_id"`
}
type GatewayCoverageVerifier interface {
	VerifyGatewayCoverage(context.Context, GatewayCoverage, []byte) error
}

func (r *Router) RouterID() string {
	if r == nil {
		return ""
	}
	return r.routerID
}
func (r *Router) SetGatewayCoverageVerifier(v GatewayCoverageVerifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.coverageVerifier = v
}
func (r *Router) validateCoverage(ctx context.Context, c GatewayCoverage, proof []byte) error {
	if r == nil || c.Tenant == "" || c.AppID == "" || c.GatewayID == "" || c.BotGeneration < 1 || c.RouterID != r.routerID || c.Epoch < 1 || c.CutoverID == "" || len(proof) == 0 {
		return ErrInvalid
	}
	r.mu.Lock()
	verifier := r.coverageVerifier
	r.mu.Unlock()
	if verifier == nil {
		return ErrInvalid
	}
	return verifier.VerifyGatewayCoverage(ctx, c, proof)
}

// RecordGatewayCoverage may be called only after the trusted cutover verifier
// proves real hook installation and drain of the previous receive path.
func (r *Router) RecordGatewayCoverage(ctx context.Context, c GatewayCoverage, proof []byte) error {
	if err := r.validateCoverage(ctx, c, proof); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old, oldProof []byte
	var oldDigest string
	err = tx.QueryRowContext(ctx, `SELECT body,proof,digest FROM gateway_coverage WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=?`, c.Tenant, c.AppID, c.GatewayID, c.BotGeneration).Scan(&old, &oldProof, &oldDigest)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO gateway_coverage(tenant,app_id,gateway,binding_generation,body,proof,digest) VALUES(?,?,?,?,?,?,?)`, c.Tenant, c.AppID, c.GatewayID, c.BotGeneration, raw, proof, digest)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	if string(old) == string(raw) && string(oldProof) == string(proof) && oldDigest == digest {
		return tx.Commit()
	}
	var previous GatewayCoverage
	oldSum := sha256.Sum256(old)
	if json.Unmarshal(old, &previous) != nil || hex.EncodeToString(oldSum[:]) != oldDigest || previous.Tenant != c.Tenant || previous.AppID != c.AppID || previous.GatewayID != c.GatewayID || previous.BotGeneration != c.BotGeneration || previous.RouterID != c.RouterID || c.Epoch <= previous.Epoch || digest == oldDigest {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gateway_coverage_lineage(tenant,app_id,gateway,binding_generation,previous_digest,next_digest,previous_body,next_body) VALUES(?,?,?,?,?,?,?,?)`, c.Tenant, c.AppID, c.GatewayID, c.BotGeneration, oldDigest, digest, old, raw)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE gateway_coverage SET body=?,proof=?,digest=? WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND digest=?`, raw, proof, digest, c.Tenant, c.AppID, c.GatewayID, c.BotGeneration, oldDigest)
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

// NextGatewayCoverageEpoch uses the persisted epoch, avoiding dependence on a
// wall clock that can move backwards after restart. A concurrent writer may
// win; RecordGatewayCoverage then rejects the stale selection.
func (r *Router) NextGatewayCoverageEpoch(ctx context.Context, tenant, app, gateway string, generation int64) (int64, error) {
	if r == nil || tenant == "" || app == "" || gateway == "" || generation < 1 {
		return 0, ErrInvalid
	}
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT body FROM gateway_coverage WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=?`, tenant, app, gateway, generation).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	var c GatewayCoverage
	if json.Unmarshal(raw, &c) != nil || c.Epoch < 1 || c.Epoch == int64(^uint64(0)>>1) {
		return 0, ErrConflict
	}
	return c.Epoch + 1, nil
}
func (r *Router) coverageForScope(ctx context.Context, b OwnerScopeBinding) (string, error) {
	var raw, proof []byte
	var digest string
	err := r.db.QueryRowContext(ctx, `SELECT body,proof,digest FROM gateway_coverage WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=?`, b.Tenant, b.AppID, b.GatewayID, b.BotGeneration).Scan(&raw, &proof, &digest)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) {
		return "", ErrConflict
	}
	var c GatewayCoverage
	if err = json.Unmarshal(raw, &c); err != nil {
		return "", err
	}
	if c.Tenant != b.Tenant || c.AppID != b.AppID || c.GatewayID != b.GatewayID || c.BotGeneration != b.BotGeneration {
		return "", ErrConflict
	}
	if err = r.validateCoverage(ctx, c, proof); err != nil {
		return "", err
	}
	return digest, nil
}
func (r *Router) verifyBoundCoverageLocked(ctx context.Context, owner Owner, digest string) error {
	var raw []byte
	if err := r.db.QueryRowContext(ctx, `SELECT binding FROM owner_scope_bindings WHERE owner=?`, ownerJSON(owner)).Scan(&raw); err != nil {
		return err
	}
	var b OwnerScopeBinding
	if err := json.Unmarshal(raw, &b); err != nil {
		return err
	}
	if b.Owner != owner {
		return ErrConflict
	}
	actual, err := r.coverageForScope(ctx, b)
	if err != nil {
		return err
	}
	if actual == digest {
		return nil
	}
	// Every edge was atomically recorded only after a trusted verifier proved
	// the successor's live hook and exclusive old-process stop. The original
	// frozen proof stays unchanged; the chain preserves each cutover identity.
	seen := map[string]bool{}
	for digest != actual {
		if digest == "" || seen[digest] || len(seen) >= 10000 {
			return ErrConflict
		}
		seen[digest] = true
		var next string
		var before, after []byte
		if err := r.db.QueryRowContext(ctx, `SELECT next_digest,previous_body,next_body FROM gateway_coverage_lineage WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND previous_digest=?`, b.Tenant, b.AppID, b.GatewayID, b.BotGeneration, digest).Scan(&next, &before, &after); err != nil {
			return err
		}
		prevHash := sha256.Sum256(before)
		nextHash := sha256.Sum256(after)
		var p, n GatewayCoverage
		if hex.EncodeToString(prevHash[:]) != digest || hex.EncodeToString(nextHash[:]) != next || json.Unmarshal(before, &p) != nil || json.Unmarshal(after, &n) != nil || p.Tenant != b.Tenant || n.Tenant != b.Tenant || p.AppID != b.AppID || n.AppID != b.AppID || p.GatewayID != b.GatewayID || n.GatewayID != b.GatewayID || p.BotGeneration != b.BotGeneration || n.BotGeneration != b.BotGeneration || p.RouterID != r.routerID || n.RouterID != r.routerID || n.Epoch <= p.Epoch {
			return ErrConflict
		}
		digest = next
	}
	return nil
}
