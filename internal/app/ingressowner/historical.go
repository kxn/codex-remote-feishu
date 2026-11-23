package ingressowner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// HistoricalSourceVerifier reads a sealed source scope and verifies this exact
// SDK event, including the original payload and scope owner; proof is a lookup
// reference, never evidence by itself.
type HistoricalSourceVerifier interface {
	VerifyHistoricalSource(context.Context, OwnerScopeBinding, Event, []byte) error
}

func (r *Router) SetHistoricalSourceVerifier(v HistoricalSourceVerifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.historicalSourceVerifier = v
}
func (r *Router) getHistorical(ctx context.Context, k Key) (Record, error) {
	var rec Record
	var id int64
	var raw, ownerRaw []byte
	err := r.db.QueryRowContext(ctx, `SELECT id,event,owner FROM historical_sources WHERE tenant=? AND app_id=? AND event_id=?`, k.Tenant, k.AppID, k.EventID).Scan(&id, &raw, &ownerRaw)
	if err != nil {
		return rec, err
	}
	var owner Owner
	if json.Unmarshal(raw, &rec.Event) != nil || json.Unmarshal(ownerRaw, &owner) != nil || !owner.valid() {
		return rec, ErrConflict
	}
	rec.Sequence = -id
	rec.Owner = &owner
	rec.State = Historical
	rec.Revision = 1
	return rec, nil
}
func (r *Router) getAny(ctx context.Context, k Key) (Record, error) {
	record, err := r.get(ctx, k)
	if errors.Is(err, sql.ErrNoRows) {
		return r.getHistorical(ctx, k)
	}
	return record, err
}

// ImportHistoricalSourceRecord does not manufacture an application admission
// receipt. A subsequent identical SDK replay resolves to Historical, not a send.
func (r *Router) ImportHistoricalSourceRecord(ctx context.Context, binding OwnerScopeBinding, event Event, proof []byte) (Record, error) {
	var out Record
	if r == nil || event.validate() != nil || binding.Owner.Application != Team || event.Kind != Message || event.ChatType != "p2p" || event.Key.Tenant != binding.Tenant || event.Key.AppID != binding.AppID || event.Source.GatewayID != binding.GatewayID || event.Source.BindingGeneration != binding.BotGeneration || event.ChatID != binding.ChatID || event.Operator.IDType != "open_id" || event.Operator.ID != binding.OpenID || len(proof) == 0 {
		return out, ErrInvalid
	}
	release := r.acquire(event.Key)
	defer release()
	ownerRelease := r.acquireOwner(binding.Owner)
	defer ownerRelease()
	if err := r.verifyHistoricalDrainLocked(ctx, binding.Owner); err != nil {
		return out, err
	}
	rawBinding, _ := json.Marshal(binding)
	sum := sha256.Sum256(rawBinding)
	if err := r.verifyOwnerScopeLocked(ctx, binding.Owner, hex.EncodeToString(sum[:])); err != nil {
		return out, err
	}
	r.mu.Lock()
	verifier := r.historicalSourceVerifier
	r.mu.Unlock()
	if verifier == nil {
		return out, ErrInvalid
	}
	if err := verifier.VerifyHistoricalSource(ctx, binding, event, proof); err != nil {
		return out, err
	}
	if existing, err := r.get(ctx, event.Key); err == nil {
		a, _ := facts(existing.Event)
		b, _ := facts(event)
		if string(a) != string(b) || existing.Owner == nil || *existing.Owner != binding.Owner {
			return out, ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return out, err
	}
	owner := ownerJSON(binding.Owner)
	_, err = r.db.ExecContext(ctx, `INSERT INTO historical_sources(tenant,app_id,event_id,event,owner,proof) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, event.Key.Tenant, event.Key.AppID, event.Key.EventID, raw, owner, proof)
	if err != nil {
		return out, err
	}
	out, err = r.getHistorical(ctx, event.Key)
	if err != nil {
		return out, err
	}
	a, _ := facts(out.Event)
	b, _ := facts(event)
	if string(a) != string(b) || out.Owner == nil || *out.Owner != binding.Owner {
		return Record{}, ErrConflict
	}
	return out, nil
}
