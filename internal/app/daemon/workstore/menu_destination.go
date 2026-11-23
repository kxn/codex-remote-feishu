package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) ListPendingMenuInteractions(limit int) ([]MenuInteraction, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("invalid menu batch")
	}
	rows, err := t.tx.QueryContext(t.ctx, "SELECT payload FROM menu_interactions WHERE state='pending' AND NOT EXISTS (SELECT 1 FROM personal_migration_fences f WHERE f.scope_id=json_extract(menu_interactions.payload,'$.scope_id')) ORDER BY json_extract(payload,'$.envelope.received_at'),id LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []MenuInteraction{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		var v MenuInteraction
		if err = json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// KnownMenuDestination returns only an exact current-App authenticated private
// chat or a real private-entry send receipt. Group chats are never candidates.
func (t *Tx) KnownMenuDestination(v MenuInteraction) (chatID, messageID string, err error) {
	var payload string
	err = t.tx.QueryRowContext(t.ctx, `SELECT payload FROM raw_ingress WHERE json_extract(payload,'$.route.scope_id')=? AND json_extract(payload,'$.envelope.chat_type')='p2p' AND json_extract(payload,'$.envelope.sender_type')='user' AND json_extract(payload,'$.gateway.app_id')=? AND json_extract(payload,'$.gateway.bot_generation')=? ORDER BY sequence DESC LIMIT 1`, v.ScopeID, v.Gateway.AppID, v.Gateway.BotGeneration).Scan(&payload)
	if err == nil {
		var raw work.IngressRecord
		if err = json.Unmarshal([]byte(payload), &raw); err != nil {
			return "", "", err
		}
		principal, _, e := t.ResolvePrincipal(work.IdentityEvidence{Tenant: raw.Envelope.Tenant, AppID: raw.Gateway.AppID, Generation: raw.Gateway.BotGeneration, UnionDomain: raw.Gateway.UnionDomain, IDs: raw.Envelope.SenderIDs, ObservationRef: raw.ID})
		if e != nil {
			return "", "", e
		}
		if principal.ID != v.PrincipalID || raw.Envelope.Tenant != v.Envelope.Tenant {
			return "", "", ErrScopeDenied
		}
		return raw.Envelope.ChatID, raw.Envelope.MessageID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	err = t.tx.QueryRowContext(t.ctx, `SELECT payload FROM menu_interactions WHERE json_extract(payload,'$.scope_id')=? AND json_extract(payload,'$.destination_state')='confirmed' AND json_extract(payload,'$.gateway.app_id')=? AND json_extract(payload,'$.gateway.bot_generation')=? ORDER BY json_extract(payload,'$.envelope.received_at') DESC LIMIT 1`, v.ScopeID, v.Gateway.AppID, v.Gateway.BotGeneration).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	var previous MenuInteraction
	if err = json.Unmarshal([]byte(payload), &previous); err != nil {
		return "", "", err
	}
	if previous.PrincipalID != v.PrincipalID || previous.Envelope.Tenant != v.Envelope.Tenant {
		return "", "", ErrScopeDenied
	}
	return previous.ChatID, previous.MessageID, nil
}
func (t *Tx) BeginMenuDestination(id string, expected int64, text string) (v MenuInteraction, send bool, err error) {
	err = t.atomic(func() error {
		var e error
		v, e = t.GetMenuInteraction(id)
		if e != nil {
			return e
		}
		if v.Revision != expected {
			return ErrConflict
		}
		if v.DestinationState != "" {
			return nil
		}
		if e = t.AssertPersonalMigrationOpen(v.ScopeID); e != nil {
			return e
		}
		if text == "" || v.Envelope.SenderIDs.OpenID == "" {
			return fmt.Errorf("menu entry needs actual App-qualified open_id and frozen text")
		}
		binding, e := t.GetRoleBotBinding(v.Gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.AppID != v.Gateway.AppID || binding.IdentityGeneration != v.Gateway.BotGeneration || binding.GatewayRef != v.Gateway.GatewayRef {
			return ErrConflict
		}
		body := work.NewContent(v.ID+":private-entry", v.ScopeID, "text/plain; charset=utf-8", []byte(text), v.Envelope.ReceivedAt)
		if e = t.PutContent(body); e != nil {
			return e
		}
		v.DestinationTextRef = body.ID
		v.DestinationState = work.OperationSending
		v.Revision++
		send = true
		return t.updateMenuInteraction(v, expected)
	})
	return
}
func (t *Tx) FinishMenuDestination(id string, expected int64, chatID, messageID, lastError string, confirmed bool) error {
	v, err := t.GetMenuInteraction(id)
	if err != nil {
		return err
	}
	if v.Revision != expected || v.DestinationState != work.OperationSending {
		return ErrConflict
	}
	v.DestinationState = work.OperationUnknown
	if confirmed {
		if chatID == "" || messageID == "" {
			return fmt.Errorf("actual private entry receipt must include chat and message")
		}
		v.DestinationState = work.OperationConfirmed
	}
	v.ChatID = chatID
	v.MessageID = messageID
	v.LastError = lastError
	v.Revision++
	return t.updateMenuInteraction(v, expected)
}
func (t *Tx) AdoptKnownMenuDestination(id string, expected int64) error {
	v, err := t.GetMenuInteraction(id)
	if err != nil {
		return err
	}
	if v.Revision != expected {
		return ErrConflict
	}
	chat, message, err := t.KnownMenuDestination(v)
	if err != nil {
		return err
	}
	v.ChatID = chat
	v.MessageID = message
	v.DestinationState = work.OperationConfirmed
	v.Revision++
	return t.updateMenuInteraction(v, expected)
}
func (t *Tx) FinishMenuInteraction(id string, expected int64, resultRef string) error {
	v, err := t.GetMenuInteraction(id)
	if err != nil {
		return err
	}
	if v.State == "handled" {
		if v.ResultRef != resultRef {
			return ErrIdempotencyConflict
		}
		return nil
	}
	if v.Revision != expected || v.DestinationState != work.OperationConfirmed || v.ChatID == "" || resultRef == "" {
		return ErrConflict
	}
	view, err := t.GetUIView(resultRef)
	if err != nil {
		return err
	}
	if view.ScopeID != v.ScopeID || view.PrincipalID != v.PrincipalID || view.ChatID != v.ChatID || view.Binding.AppID != v.Gateway.AppID {
		return ErrScopeDenied
	}
	v.State = "handled"
	v.ResultRef = resultRef
	v.Revision++
	return t.updateMenuInteraction(v, expected)
}
