package ingressowner

import (
	"context"
	"database/sql"
	"encoding/json"
)

// LegacyScopeBarrier records a source personal scope whose historical owner
// cannot yet be proved. Empty app/gateway with generation zero blocks fresh
// private ownership for all bots until its source role can be identified.
type LegacyScopeBarrier struct {
	ScopeID, RoleID, AppID, GatewayID, Reason string
	BotGeneration                             int64
}
type LegacyScopeVerifier interface {
	VerifyLegacyScopeBarrier(context.Context, LegacyScopeBarrier) error
}

func (r *Router) SetLegacyScopeVerifier(v LegacyScopeVerifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.legacyScopeVerifier = v
}
func (r *Router) RecordLegacyScopeBarrier(ctx context.Context, b LegacyScopeBarrier) error {
	if r == nil || b.ScopeID == "" || b.RoleID == "" || b.Reason == "" || (b.AppID == "") != (b.GatewayID == "") || (b.AppID == "" && b.BotGeneration != 0) || (b.AppID != "" && b.BotGeneration < 1) {
		return ErrInvalid
	}
	r.mu.Lock()
	verifier := r.legacyScopeVerifier
	r.mu.Unlock()
	if verifier == nil {
		return ErrInvalid
	}
	if err := verifier.VerifyLegacyScopeBarrier(ctx, b); err != nil {
		return err
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO legacy_scope_barriers(scope_id,role_id,app_id,gateway,binding_generation,reason) VALUES(?,?,?,?,?,?) ON CONFLICT(scope_id) DO UPDATE SET role_id=excluded.role_id,app_id=excluded.app_id,gateway=excluded.gateway,binding_generation=excluded.binding_generation,reason=excluded.reason`, b.ScopeID, b.RoleID, b.AppID, b.GatewayID, b.BotGeneration, b.Reason)
	return err
}

func (r *Router) ListLegacyScopeBarriers(ctx context.Context, after string, limit int) ([]LegacyScopeBarrier, error) {
	if r == nil || limit < 1 || limit > 1000 {
		return nil, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT scope_id,role_id,app_id,gateway,binding_generation,reason FROM legacy_scope_barriers WHERE scope_id>? ORDER BY scope_id LIMIT ?`, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LegacyScopeBarrier
	for rows.Next() {
		var b LegacyScopeBarrier
		if err = rows.Scan(&b.ScopeID, &b.RoleID, &b.AppID, &b.GatewayID, &b.BotGeneration, &b.Reason); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ResolveLegacyScopeBarrier removes a guard only after the source-bound owner
// and its original SDK witness have been independently reverified.
func (r *Router) ResolveLegacyScopeBarrier(ctx context.Context, b OwnerScopeBinding) error {
	unlock := r.acquireOwner(b.Owner)
	defer unlock()
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	var stored []byte
	var digest string
	if err = r.db.QueryRowContext(ctx, `SELECT binding,digest FROM owner_scope_bindings WHERE owner=?`, ownerJSON(b.Owner)).Scan(&stored, &digest); err != nil {
		return err
	}
	if string(stored) != string(raw) {
		return ErrConflict
	}
	if err = r.verifyOwnerScopeLocked(ctx, b.Owner, digest); err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `DELETE FROM legacy_scope_barriers WHERE scope_id=? AND role_id=?`, b.ScopeID, b.RoleID)
	return err
}

func assertLegacyOpen(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, app, gateway string, generation int64) error {
	var count int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM legacy_scope_barriers WHERE (app_id='' AND gateway='' AND binding_generation=0) OR gateway=?`, gateway).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrUnresolved
	}
	return nil
}

// AssertPrivateEventOwner permits a previously proven migration owner even
// when another ambiguous personal scope for that bot remains blocked.
func (r *Router) AssertPrivateEventOwner(ctx context.Context, event Event, b PrivateBinding) error {
	if event.Operator.IDType == "open_id" {
		var count int
		err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM imported_scope_sources WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND chat_id=? AND open_id=?`, event.Key.Tenant, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration, b.ChatID, event.Operator.ID).Scan(&count)
		if err != nil {
			return err
		}
		if count == 1 {
			return nil
		}
	}
	return assertLegacyOpen(ctx, r.db, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration)
}
