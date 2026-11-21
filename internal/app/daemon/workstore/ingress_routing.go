package workstore

import (
	"database/sql"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (t *Tx) FindPersonalScope(principalID, roleID string) (work.WorkScope, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM scopes WHERE kind='personal' AND principal_id=? AND role_id=?", principalID, roleID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.WorkScope{}, ErrNotFound
	}
	if err != nil {
		return work.WorkScope{}, err
	}
	return t.GetScope(id)
}
func (t *Tx) FindBootstrapScope(tenant, chatID string, generation int64) (work.WorkScope, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM scopes WHERE kind='bootstrap' AND tenant=? AND chat_id=? AND binding_generation=?", tenant, chatID, generation).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.WorkScope{}, ErrNotFound
	}
	if err != nil {
		return work.WorkScope{}, err
	}
	return t.GetScope(id)
}

func (t *Tx) FindFirstEstablishmentIngress(tenant, chatID string, generation int64) (work.IngressRecord, error) {
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM raw_ingress WHERE json_extract(payload,'$.route.kind')='bootstrap' AND json_extract(payload,'$.envelope.tenant')=? AND json_extract(payload,'$.envelope.chat_id')=? AND json_extract(payload,'$.route.binding_generation')=? AND json_extract(payload,'$.envelope.sender_type')='user' ORDER BY sequence LIMIT 1", tenant, chatID, generation).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return work.IngressRecord{}, ErrNotFound
	}
	if err != nil {
		return work.IngressRecord{}, err
	}
	return t.GetIngress(id)
}
func (t *Tx) WakeEstablishmentIngress(tenant, chatID string, generation int64, except string) error {
	_, err := t.tx.ExecContext(t.ctx, "UPDATE raw_ingress SET revision=revision+1,payload=json_set(payload,'$.next_attempt_at',NULL,'$.revision',revision+1) WHERE id<>? AND state='pending' AND json_extract(payload,'$.route.kind')='bootstrap' AND json_extract(payload,'$.envelope.tenant')=? AND json_extract(payload,'$.envelope.chat_id')=? AND json_extract(payload,'$.route.binding_generation')=?", except, tenant, chatID, generation)
	return err
}
