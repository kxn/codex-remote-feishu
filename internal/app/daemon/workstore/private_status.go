package workstore

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PrivateContextStatus struct {
	Pending, Claimed, AwaitingResponse int
	FirstUser, LastUser                *work.Message
}

// Read only the selected private conversation. Other conversations and the
// management copy of a blocked input must not change its status or previews.
func (t *Tx) ReadPrivateContextStatus(scopeID, contextID, principalID string) (PrivateContextStatus, error) {
	var result PrivateContextStatus
	scope, err := t.GetScope(scopeID)
	if err != nil {
		return result, err
	}
	wc, err := t.GetContext(contextID)
	if err != nil {
		return result, err
	}
	if scope.Kind != work.ScopePersonal || scope.PrincipalID != principalID || wc.ScopeID != scope.ID || wc.RoleID != scope.RoleID || wc.Purpose == work.PurposeControl {
		return result, ErrScopeDenied
	}
	err = t.tx.QueryRowContext(t.ctx, `SELECT coalesce(sum(state='pending'),0),coalesce(sum(state='claimed'),0) FROM inbox WHERE scope_id=? AND json_extract(payload,'$.recipient_context_id')=?`, scopeID, contextID).Scan(&result.Pending, &result.Claimed)
	if err != nil {
		return result, err
	}
	result.AwaitingResponse, err = t.privateStatusAnswerableRequests(wc)
	if err != nil {
		return result, err
	}

	for _, descending := range []bool{false, true} {
		order := " ASC"
		if descending {
			order = " DESC"
		}
		var raw string
		err := t.tx.QueryRowContext(t.ctx, `SELECT payload FROM messages WHERE scope_id=? AND json_extract(payload,'$.source.kind')='principal' AND json_extract(payload,'$.source.id')=? AND coalesce(json_extract(payload,'$.recalled_evidence_ref'),'')='' AND EXISTS(SELECT 1 FROM inbox i WHERE i.scope_id=messages.scope_id AND json_extract(i.payload,'$.message_id')=messages.id AND json_extract(i.payload,'$.recipient_context_id')=?) ORDER BY julianday(json_extract(payload,'$.created_at'))`+order+",id"+order+" LIMIT 1", scopeID, principalID, contextID).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return result, err
		}
		var message work.Message
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			return result, err
		}
		if descending {
			result.LastUser = &message
		} else {
			result.FirstUser = &message
		}
	}
	return result, nil
}
