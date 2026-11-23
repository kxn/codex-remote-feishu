package ingressowner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// GroupOwnerWitness identifies a real historical team SDK observation. The
// owner itself is created by the neutral registry, never inferred from IDs.
type GroupOwnerWitness struct {
	SourceEvent       Key    `json:"source_event"`
	SourceFactHash    string `json:"source_fact_hash"`
	GatewayID         string `json:"gateway_id"`
	BindingGeneration int64  `json:"binding_generation"`
	ChatID            string `json:"chat_id"`
}
type GroupOwnerWitnessVerifier interface {
	VerifyGroupOwnerWitness(context.Context, GroupOwnerWitness, []byte) error
}

func (r *Router) SetGroupOwnerWitnessVerifier(v GroupOwnerWitnessVerifier) {
	r.mu.Lock()
	r.groupWitnessVerifier = v
	r.mu.Unlock()
}
func (r *Router) ImportHistoricalGroupOwner(ctx context.Context, w GroupOwnerWitness, proof []byte) (Owner, error) {
	var empty Owner
	if r == nil || w.SourceEvent.Tenant == "" || w.SourceEvent.AppID == "" || w.SourceEvent.EventID == "" || w.SourceFactHash == "" || w.GatewayID == "" || w.BindingGeneration < 1 || w.ChatID == "" || len(proof) == 0 {
		return empty, ErrInvalid
	}
	r.mu.Lock()
	verifier := r.groupWitnessVerifier
	r.mu.Unlock()
	if verifier == nil {
		return empty, ErrInvalid
	}
	if err := verifier.VerifyGroupOwnerWitness(ctx, w, proof); err != nil {
		return empty, err
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return empty, err
	}
	owner := Owner{Application: Team, ID: "owner_" + hex.EncodeToString(nonce), Epoch: 1}
	raw, _ := json.Marshal(owner)
	witness, _ := json.Marshal(w)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO group_owners(tenant,app_id,gateway,binding_generation,chat_id,owner) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, w.SourceEvent.Tenant, w.SourceEvent.AppID, w.GatewayID, w.BindingGeneration, w.ChatID, raw); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO group_owner_witnesses(tenant,app_id,gateway,binding_generation,chat_id,witness,proof) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, w.SourceEvent.Tenant, w.SourceEvent.AppID, w.GatewayID, w.BindingGeneration, w.ChatID, witness, proof); err != nil {
		return empty, err
	}
	var existing []byte
	if err = tx.QueryRowContext(ctx, `SELECT owner FROM group_owners WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND chat_id=?`, w.SourceEvent.Tenant, w.SourceEvent.AppID, w.GatewayID, w.BindingGeneration, w.ChatID).Scan(&existing); err != nil {
		return empty, err
	}
	if json.Unmarshal(existing, &owner) != nil || !owner.valid() || owner.Application != Team {
		return empty, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return owner, nil
}
