package ingressowner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// StoreStagedPrivateAction records the immutable result of the trusted SDK
// media/quote staging worker. It cannot bypass an owner migration gate.
func (r *Router) StoreStagedPrivateAction(ctx context.Context, key Key, owner Owner, body json.RawMessage) error {
	if r == nil || owner.Application != Private || !owner.valid() || !json.Valid(body) || string(body) == "null" {
		return ErrInvalid
	}
	release := r.acquire(key)
	defer release()
	record, err := r.get(ctx, key)
	if err != nil {
		return err
	}
	if record.State != Pending || record.Owner == nil || deliveryOwner(record) != owner || record.Event.Kind != Message {
		return ErrConflict
	}
	ownerRelease := r.acquireOwner(*record.Owner)
	defer ownerRelease()
	record, err = r.get(ctx, key)
	if err != nil {
		return err
	}
	if record.State != Pending || deliveryOwner(record) != owner {
		return ErrConflict
	}
	if err = r.allowOwnerDelivery(ctx, record, false); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	ownerRaw := ownerJSON(owner)
	_, err = r.db.ExecContext(ctx, `INSERT INTO staged_private_actions(seq,owner,body,body_hash) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, record.Sequence, ownerRaw, body, digest)
	if err != nil {
		return err
	}
	var oldOwner, oldBody []byte
	var oldHash string
	if err = r.db.QueryRowContext(ctx, `SELECT owner,body,body_hash FROM staged_private_actions WHERE seq=?`, record.Sequence).Scan(&oldOwner, &oldBody, &oldHash); err != nil {
		return err
	}
	if string(oldOwner) != string(ownerRaw) || string(oldBody) != string(body) || oldHash != digest {
		return ErrConflict
	}
	return nil
}
func (r *Router) StagedPrivateAction(ctx context.Context, record Record, owner Owner) (json.RawMessage, error) {
	if r == nil || record.Owner == nil || owner.Application != Private || deliveryOwner(record) != owner {
		return nil, ErrInvalid
	}
	var storedOwner, body []byte
	var digest string
	query := `SELECT owner,body,body_hash FROM staged_private_actions WHERE seq=?`
	id := record.Sequence
	if record.State == Historical && record.Sequence < 0 {
		query = `SELECT owner,body,body_hash FROM historical_staged_actions WHERE id=?`
		id = -record.Sequence
	}
	err := r.db.QueryRowContext(ctx, query, id).Scan(&storedOwner, &body, &digest)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if string(storedOwner) != string(ownerJSON(owner)) || digest != hex.EncodeToString(sum[:]) || !json.Valid(body) {
		return nil, ErrConflict
	}
	return body, nil
}

// HistoricalStagingVerifier independently reads the sealed source manifest,
// target private binding and controlled assets; caller JSON alone is not proof.
type HistoricalStagingVerifier interface {
	VerifyHistoricalPrivateStaging(context.Context, Record, Owner, json.RawMessage, []byte) error
}

func (r *Router) SetHistoricalStagingVerifier(v HistoricalStagingVerifier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.historicalStagingVerifier = v
}
func (r *Router) ImportStagedPrivateAction(ctx context.Context, key Key, target Owner, body json.RawMessage, proof []byte) error {
	if r == nil || target.Application != Private || !target.valid() || !json.Valid(body) || string(body) == "null" || len(proof) == 0 {
		return ErrInvalid
	}
	release := r.acquire(key)
	defer release()
	record, err := r.getAny(ctx, key)
	if err != nil {
		return err
	}
	if record.Owner == nil || record.Owner.Application != Team || record.Event.Kind != Message || record.Event.ChatType != "p2p" || (record.State != Accepted && record.State != Pending && record.State != Historical) {
		return ErrConflict
	}
	ownerRelease := r.acquireOwner(*record.Owner)
	defer ownerRelease()
	if err = r.verifyHistoricalDrainLocked(ctx, *record.Owner); err != nil {
		return err
	}
	record, err = r.getAny(ctx, key)
	if err != nil {
		return err
	}
	r.mu.Lock()
	verifier := r.historicalStagingVerifier
	r.mu.Unlock()
	if verifier == nil {
		return ErrInvalid
	}
	if err = verifier.VerifyHistoricalPrivateStaging(ctx, record, target, body, proof); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])
	ownerRaw := ownerJSON(target)
	insert := `INSERT INTO staged_private_actions(seq,owner,body,body_hash) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`
	query := `SELECT owner,body,body_hash FROM staged_private_actions WHERE seq=?`
	id := record.Sequence
	if record.State == Historical && record.Sequence < 0 {
		insert = `INSERT INTO historical_staged_actions(id,owner,body,body_hash) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`
		query = `SELECT owner,body,body_hash FROM historical_staged_actions WHERE id=?`
		id = -record.Sequence
	}
	_, err = r.db.ExecContext(ctx, insert, id, ownerRaw, body, digest)
	if err != nil {
		return err
	}
	var oldOwner, oldBody []byte
	var oldHash string
	if err = r.db.QueryRowContext(ctx, query, id).Scan(&oldOwner, &oldBody, &oldHash); err != nil {
		return err
	}
	if string(oldOwner) != string(ownerRaw) || string(oldBody) != string(body) || oldHash != digest {
		return ErrConflict
	}
	return nil
}
