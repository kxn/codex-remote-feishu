package workstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

const SchemaPlatformObservationsV1 = `
CREATE TABLE team_platform_observations (
 id TEXT PRIMARY KEY, tenant TEXT NOT NULL, chat_id TEXT NOT NULL,
 message_id TEXT NOT NULL, kind TEXT NOT NULL, payload_hash TEXT NOT NULL,
 payload TEXT NOT NULL CHECK(json_valid(payload))
);
CREATE INDEX team_platform_observations_message ON team_platform_observations(tenant,chat_id,message_id,kind);
`

type PlatformObservation struct {
	ID         string              `json:"id"`
	Gateway    work.IngressGateway `json:"gateway"`
	EventID    string              `json:"event_id"`
	Tenant     string              `json:"tenant"`
	ChatID     string              `json:"chat_id"`
	MessageID  string              `json:"message_id"`
	Kind       string              `json:"kind"`
	Facts      json.RawMessage     `json:"facts"`
	ReceivedAt time.Time           `json:"received_at"`
}

func (t *Tx) AcceptPlatformObservation(v PlatformObservation) (string, bool, error) {
	var replay bool
	err := t.atomic(func() error {
		if v.EventID == "" || v.Tenant == "" || v.ReceivedAt.IsZero() || !json.Valid(v.Facts) {
			return fmt.Errorf("platform event requires authenticated identity and facts")
		}
		switch v.Kind {
		case "message.recalled":
			if v.ChatID == "" || v.MessageID == "" {
				return fmt.Errorf("recall requires original message identity")
			}
		case "reaction.created", "bot.added", "bot.removed", "chat.deleted":
		default:
			return fmt.Errorf("unsupported platform event")
		}
		binding, e := t.GetRoleBotBinding(v.Gateway.RoleID)
		if e != nil {
			return e
		}
		if binding.GatewayRef != v.Gateway.GatewayRef || binding.AppID != v.Gateway.AppID || binding.IdentityGeneration != v.Gateway.BotGeneration {
			return ErrScopeDenied
		}
		key, _ := json.Marshal([]any{v.Gateway.AppID, v.Gateway.BotGeneration, v.EventID})
		digest := sha256.Sum256(key)
		v.ID = "platform:" + hex.EncodeToString(digest[:])
		canonical := v
		canonical.ReceivedAt = time.Time{}
		raw, e := json.Marshal(canonical)
		if e != nil {
			return e
		}
		hash := sha256.Sum256(raw)
		sum := hex.EncodeToString(hash[:])
		var previous string
		e = t.tx.QueryRowContext(t.ctx, "SELECT payload_hash FROM team_platform_observations WHERE id=?", v.ID).Scan(&previous)
		if e == nil {
			if previous != sum {
				return ErrIdempotencyConflict
			}
			replay = true
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		body, e := json.Marshal(v)
		if e != nil {
			return e
		}
		_, e = t.tx.ExecContext(t.ctx, "INSERT INTO team_platform_observations(id,tenant,chat_id,message_id,kind,payload_hash,payload) VALUES(?,?,?,?,?,?,?)", v.ID, v.Tenant, v.ChatID, v.MessageID, v.Kind, sum, string(body))
		if e != nil {
			return e
		}
		if v.Kind == "message.recalled" {
			return t.applyPlatformRecall(v)
		}
		return nil
	})
	return v.ID, replay, err
}
func (t *Tx) IsPlatformMessageRecalled(tenant, chat, message string) (bool, error) {
	var count int
	err := t.tx.QueryRowContext(t.ctx, "SELECT count(*) FROM team_platform_observations WHERE tenant=? AND chat_id=? AND message_id=? AND kind='message.recalled'", tenant, chat, message).Scan(&count)
	return count > 0, err
}
func (t *Tx) applyPlatformRecall(v PlatformObservation) error {
	key, _ := json.Marshal(work.MessageKey{Tenant: v.Tenant, ChatID: v.ChatID, MessageID: v.MessageID})
	var id string
	err := t.tx.QueryRowContext(t.ctx, "SELECT id FROM messages WHERE source_key=?", string(key)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	message, err := t.GetMessage(id)
	if err != nil {
		return err
	}
	if message.RecalledEvidenceRef != "" {
		return nil
	}
	evidence := work.NewContent(v.ID, message.ScopeID, "application/json", v.Facts, v.ReceivedAt)
	if err = t.PutContent(evidence); err != nil {
		return err
	}
	old := message.Revision
	message.Revision++
	message.RecalledEvidenceRef = evidence.ID
	if err = t.putMessage(message, old); err != nil {
		return err
	}
	if err = t.invalidateRecalledMessageMemory(message); err != nil {
		return err
	}
	inputs, err := t.ListInbox(message.ScopeID)
	if err != nil {
		return err
	}
	for _, input := range inputs {
		if input.MessageID != message.ID {
			continue
		}
		if input.State == work.InboxPending {
			revision := input.Revision
			input.Revision++
			input.State = work.InboxCancelled
			if err = t.PutInbox(input, revision); err != nil {
				return err
			}
		} else if input.State == work.InboxClaimed || input.State == work.InboxProcessed {
			noticeID := v.ID + ":" + input.ID
			text := "飞书已撤回一条先前输入。撤回不等于撤销已经发生的外部操作。请核查这条来源对当前工作和已保存事实的影响；需要新的授权时向负责人或用户确认。来源消息：" + message.ID
			body := work.NewContent(noticeID+":body", message.ScopeID, "text/plain", []byte(text), v.ReceivedAt)
			if err = t.PutContent(body); err != nil {
				return err
			}
			notice := work.Message{ID: noticeID, ScopeID: message.ScopeID, ConversationID: message.ConversationID, Source: work.Actor{Kind: work.ActorSystem, ID: "platform-observer"}, BodyRef: body.ID, DerivedFrom: &work.SourceRef{Kind: "platform_recall", ID: v.ID, Version: "1", EvidenceRef: evidence.ID}, Revision: 1, CreatedAt: v.ReceivedAt}
			_, _, err = t.AcceptMessage(notice, []work.Inbox{{ID: noticeID + ":inbox", ScopeID: input.ScopeID, RecipientRoleID: input.RecipientRoleID, RecipientContextID: input.RecipientContextID, RelatedTaskID: input.TaskID, EventID: noticeID, State: work.InboxPending, Revision: 1}})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *Tx) invalidateRecalledMessageMemory(message work.Message) error {
	entries, err := listRecords[work.MemoryEntry](t, "memories", " WHERE scope_id=? AND state='valid'", message.ScopeID)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		for _, source := range entry.Sources {
			if source.Kind == "message" && source.ID == message.ID {
				before := entry.Revision
				entry.Revision++
				entry.State = work.MemoryInvalidated
				if err = t.PutMemory(entry, before); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}
