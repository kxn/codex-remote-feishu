package workpublish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type Sender interface {
	PublishText(context.Context, feishu.ProjectPublishRequest) (feishu.ProjectPublishReceipt, error)
}
type CardSender interface {
	PublishCard(context.Context, feishu.ProjectCardPublishRequest) (feishu.ProjectPublishReceipt, error)
}
type CardPatcher interface {
	PatchCard(context.Context, feishu.ProjectCardPatchRequest) (feishu.ProjectPublishReceipt, error)
}
type Resolver interface {
	Resolve(context.Context, work.RoleBotBinding) (Sender, error)
}
type Publisher struct {
	store    *workstore.Store
	resolver Resolver
	now      func() time.Time
}

func New(store *workstore.Store, resolver Resolver, now func() time.Time) (*Publisher, error) {
	if store == nil || resolver == nil {
		return nil, fmt.Errorf("store and explicit bot resolver are required")
	}
	if now == nil {
		now = time.Now
	}
	return &Publisher{store: store, resolver: resolver, now: now}, nil
}

type publication struct {
	Outbox         work.Outbox
	Operation      work.Operation
	Projection     workservice.MessageProjection
	Binding        work.RoleBotBinding
	TerminalReason string
	ReplyInThread  bool
}

func (p *Publisher) RunOnce(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, fmt.Errorf("publication limit must be 1..100")
	}
	var ids []string
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		ids, err = p.selectPublications(tx, limit)
		return err
	})
	if err != nil {
		return 0, err
	}
	completed := 0
	var failures []error
	for _, id := range ids {
		if err = p.send(ctx, id, false); err != nil {
			if errors.Is(err, workstore.ErrConflict) {
				continue
			}
			failures = append(failures, fmt.Errorf("publication %s: %w", id, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		completed++
		if completed == limit {
			break
		}
	}
	return completed, errors.Join(failures...)
}

// RetryUnknown is an explicit recovery action. The platform only deduplicates
// a UUID for one hour, so older uncertain sends need external reconciliation.
func (p *Publisher) RetryUnknown(ctx context.Context, outboxID string) error {
	return p.send(ctx, outboxID, true)
}

func (p *Publisher) send(ctx context.Context, id string, recovery bool) error {
	job, err := p.claim(ctx, id, recovery)
	if err != nil {
		return err
	}
	if job.TerminalReason != "" {
		return nil
	}
	// No SQLite transaction or App-wide lock is held across the network.
	sender, err := p.resolver.Resolve(ctx, job.Binding)
	if err == nil && sender == nil {
		err = fmt.Errorf("configured bot sender is unavailable")
	}
	if err == nil && job.Projection.Media == nil {
		err = p.revalidateClaim(ctx, job)
	}
	notSent := err != nil
	var receipt feishu.ProjectPublishReceipt
	if err == nil {
		uuid := sha256.Sum256([]byte(job.Operation.ID))
		if job.Projection.Media != nil {
			receipt, err, notSent = p.sendPrivateMedia(ctx, sender, &job, hex.EncodeToString(uuid[:20]))
		} else if job.Projection.ReplaceMessageID != "" {
			patcher, ok := sender.(CardPatcher)
			if !ok {
				err = fmt.Errorf("card update adapter is unavailable")
				notSent = true
			} else {
				receipt, err = patcher.PatchCard(ctx, feishu.ProjectCardPatchRequest{ChatID: job.Projection.ChatID, MessageID: job.Projection.ReplaceMessageID, Card: job.Projection.Card})
			}
		} else if len(job.Projection.Card) > 0 {
			cardSender, ok := sender.(CardSender)
			if !ok {
				err = fmt.Errorf("card publication adapter is unavailable")
				notSent = true
			} else {
				receipt, err = cardSender.PublishCard(ctx, feishu.ProjectCardPublishRequest{ChatID: job.Projection.ChatID, ReplyToMessageID: job.Projection.ReplyToMessageID, ReplyInThread: job.ReplyInThread, Card: job.Projection.Card, UUID: hex.EncodeToString(uuid[:20])})
			}
		} else {
			receipt, err = sender.PublishText(ctx, feishu.ProjectPublishRequest{ChatID: job.Projection.ChatID, ReplyToMessageID: job.Projection.ReplyToMessageID, ReplyInThread: job.ReplyInThread, Text: job.Projection.Body, UUID: hex.EncodeToString(uuid[:20])})
		}
	}
	return p.finish(ctx, job, receipt, err, notSent)
}

func (p *Publisher) finish(ctx context.Context, job publication, receipt feishu.ProjectPublishReceipt, sendErr error, notSent bool) error {
	return p.store.Within(ctx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(job.Operation.ID)
		if err != nil {
			return err
		}
		item, err := tx.GetOutbox(job.Outbox.ID)
		if err != nil {
			return err
		}
		if op.Revision != job.Operation.Revision || item.Revision != job.Outbox.Revision {
			return workstore.ErrConflict
		}
		if sendErr == nil && (receipt.MessageID == "" || receipt.ChatID != job.Projection.ChatID) {
			sendErr = fmt.Errorf("sender did not provide matching delivery receipt")
		}
		if sendErr == nil {
			encoded, e := json.Marshal(receipt)
			if e != nil {
				return e
			}
			op.State, op.ExternalReceipt = work.OperationConfirmed, string(encoded)
			item.State, item.ExternalMessageID = work.OutboxSent, receipt.MessageID
		} else if errors.Is(sendErr, workservice.ErrPublicationDenied) {
			op.State, op.LastError, item.State = work.OperationTerminal, sendErr.Error(), work.OutboxTerminal
		} else {
			op.State, op.LastError = work.OperationUnknown, sendErr.Error()
			item.State = work.OutboxUnknown
			var apiErr *feishu.APIError
			if notSent || errors.As(sendErr, &apiErr) && apiErr.Code != 0 && apiErr.StatusCode < 500 {
				// A platform rejection is distinct from a lost network response.
				op.State, item.State = work.OperationRetryable, work.OutboxRetryable
				next := p.now().Add(time.Minute)
				if apiErr != nil && apiErr.RetryAfter > time.Minute {
					next = p.now().Add(apiErr.RetryAfter)
				}
				op.NextAttemptAt, item.NextAttemptAt = &next, &next
			}
		}
		op.Revision++
		op.UpdatedAt = p.now()
		item.Revision++
		if _, _, err = tx.PutOperation(op, job.Operation.Revision); err != nil {
			return err
		}
		if err = tx.PutOutbox(item, job.Outbox.Revision); err != nil {
			return err
		}
		if item.State != work.OutboxSent {
			return nil
		}
		if job.Projection.UIViewID != "" {
			view, e := tx.GetUIView(job.Projection.UIViewID)
			if e != nil {
				return e
			}
			if view.OperationID != op.ID || view.ScopeID != op.ScopeID {
				return fmt.Errorf("UI publication owner mismatch")
			}
			previous := view.Revision
			view.Revision++
			view.ExternalMessageID = receipt.MessageID
			if e = tx.PutUIView(view, previous); e != nil {
				return e
			}
		}
		if job.Projection.ReplaceMessageID != "" {
			// Updating presentation does not rewrite the original message's
			// conversation mapping or create a second platform message.
			mapping, e := tx.GetPlatformMessageMapping(job.Projection.Tenant, receipt.ChatID, receipt.MessageID)
			if e != nil {
				return e
			}
			if mapping.ScopeID != item.ScopeID || mapping.RoleID != job.Binding.RoleID || mapping.BotGeneration != job.Binding.IdentityGeneration {
				return fmt.Errorf("updated message no longer belongs to this publication")
			}
			return nil
		}
		root := job.Projection.RootMessageID
		if root == "" {
			root = job.Projection.ReplyToMessageID
		}
		if root == "" {
			root = receipt.MessageID
		}
		mapping := work.PlatformMessageMapping{Tenant: job.Projection.Tenant, ChatID: receipt.ChatID, MessageID: receipt.MessageID, ScopeID: item.ScopeID, TaskID: job.Projection.TaskID, RootMessageID: root, ChannelGeneration: job.Projection.ChannelGeneration, OperationID: op.ID, OutboxID: item.ID, RoleID: job.Binding.RoleID, BotGeneration: job.Binding.IdentityGeneration}
		if err = tx.PutPlatformMessageMapping(mapping); err != nil {
			return err
		}
		scope, err := tx.GetScope(item.ScopeID)
		if err != nil {
			return err
		}
		if mapping.TaskID != "" && scope.Kind != work.ScopePersonal {
			conversation, e := tx.GetTaskConversation(item.ScopeID, mapping.TaskID)
			if errors.Is(e, workstore.ErrNotFound) {
				return tx.PutTaskConversation(work.TaskConversation{ScopeID: item.ScopeID, TaskID: mapping.TaskID, Tenant: mapping.Tenant, ChatID: mapping.ChatID, RootMessageID: root, ChannelGeneration: mapping.ChannelGeneration, Revision: 1}, 0)
			}
			if e != nil {
				return e
			}
			if conversation.RootMessageID != root || conversation.ChatID != mapping.ChatID {
				return fmt.Errorf("publication no longer matches task conversation")
			}
		}
		return nil
	})
}
