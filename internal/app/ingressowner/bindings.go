package ingressowner

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
)

// RecordPublished requires a trusted verifier over a real platform receipt. A
// caller-provided string or a card callback cannot itself prove publication.
func (r *Router) RecordPublished(ctx context.Context, m PlatformMessage, receipt []byte, expectedVersion int64) error {
	if err := m.validate(); err != nil {
		return err
	}
	// Serialize the confirmed view's local authority with admission. This
	// lock is acquired after platform I/O by publishers; it does not claim
	// to order remote HTTP arrival or Feishu rendering.
	releaseOwner := r.acquireOwner(m.Owner)
	defer releaseOwner()
	if r.verifier == nil {
		return ErrInvalid
	}
	if err := r.verifier.VerifyPlatformReceipt(ctx, m, receipt); err != nil {
		return err
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM messages WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=?`, m.Tenant, m.AppID, m.ChatID, m.MessageID).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		if expectedVersion != 0 {
			return ErrConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO messages(tenant,app_id,chat_id,message_id,payload) VALUES(?,?,?,?,?)`, m.Tenant, m.AppID, m.ChatID, m.MessageID, raw)
	} else if err == nil {
		var old PlatformMessage
		if err = json.Unmarshal(existing, &old); err != nil {
			return err
		}
		if reflect.DeepEqual(old, m) {
			return nil
		}
		if m.Lifecycle == "active" && m.CardRef != "" && m.CardRef == old.CardRef {
			return ErrConflict
		}
		if old.Owner != m.Owner || old.GatewayID != m.GatewayID || old.BindingGeneration != m.BindingGeneration || old.Version != expectedVersion || m.Version != old.Version+1 {
			return ErrConflict
		}
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE messages SET payload=? WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=? AND payload=?`, raw, m.Tenant, m.AppID, m.ChatID, m.MessageID, existing)
		if err == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				return ErrConflict
			}
		}
	}
	if err != nil {
		return err
	}
	if m.CardRef != "" && m.Lifecycle == "active" {
		result, e := tx.ExecContext(ctx, `INSERT INTO card_tokens(tenant,app_id,chat_id,message_id,card_kind,card_ref,version) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, m.Tenant, m.AppID, m.ChatID, m.MessageID, m.CardKind, m.CardRef, m.Version)
		if e != nil {
			return e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if count != 1 {
			return ErrConflict
		}
	}
	return tx.Commit()
}
func (r *Router) RecordPrivateBinding(ctx context.Context, b PrivateBinding, proof []byte) error {
	if err := b.validate(); err != nil {
		return err
	}
	if r.bindingVerifier == nil {
		return ErrInvalid
	}
	if err := r.bindingVerifier.VerifyPrivateBinding(ctx, b, proof); err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO private_bindings(tenant,app_id,gateway,generation,id_type,operator_id,payload) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, b.Tenant, b.AppID, b.GatewayID, b.BindingGeneration, b.Operator.IDType, b.Operator.ID, raw)
	if err != nil {
		return err
	}
	var saved []byte
	err = tx.QueryRowContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=? AND id_type=? AND operator_id=?`, b.Tenant, b.AppID, b.GatewayID, b.BindingGeneration, b.Operator.IDType, b.Operator.ID).Scan(&saved)
	if err != nil {
		return err
	}
	if string(saved) != string(raw) {
		return ErrConflict
	}
	return tx.Commit()
}
func (r *Router) lookupMessage(ctx context.Context, e Event) (*PlatformMessage, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM messages WHERE tenant=? AND app_id=? AND message_id=? AND (?='' OR chat_id=?) LIMIT 2`, e.Key.Tenant, e.Key.AppID, e.MessageID, e.ChatID, e.ChatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var selected *PlatformMessage
	for rows.Next() {
		var raw []byte
		var m PlatformMessage
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		if selected != nil {
			return nil, ErrUnresolved
		}
		selected = &m
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if selected == nil {
		return nil, ErrUnresolved
	}
	return selected, nil
}
func (r *Router) lookupPrivate(ctx context.Context, e Event) (*PrivateBinding, error) {
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=? AND id_type=? AND operator_id=?`, e.Key.Tenant, e.Key.AppID, e.Source.GatewayID, e.Source.BindingGeneration, e.Operator.IDType, e.Operator.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUnresolved
	}
	if err != nil {
		return nil, err
	}
	var b PrivateBinding
	if err = json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// PublishedMessage reads an exact scoped platform message, never an inferred
// owner from a callback. Receipt verification remains required for updates.
func (r *Router) PublishedMessage(ctx context.Context, tenant, app, chat, message string) (PlatformMessage, error) {
	var result PlatformMessage
	var raw []byte
	if tenant == "" || app == "" || chat == "" || message == "" {
		return result, ErrInvalid
	}
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM messages WHERE tenant=? AND app_id=? AND chat_id=? AND message_id=?`, tenant, app, chat, message).Scan(&raw)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}

// VerifyPrivateBindingRecord compares a candidate against the binding admitted
// earlier through the trusted P2P binding verifier.
func (r *Router) VerifyPrivateBindingRecord(ctx context.Context, b PrivateBinding) error {
	if err := b.validate(); err != nil {
		return err
	}
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=? AND id_type=? AND operator_id=?`, b.Tenant, b.AppID, b.GatewayID, b.BindingGeneration, b.Operator.IDType, b.Operator.ID).Scan(&raw)
	if err != nil {
		return err
	}
	expected, _ := json.Marshal(b)
	if string(raw) != string(expected) {
		return ErrConflict
	}
	return nil
}

// PrivateBindingForOwner resolves only a unique previously verified P2P
// binding for this exact bot identity, chat, and application owner.
func (r *Router) PrivateBindingForOwner(ctx context.Context, tenant, app, gateway string, generation int64, chat string, owner Owner) (PrivateBinding, error) {
	var result PrivateBinding
	if tenant == "" || app == "" || gateway == "" || generation <= 0 || chat == "" || owner.Application != Private || !owner.valid() {
		return result, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=?`, tenant, app, gateway, generation)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var raw []byte
		var b PrivateBinding
		if err = rows.Scan(&raw); err != nil {
			return result, err
		}
		if err = json.Unmarshal(raw, &b); err != nil {
			return result, err
		}
		if b.ChatID == chat && b.Owner == owner {
			if found {
				return result, ErrConflict
			}
			result = b
			found = true
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if !found {
		return result, ErrUnresolved
	}
	return result, nil
}

// EnsureObservedPrivateBinding creates the first P2P binding only from an
// already durably recorded, authenticated message event. The opaque owner is
// minted here; caller-provided card data cannot choose the binding.
func (r *Router) EnsureObservedPrivateBinding(ctx context.Context, event Event) (PrivateBinding, error) {
	var result PrivateBinding
	if event.Kind != Message || event.ChatType != "p2p" || event.Operator.IDType == "" || event.Operator.ID == "" || event.ChatID == "" {
		return result, ErrInvalid
	}
	var sender struct {
		Type   string `json:"sender_type"`
		Tenant string `json:"sender_tenant"`
	}
	if json.Unmarshal(event.Payload, &sender) != nil || sender.Type != "user" || (sender.Tenant != "" && sender.Tenant != event.Key.Tenant) {
		return result, ErrInvalid
	}
	stored, err := r.get(ctx, event.Key)
	if err != nil {
		return result, err
	}
	a, err := facts(stored.Event)
	if err != nil {
		return result, err
	}
	b, err := facts(event)
	if err != nil || string(a) != string(b) || stored.Event.Source.GatewayID != event.Source.GatewayID || stored.Event.Source.BindingGeneration != event.Source.BindingGeneration {
		return result, ErrConflict
	}
	if err = assertLegacyOpen(ctx, r.db, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration); err != nil {
		return result, err
	}
	nonce := make([]byte, 24)
	if _, err = rand.Read(nonce); err != nil {
		return result, err
	}
	result = PrivateBinding{Tenant: event.Key.Tenant, AppID: event.Key.AppID, GatewayID: event.Source.GatewayID, BindingGeneration: event.Source.BindingGeneration, Operator: event.Operator, ChatID: event.ChatID, Owner: Owner{Application: Private, ID: "owner_" + hex.EncodeToString(nonce), Epoch: 1}}
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = assertLegacyOpen(ctx, tx, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration); err != nil {
		return result, err
	}
	if event.Operator.IDType == "open_id" {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM imported_scope_sources WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND chat_id=? AND open_id=?`, event.Key.Tenant, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration, event.ChatID, event.Operator.ID).Scan(&n); err != nil {
			return result, err
		}
		if n != 0 {
			return result, ErrConflict
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO private_bindings(tenant,app_id,gateway,generation,id_type,operator_id,payload) VALUES(?,?,?,?,?,?,?) ON CONFLICT DO NOTHING`, result.Tenant, result.AppID, result.GatewayID, result.BindingGeneration, result.Operator.IDType, result.Operator.ID, raw); err != nil {
		return result, err
	}
	var storedRaw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=? AND id_type=? AND operator_id=?`, result.Tenant, result.AppID, result.GatewayID, result.BindingGeneration, result.Operator.IDType, result.Operator.ID).Scan(&storedRaw); err != nil {
		return result, err
	}
	var actual PrivateBinding
	if err = json.Unmarshal(storedRaw, &actual); err != nil {
		return result, err
	}
	if actual.ChatID != event.ChatID {
		return result, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return actual, nil
}
func (r *Router) PrivateBindingForEvent(ctx context.Context, event Event) (PrivateBinding, error) {
	b, err := r.lookupPrivate(ctx, event)
	if err != nil {
		return PrivateBinding{}, err
	}
	return *b, nil
}

func (r *Router) PrivateBindingForOperator(ctx context.Context, tenant, app, gateway string, generation int64, chat, operatorID string) (PrivateBinding, error) {
	var result PrivateBinding
	if tenant == "" || app == "" || gateway == "" || generation <= 0 || chat == "" || operatorID == "" {
		return result, ErrInvalid
	}
	rows, err := r.db.QueryContext(ctx, `SELECT payload FROM private_bindings WHERE tenant=? AND app_id=? AND gateway=? AND generation=? AND operator_id=?`, tenant, app, gateway, generation, operatorID)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var raw []byte
		var b PrivateBinding
		if err = rows.Scan(&raw); err != nil {
			return result, err
		}
		if err = json.Unmarshal(raw, &b); err != nil {
			return result, err
		}
		if b.ChatID == chat {
			if found {
				return result, ErrConflict
			}
			found = true
			result = b
		}
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if !found {
		return result, ErrUnresolved
	}
	return result, nil
}

// EnsureObservedTeamOwner allocates a stable group owner only after the exact
// authenticated group message has been durably admitted to the neutral router.
func (r *Router) EnsureObservedTeamOwner(ctx context.Context, event Event) (Owner, error) {
	var owner Owner
	if event.Kind != Message || event.ChatType != "group" || event.ChatID == "" {
		return owner, ErrInvalid
	}
	stored, err := r.get(ctx, event.Key)
	if err != nil {
		return owner, err
	}
	a, e := facts(stored.Event)
	if e != nil {
		return owner, e
	}
	b, e := facts(event)
	if e != nil || string(a) != string(b) || stored.Event.Source.GatewayID != event.Source.GatewayID || stored.Event.Source.BindingGeneration != event.Source.BindingGeneration {
		return owner, ErrConflict
	}
	nonce := make([]byte, 24)
	if _, err = rand.Read(nonce); err != nil {
		return owner, err
	}
	owner = Owner{Application: Team, ID: "owner_" + hex.EncodeToString(nonce), Epoch: 1}
	raw, _ := json.Marshal(owner)
	_, err = r.db.ExecContext(ctx, `INSERT INTO group_owners(tenant,app_id,gateway,binding_generation,chat_id,owner) VALUES(?,?,?,?,?,?) ON CONFLICT DO NOTHING`, event.Key.Tenant, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration, event.ChatID, raw)
	if err != nil {
		return Owner{}, err
	}
	return r.TeamOwnerForChat(ctx, event.Key.Tenant, event.Key.AppID, event.Source.GatewayID, event.Source.BindingGeneration, event.ChatID)
}
func (r *Router) TeamOwnerForChat(ctx context.Context, tenant, app, gateway string, generation int64, chat string) (Owner, error) {
	var owner Owner
	var raw []byte
	err := r.db.QueryRowContext(ctx, `SELECT owner FROM group_owners WHERE tenant=? AND app_id=? AND gateway=? AND binding_generation=? AND chat_id=?`, tenant, app, gateway, generation, chat).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return owner, ErrUnresolved
	}
	if err != nil {
		return owner, err
	}
	err = json.Unmarshal(raw, &owner)
	if err != nil || !owner.valid() || owner.Application != Team {
		return Owner{}, ErrConflict
	}
	return owner, nil
}
