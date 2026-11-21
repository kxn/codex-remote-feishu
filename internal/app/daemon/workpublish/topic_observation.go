package workpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ObserveTaskTopic performs a bounded read to recover from missed recall
// events. It never infers deletion from a read error and never creates a root.
func (p *Publisher) ObserveTaskTopic(ctx context.Context, scopeID, taskID string) error {
	var op work.Operation
	var req workstore.TopicReadRequest
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		conversation, e := tx.GetTaskConversation(scopeID, taskID)
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		if e != nil {
			return e
		}
		unavailable, e := tx.TaskTopicUnavailable(conversation)
		if e != nil || unavailable {
			return e
		}
		mapping, e := tx.GetPlatformMessageMapping(conversation.Tenant, conversation.ChatID, conversation.RootMessageID)
		if e != nil {
			return e
		}
		binding, e := tx.GetRoleBotBinding(mapping.RoleID)
		if e != nil {
			return e
		}
		if mapping.ScopeID != scopeID || binding.IdentityGeneration != mapping.BotGeneration {
			return workstore.ErrConflict
		}
		req = workstore.TopicReadRequest{Conversation: conversation, Binding: binding}
		raw, e := json.Marshal(req)
		if e != nil {
			return e
		}
		key := sha256.Sum256(append(raw, []byte(p.now().UTC().Truncate(time.Minute).Format(time.RFC3339))...))
		id := "topic-read:" + hex.EncodeToString(key[:])
		if _, e = tx.GetOperation(id); e == nil {
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		content := work.NewContent(id+":request", scopeID, "application/json", raw, p.now())
		if e = tx.PutContent(content); e != nil {
			return e
		}
		op = work.Operation{ID: id, ScopeID: scopeID, Actor: work.Actor{Kind: work.ActorRole, ID: binding.RoleID}, Kind: "platform.topic.read", TargetID: conversation.RootMessageID, TargetGeneration: binding.IdentityGeneration, IdempotencyKey: id, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationSending, Revision: 1, CreatedAt: p.now(), UpdatedAt: p.now()}
		_, _, e = tx.PutOperation(op, 0)
		return e
	})
	if err != nil || op.ID == "" {
		return err
	}
	sender, err := p.resolver.Resolve(ctx, req.Binding)
	var result workstore.TopicReadResult
	if err == nil {
		reader, ok := sender.(publicationObserver)
		if !ok {
			err = fmt.Errorf("message read capability is unavailable")
		} else {
			observed, e := reader.ReadPublishedMessage(ctx, req.Conversation.RootMessageID)
			err = e
			if err == nil {
				result = workstore.TopicReadResult{MessageID: observed.MessageID, ChatID: observed.ChatID, Deleted: observed.Deleted}
				if result.MessageID != req.Conversation.RootMessageID || result.ChatID != req.Conversation.ChatID {
					err = fmt.Errorf("topic observation returned a different message")
				}
			}
		}
	}
	readErr := err
	err = p.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetOperation(op.ID)
		if e != nil {
			return e
		}
		if current.Revision != op.Revision {
			return workstore.ErrConflict
		}
		op.Revision++
		op.UpdatedAt = p.now()
		if readErr != nil {
			op.State = work.OperationTerminal
			op.LastError = readErr.Error()
		} else {
			raw, e := json.Marshal(result)
			if e != nil {
				return e
			}
			content := work.NewContent(op.ID+":result", scopeID, "application/json", raw, p.now())
			if e = tx.PutContent(content); e != nil {
				return e
			}
			op.State = work.OperationConfirmed
			op.ExternalReceipt = content.ID
		}
		_, _, e = tx.PutOperation(op, current.Revision)
		return e
	})
	return errors.Join(readErr, err)
}
