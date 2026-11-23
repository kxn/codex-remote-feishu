package ingressowner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// IssuedPrivateView is a durable publication intent, not proof a platform
// message exists. RecordPublished additionally requires a trusted receipt.
type IssuedPrivateView struct {
	Owner             Owner
	EffectID          string
	Ordinal           int
	RenderHash, Token string
}

func (r *Router) IssuePrivateView(ctx context.Context, owner Owner, effectID string, ordinal int, immutableRender []byte) (IssuedPrivateView, error) {
	var out IssuedPrivateView
	if r == nil || !owner.valid() || owner.Application != Private || effectID == "" || ordinal < 0 || len(immutableRender) == 0 || !json.Valid(immutableRender) {
		return out, ErrInvalid
	}
	digest := sha256.Sum256(immutableRender)
	renderHash := hex.EncodeToString(digest[:])
	ownerRaw, _ := json.Marshal(owner)
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return out, err
	}
	token := hex.EncodeToString(secret)
	_, err := r.db.ExecContext(ctx, `INSERT INTO private_views(owner,effect_id,ordinal,render_hash,token) VALUES(?,?,?,?,?) ON CONFLICT(owner,effect_id,ordinal) DO NOTHING`, ownerRaw, effectID, ordinal, renderHash, token)
	if err != nil {
		return out, err
	}
	out = IssuedPrivateView{Owner: owner, EffectID: effectID, Ordinal: ordinal}
	if err = r.db.QueryRowContext(ctx, `SELECT render_hash,token FROM private_views WHERE owner=? AND effect_id=? AND ordinal=?`, ownerRaw, effectID, ordinal).Scan(&out.RenderHash, &out.Token); err != nil {
		return IssuedPrivateView{}, err
	}
	if out.RenderHash != renderHash {
		return IssuedPrivateView{}, ErrConflict
	}
	return out, nil
}
func (r *Router) PrivateView(ctx context.Context, owner Owner, effectID string, ordinal int) (IssuedPrivateView, error) {
	var out IssuedPrivateView
	if r == nil || !owner.valid() || owner.Application != Private || effectID == "" || ordinal < 0 {
		return out, ErrInvalid
	}
	raw, _ := json.Marshal(owner)
	out = IssuedPrivateView{Owner: owner, EffectID: effectID, Ordinal: ordinal}
	err := r.db.QueryRowContext(ctx, `SELECT render_hash,token FROM private_views WHERE owner=? AND effect_id=? AND ordinal=?`, raw, effectID, ordinal).Scan(&out.RenderHash, &out.Token)
	if err != nil {
		return IssuedPrivateView{}, fmt.Errorf("private view: %w", err)
	}
	return out, nil
}

// PrivateViewByToken resolves only an already-issued opaque token. It proves
// intent, not publication; a platform read is still mandatory before binding.
func (r *Router) PrivateViewByToken(ctx context.Context, token string) (IssuedPrivateView, error) {
	var out IssuedPrivateView
	var owner []byte
	if r == nil || token == "" {
		return out, ErrInvalid
	}
	err := r.db.QueryRowContext(ctx, `SELECT owner,effect_id,ordinal,render_hash FROM private_views WHERE token=?`, token).Scan(&owner, &out.EffectID, &out.Ordinal, &out.RenderHash)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(owner, &out.Owner) != nil || !out.Owner.valid() || out.Owner.Application != Private {
		return IssuedPrivateView{}, ErrConflict
	}
	out.Token = token
	return out, nil
}
