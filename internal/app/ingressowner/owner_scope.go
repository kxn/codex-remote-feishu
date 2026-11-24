package ingressowner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// OwnerScopeBinding links a frozen neutral SDK event to a specific team scope.
// The team verifier must independently compare that event to its durable source.
type OwnerScopeBinding struct {
	Owner          Owner  `json:"owner"`
	SourceEventKey Key    `json:"source_event_key"`
	SourceFactHash string `json:"source_fact_hash"`
	ScopeID        string `json:"scope_id"`
	PrincipalID    string `json:"principal_id"`
	RoleID         string `json:"role_id"`
	GatewayID      string `json:"gateway_id"`
	AppID          string `json:"app_id"`
	BotGeneration  int64  `json:"bot_generation"`
	Tenant         string `json:"tenant"`
	ChatID         string `json:"chat_id"`
	OpenID         string `json:"open_id"`
}
type OwnerScopeBindingVerifier interface {
	VerifyOwnerScopeBinding(context.Context, OwnerScopeBinding, *Record, []byte) error
}

func (r *Router) SetOwnerScopeBindingVerifier(v OwnerScopeBindingVerifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopeVerifier = v
}
func (r *Router) validateOwnerScope(ctx context.Context, b OwnerScopeBinding, proof []byte) error {
	if !b.Owner.valid() || b.Owner.Application != Team || b.ScopeID == "" || b.PrincipalID == "" || b.RoleID == "" || b.GatewayID == "" || b.AppID == "" || b.BotGeneration < 1 || b.SourceFactHash == "" || b.Tenant == "" || b.ChatID == "" || b.OpenID == "" || len(proof) == 0 {
		return ErrInvalid
	}
	record, err := r.get(ctx, b.SourceEventKey)
	var neutral *Record
	if err == nil {
		e := record.Event
		hash, e2 := EventFactHash(e)
		if e2 != nil {
			return e2
		}
		if record.State == Unresolved || record.Owner == nil || *record.Owner != b.Owner || e.Kind != Message || e.Key.Tenant != b.Tenant || e.Key.AppID != b.AppID || e.Source.GatewayID != b.GatewayID || e.Source.BindingGeneration != b.BotGeneration || e.ChatID != b.ChatID || e.Operator.IDType != "open_id" || e.Operator.ID != b.OpenID || hash != b.SourceFactHash {
			return ErrConflict
		}
		neutral = &record
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	r.mu.Lock()
	verifier := r.scopeVerifier
	r.mu.Unlock()
	if verifier == nil {
		return ErrInvalid
	}
	return verifier.VerifyOwnerScopeBinding(ctx, b, neutral, proof)
}
func (r *Router) RecordOwnerScopeBinding(ctx context.Context, b OwnerScopeBinding, sourceProof []byte) error {
	if r == nil {
		return ErrInvalid
	}
	release := r.acquireOwner(b.Owner)
	defer release()
	// Direct registration requires an existing neutral event; historical imports
	// must use ImportOwnerScopeBinding so the owner is minted by this router.
	source, err := r.get(ctx, b.SourceEventKey)
	if err != nil {
		return err
	}
	if source.State != Accepted {
		return ErrConflict
	}
	if err := r.validateOwnerScope(ctx, b, sourceProof); err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	owner := ownerJSON(b.Owner)
	if _, err = r.db.ExecContext(ctx, `INSERT INTO owner_scope_bindings(owner,binding,proof,digest) VALUES(?,?,?,?) ON CONFLICT(owner) DO NOTHING`, owner, raw, sourceProof, digest); err != nil {
		return err
	}
	var old, oldProof []byte
	var oldDigest string
	if err = r.db.QueryRowContext(ctx, `SELECT binding,proof,digest FROM owner_scope_bindings WHERE owner=?`, owner).Scan(&old, &oldProof, &oldDigest); err != nil {
		return err
	}
	if string(old) != string(raw) || string(oldProof) != string(sourceProof) || oldDigest != digest {
		return ErrConflict
	}
	return nil
}
func (r *Router) verifyOwnerScopeLocked(ctx context.Context, owner Owner, digest string) error {
	var raw, proof []byte
	var stored string
	err := r.db.QueryRowContext(ctx, `SELECT binding,proof,digest FROM owner_scope_bindings WHERE owner=?`, ownerJSON(owner)).Scan(&raw, &proof, &stored)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if stored != digest || stored != hex.EncodeToString(sum[:]) {
		return ErrConflict
	}
	var b OwnerScopeBinding
	if err = json.Unmarshal(raw, &b); err != nil {
		return err
	}
	if b.Owner != owner {
		return ErrConflict
	}
	return r.validateOwnerScope(ctx, b, proof)
}
func (r *Router) BeginBoundOwnerMigration(ctx context.Context, b OwnerScopeBinding, id string) (DrainProof, error) {
	var digest string
	if err := r.db.QueryRowContext(ctx, `SELECT digest FROM owner_scope_bindings WHERE owner=?`, ownerJSON(b.Owner)).Scan(&digest); err != nil {
		return DrainProof{}, err
	}
	release := r.acquireOwner(b.Owner)
	err := r.verifyOwnerScopeLocked(ctx, b.Owner, digest)
	release()
	if err != nil {
		return DrainProof{}, err
	}
	var stored []byte
	if err = r.db.QueryRowContext(ctx, `SELECT binding FROM owner_scope_bindings WHERE owner=?`, ownerJSON(b.Owner)).Scan(&stored); err != nil {
		return DrainProof{}, err
	}
	expected, _ := json.Marshal(b)
	if string(stored) != string(expected) {
		return DrainProof{}, ErrConflict
	}
	coverage, err := r.coverageForScope(ctx, b)
	if err != nil {
		return DrainProof{}, err
	}
	proof, err := r.BeginOwnerMigration(ctx, b.Owner, id)
	if err != nil {
		return DrainProof{}, err
	}
	proof.BindingDigest = digest
	proof.CoverageDigest = coverage
	proof.Digest = drainDigest(proof)
	if err = r.VerifyOwnerMigrationDrain(ctx, b.Owner, id, proof); err != nil {
		return DrainProof{}, err
	}
	return proof, nil
}

// VerifyBoundOwnerMigrationDrain refuses unbound proofs for source snapshots.
func (r *Router) VerifyBoundOwnerMigrationDrain(ctx context.Context, b OwnerScopeBinding, id string, p DrainProof) error {
	if p.BindingDigest == "" || p.CoverageDigest == "" || p.Owner != b.Owner {
		return ErrInvalid
	}
	release := r.acquireOwner(b.Owner)
	defer release()
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	if p.BindingDigest != hex.EncodeToString(sum[:]) || p.MigrationID != id || p.Digest != drainDigest(p) {
		return ErrConflict
	}
	if err = r.verifyOwnerScopeLocked(ctx, b.Owner, p.BindingDigest); err != nil {
		return err
	}
	return r.verifyDrainLocked(ctx, p)
}

// ImportOwnerScopeBinding assigns an opaque owner to a verified historical
// source event. It never derives owner IDs from scope names or user input.
func (r *Router) ImportOwnerScopeBinding(ctx context.Context, b OwnerScopeBinding, proof []byte) (OwnerScopeBinding, error) {
	if r == nil || b.Owner.ID != "" || b.Owner.Epoch != 0 || b.Owner.Application != "" && b.Owner.Application != Team || b.SourceEventKey.Tenant == "" || b.SourceEventKey.AppID == "" || b.SourceEventKey.EventID == "" {
		return OwnerScopeBinding{}, ErrInvalid
	}
	release := r.acquire(b.SourceEventKey)
	defer release()
	var existing []byte
	err := r.db.QueryRowContext(ctx, `SELECT owner FROM imported_scope_sources WHERE tenant=? AND app_id=? AND event_id=?`, b.SourceEventKey.Tenant, b.SourceEventKey.AppID, b.SourceEventKey.EventID).Scan(&existing)
	if err == nil {
		if json.Unmarshal(existing, &b.Owner) != nil {
			return OwnerScopeBinding{}, ErrConflict
		}
		var stored, oldProof []byte
		if err = r.db.QueryRowContext(ctx, `SELECT binding,proof FROM owner_scope_bindings WHERE owner=?`, existing).Scan(&stored, &oldProof); err != nil {
			return OwnerScopeBinding{}, err
		}
		expected, _ := json.Marshal(b)
		if string(stored) != string(expected) {
			return OwnerScopeBinding{}, ErrConflict
		}
		// The original immutable owner and proof remain authoritative. Source
		// ingress revisions may advance after a partial startup or rollback, so
		// reverify both observations rather than requiring identical proof bytes.
		if err = r.validateOwnerScope(ctx, b, oldProof); err != nil {
			return OwnerScopeBinding{}, err
		}
		if err = r.validateOwnerScope(ctx, b, proof); err != nil {
			return OwnerScopeBinding{}, err
		}
		return b, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return OwnerScopeBinding{}, err
	}
	record, e := r.get(ctx, b.SourceEventKey)
	if e == nil {
		if record.Owner == nil {
			return OwnerScopeBinding{}, ErrConflict
		}
		b.Owner = *record.Owner
	} else if errors.Is(e, sql.ErrNoRows) {
		nonce := make([]byte, 24)
		if _, e = rand.Read(nonce); e != nil {
			return OwnerScopeBinding{}, e
		}
		b.Owner = Owner{Application: Team, ID: "owner_" + hex.EncodeToString(nonce), Epoch: 1}
	} else {
		return OwnerScopeBinding{}, e
	}
	if err = r.validateOwnerScope(ctx, b, proof); err != nil {
		return OwnerScopeBinding{}, err
	}
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	owner := ownerJSON(b.Owner)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return OwnerScopeBinding{}, err
	}
	defer tx.Rollback()
	var existingBinding []byte
	e = tx.QueryRowContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=? AND id_type='open_id' AND operator_id=?`, b.Tenant, b.AppID, b.GatewayID, b.BotGeneration, b.OpenID).Scan(&existingBinding)
	if e == nil {
		var private PrivateBinding
		if json.Unmarshal(existingBinding, &private) != nil {
			return OwnerScopeBinding{}, ErrConflict
		}
		if private.ChatID == b.ChatID {
			return OwnerScopeBinding{}, ErrConflict
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return OwnerScopeBinding{}, e
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO owner_scope_bindings(owner,binding,proof,digest) VALUES(?,?,?,?)`, owner, raw, proof, digest); err != nil {
		return OwnerScopeBinding{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO imported_scope_sources(tenant,app_id,event_id,gateway,binding_generation,chat_id,open_id,owner) VALUES(?,?,?,?,?,?,?,?)`, b.SourceEventKey.Tenant, b.SourceEventKey.AppID, b.SourceEventKey.EventID, b.GatewayID, b.BotGeneration, b.ChatID, b.OpenID, owner); err != nil {
		return OwnerScopeBinding{}, err
	}
	if err = tx.Commit(); err != nil {
		return OwnerScopeBinding{}, err
	}
	return b, nil
}
func (r *Router) LookupImportedOwner(ctx context.Context, tenant, app, gateway string, generation int64, chat, openID string) (Owner, error) {
	var owner Owner
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT owner FROM imported_scope_sources WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND chat_id=? AND open_id=?`, tenant, app, gateway, generation, chat, openID).Scan(&raw)
	if err != nil {
		return owner, err
	}
	err = json.Unmarshal(raw, &owner)
	return owner, err
}
