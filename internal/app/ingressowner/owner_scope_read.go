package ingressowner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// OwnerScopeBindingFor returns the verified, immutable source-scope binding
// associated with this exact owner. It does not authorize a transfer.
func (r *Router) OwnerScopeBindingFor(ctx context.Context, owner Owner) (OwnerScopeBinding, error) {
	var binding OwnerScopeBinding
	if r == nil || !owner.valid() || owner.Application != Team {
		return binding, ErrInvalid
	}
	var raw, proof []byte
	var digest string
	if err := r.db.QueryRowContext(ctx, `SELECT binding,proof,digest FROM owner_scope_bindings WHERE owner=?`, ownerJSON(owner)).Scan(&raw, &proof, &digest); err != nil {
		return binding, err
	}
	sum := sha256.Sum256(raw)
	if digest != hex.EncodeToString(sum[:]) || json.Unmarshal(raw, &binding) != nil || binding.Owner != owner {
		return OwnerScopeBinding{}, ErrConflict
	}
	if err := r.validateOwnerScope(ctx, binding, proof); err != nil {
		return OwnerScopeBinding{}, err
	}
	return binding, nil
}
