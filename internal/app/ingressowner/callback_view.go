package ingressowner

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
)

// A synchronous callback may offer a replacement without a new platform send.
// Keep that fact separate from messages, whose ownership still comes solely
// from actual platform receipts. An asynchronous message update advances its
// version and thereby invalidates all earlier offered callback views.
type callbackViewReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Router) privateCardReferenceCurrent(ctx context.Context, e Event, target *PlatformMessage) (bool, error) {
	return privateCardReferenceCurrent(ctx, r.db, e, target)
}

// CurrentPrivateView checks the same authority used for callbacks. known is
// false while an offered callback response has not yet been committed.
func (r *Router) CurrentPrivateView(ctx context.Context, owner Owner, tenant, app, chat, message, token, sourceEventID string) (known, current bool, err error) {
	if !owner.valid() || owner.Application != Private || token == "" {
		return false, false, ErrInvalid
	}
	target, err := r.PublishedMessage(ctx, tenant, app, chat, message)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if target.Owner != owner {
		return true, false, ErrConflict
	}
	if target.CardRef == token {
		known = true
	} else {
		var n int
		if err = r.db.QueryRowContext(ctx, `SELECT count(*) FROM private_callback_views WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=? AND token=?`, tenant, app, chat, message, token).Scan(&n); err != nil {
			return false, false, err
		}
		known = n != 0
		if !known {
			ownerJSON, e := json.Marshal(owner)
			if e != nil {
				return false, false, e
			}
			if e = r.db.QueryRowContext(ctx, `SELECT count(*) FROM private_views WHERE owner=? AND token=?`, ownerJSON, token).Scan(&n); e != nil {
				return false, false, e
			}
			known = n != 0
		}
	}
	if !known {
		if sourceEventID != "" {
			record, e := r.Get(ctx, Key{Tenant: tenant, AppID: app, EventID: sourceEventID})
			if errors.Is(e, sql.ErrNoRows) {
				return false, false, nil
			}
			if e != nil {
				return false, false, e
			}
			if record.Owner == nil || *record.Owner != owner {
				return true, false, ErrConflict
			}
			if record.State == Accepted || record.State == Expired {
				return true, false, nil
			}
		}
		return false, false, nil
	}
	current, err = r.privateCardReferenceCurrent(ctx, Event{CardKind: "private_view", CardRef: token}, &target)
	return known, current, err
}
func privateCardReferenceCurrent(ctx context.Context, reader callbackViewReader, e Event, target *PlatformMessage) (bool, error) {
	if target == nil || target.Lifecycle != "active" || target.CardKind != e.CardKind {
		return false, nil
	}
	if target.Owner.Application != Private || e.CardKind != "private_view" {
		return target.CardRef != "" && target.CardRef == e.CardRef, nil
	}
	var token string
	err := reader.QueryRowContext(ctx, `SELECT token FROM private_callback_views WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=? AND base_version=? ORDER BY id DESC LIMIT 1`, target.Tenant, target.AppID, target.ChatID, target.MessageID, target.Version).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return target.CardRef != "" && target.CardRef == e.CardRef, nil
	}
	if err != nil {
		return false, err
	}
	return token == e.CardRef, nil
}

// This runs in the same router transaction as the accepted admission and raw
// HTTP response. The trusted application has already committed its response
// with its input/checkpoint; retries must provide the identical token and body.
func recordCallbackReplacement(ctx context.Context, tx *sql.Tx, saved Record, receipt *AdmissionReceipt) error {
	v := receipt.Replacement
	if v == nil {
		return nil
	}
	if saved.Event.Kind != Card || saved.Event.CardKind != "private_view" || saved.Route == nil || saved.Route.Message == nil || saved.Owner == nil || receipt.Owner.Application != Private || receipt.Owner != *saved.Owner || v.TargetMessageID != saved.Event.MessageID || len(receipt.Response) == 0 {
		return ErrInvalid
	}
	secret, err := hex.DecodeString(v.Token)
	if err != nil || len(secret) != 32 {
		return ErrInvalid
	}
	hash := sha256.Sum256(receipt.Response)
	if v.ResponseHash != hex.EncodeToString(hash[:]) {
		return ErrConflict
	}
	base := saved.Route.Message
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM messages WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=?`, base.Tenant, base.AppID, base.ChatID, base.MessageID).Scan(&raw); err != nil {
		return err
	}
	var current PlatformMessage
	if json.Unmarshal(raw, &current) != nil || current.Owner != receipt.Owner {
		return ErrConflict
	}
	// A later asynchronous publication wins over a callback computed from an
	// older platform view. Consume its input but do not return an old page.
	if !reflect.DeepEqual(current, *base) {
		receipt.Replacement = nil
		receipt.Response = json.RawMessage(`{"toast":{"type":"info","content":"菜单已更新，请使用当前卡片。"}}`)
		return nil
	}
	var existingSource int64
	var latestToken string
	err = tx.QueryRowContext(ctx, `SELECT source_seq,token FROM private_callback_views WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=? AND base_version=? ORDER BY id DESC LIMIT 1`, base.Tenant, base.AppID, base.ChatID, base.MessageID, base.Version).Scan(&existingSource, &latestToken)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && existingSource != saved.Sequence && latestToken != saved.Event.CardRef {
		receipt.Replacement = nil
		receipt.Response = json.RawMessage(`{"toast":{"type":"info","content":"菜单已更新，请使用当前卡片。"}}`)
		return nil
	}
	owner, _ := json.Marshal(receipt.Owner)
	_, err = tx.ExecContext(ctx, `INSERT INTO private_callback_views(source_seq,tenant,app_id,chat_id,message_id,base_version,owner,token,response_hash) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(source_seq) DO NOTHING`, saved.Sequence, base.Tenant, base.AppID, base.ChatID, base.MessageID, base.Version, owner, v.Token, v.ResponseHash)
	if err != nil {
		return err
	}
	var storedToken, storedHash string
	if err = tx.QueryRowContext(ctx, `SELECT token,response_hash FROM private_callback_views WHERE source_seq=?`, saved.Sequence).Scan(&storedToken, &storedHash); err != nil {
		return err
	}
	if storedToken != v.Token || storedHash != v.ResponseHash {
		return ErrConflict
	}
	return nil
}

// CallbackResponseCurrent prevents an old HTTP retry from overwriting a newer
// page. The original durable response is retained unchanged for evidence.
func (r *Router) CallbackResponseCurrent(ctx context.Context, record Record) (bool, error) {
	if record.Receipt == nil || record.Receipt.Replacement == nil {
		return true, nil
	}
	target, err := r.lookupMessage(ctx, record.Event)
	if err != nil {
		return false, err
	}
	event := record.Event
	event.CardRef = record.Receipt.Replacement.Token
	return r.privateCardReferenceCurrent(ctx, event, target)
}
