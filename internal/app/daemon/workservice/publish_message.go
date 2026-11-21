package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PublishMessageRequest struct {
	Key            string `json:"key"`
	Body           string `json:"body"`
	ReplyToInputID string `json:"reply_to_input_id,omitempty"`
}

// MessageProjection freezes the sending identity and destination. A later Bot
// rebinding or channel switch cannot retarget an already accepted publication.
type MessageProjection struct {
	ReplaceMessageID  string          `json:"replace_message_id,omitempty"`
	Card              json.RawMessage `json:"card,omitempty"`
	UIViewID          string          `json:"ui_view_id,omitempty"`
	RoleID            string          `json:"role_id"`
	GatewayRef        string          `json:"gateway_ref"`
	AppID             string          `json:"app_id"`
	BotGeneration     int64           `json:"bot_generation"`
	Tenant            string          `json:"tenant"`
	ChatID            string          `json:"chat_id"`
	ChannelGeneration int64           `json:"channel_generation"`
	ReplyToMessageID  string          `json:"reply_to_message_id,omitempty"`
	TaskID            string          `json:"task_id,omitempty"`
	RootMessageID     string          `json:"root_message_id,omitempty"`
	Body              string          `json:"body"`
}

func (s *Service) PublishMessage(ctx context.Context, token, runRef string, req PublishMessageRequest) (Result, error) {
	var result Result
	claims, err := s.signer.Verify(token)
	if err != nil {
		return result, err
	}
	var bootstrap bool
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		scope, e := tx.GetScope(claims.ScopeID)
		if e != nil {
			return workauth.ErrUnauthorized
		}
		bootstrap = scope.Kind == work.ScopeBootstrap
		return nil
	})
	if err != nil {
		return result, err
	}
	apply := func(tx *workstore.Tx, c Caller) error {
		id := stableID("publication", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		var err error
		result, err = s.mutate(tx, c, "message.publish", req.Key, id, req, func() (string, int64, error) {
			if strings.TrimSpace(req.Body) == "" {
				return "", 0, fmt.Errorf("publication body is required")
			}
			projection, e := s.messageDestination(tx, c, req.ReplyToInputID)
			if e != nil {
				return "", 0, e
			}
			projection.Body = req.Body
			body, e := json.Marshal(projection)
			if e != nil {
				return "", 0, e
			}
			content := work.NewContent(stableID("body", id), c.Worker.ScopeID, "application/json", body, s.now())
			if e = tx.PutContent(content); e != nil {
				return "", 0, e
			}
			hash := sha256.Sum256(body)
			op := work.Operation{ID: id, ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "feishu.publish", TargetID: projection.ChatID, TargetGeneration: projection.BotGeneration, IdempotencyKey: id, RequestRef: content.ID, RequestHash: hex.EncodeToString(hash[:]), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
			if _, _, e = tx.PutOperation(op, 0); e != nil {
				return "", 0, e
			}
			outbox := work.Outbox{ID: stableID("outbox", id), ScopeID: c.Worker.ScopeID, OperationID: id, Target: projection.ChatID, EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}
			if e = tx.PutOutbox(outbox, 0); e != nil {
				return "", 0, e
			}
			return id, 1, nil
		})
		return err
	}
	if bootstrap {
		err = s.store.Within(ctx, func(tx *workstore.Tx) error {
			c, _, err := s.authorizeBootstrapWorker(tx, claims, runRef)
			if err != nil {
				return err
			}
			return apply(tx, c)
		})
	} else {
		err = s.withManagementWorker(ctx, token, runRef, apply)
	}
	if err == nil {
		result.Status = "accepted"
	}
	return result, err
}

func (s *Service) messageDestination(tx *workstore.Tx, c Caller, replyInputID string) (MessageProjection, error) {
	var target MessageProjection
	target.TaskID = c.Attempt.TaskID
	binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
	if err != nil {
		return target, err
	}
	target.RoleID, target.GatewayRef, target.AppID, target.BotGeneration = binding.RoleID, binding.GatewayRef, binding.AppID, binding.IdentityGeneration
	scope, err := tx.GetScope(c.Worker.ScopeID)
	if err != nil {
		return target, err
	}
	if scope.Kind == work.ScopeBootstrap {
		target.Tenant, target.ChatID, target.ChannelGeneration = scope.Tenant, scope.ChatID, scope.BindingGeneration
	}
	if scope.Kind == work.ScopeProject {
		channels, e := tx.ListProjectChannelBindings(scope.ProjectID)
		if e != nil {
			return target, e
		}
		for _, channel := range channels {
			if channel.Active && channel.Purpose == "main" {
				if target.ChatID != "" {
					return target, fmt.Errorf("ambiguous project channel")
				}
				target.Tenant, target.ChatID, target.ChannelGeneration = channel.Tenant, channel.ChatID, channel.BindingGeneration
			}
		}
	}
	if replyInputID == "" && scope.Kind == work.ScopePersonal {
		destination, e := tx.GetPersonalContextDestination(scope.ID, c.Worker.ContextID, c.Worker.RoleID)
		if e != nil {
			return target, e
		}
		target.Tenant, target.ChatID = destination.Tenant, destination.ChatID
		for _, id := range c.Attempt.InputIDs {
			input, e := tx.GetInbox(id)
			if e != nil {
				return target, e
			}
			if input.MessageID != "" {
				message, e := tx.GetMessage(input.MessageID)
				if e != nil {
					return target, e
				}
				if message.Source == (work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}) && message.PlatformKey != nil && message.PlatformKey.Tenant == target.Tenant && message.PlatformKey.ChatID == target.ChatID {
					replyInputID = id
					break
				}
			}
		}
	}
	if replyInputID != "" {
		input, e := tx.GetInbox(replyInputID)
		if e != nil {
			return target, e
		}
		if input.ScopeID != scope.ID || input.MessageID == "" {
			return target, workauth.ErrUnauthorized
		}
		message, e := tx.GetMessage(input.MessageID)
		if e != nil {
			return target, e
		}
		// Project establishment adopts a genuine bootstrap message by
		// provenance. It does not copy its canonical platform dedup key.
		if message.PlatformKey == nil && message.DerivedFrom != nil && message.DerivedFrom.Kind == "message" {
			source, sourceErr := tx.GetMessage(message.DerivedFrom.ID)
			if sourceErr != nil {
				return target, sourceErr
			}
			if source.Source != message.Source {
				return target, workauth.ErrUnauthorized
			}
			message = source
		}
		if message.PlatformKey == nil {
			return target, fmt.Errorf("input has no platform reply target")
		}
		key := message.PlatformKey
		if scope.Kind == work.ScopePersonal {
			if message.Source.Kind != work.ActorPrincipal || message.Source.ID != scope.PrincipalID {
				return target, workauth.ErrUnauthorized
			}
			target.Tenant, target.ChatID = key.Tenant, key.ChatID
		} else if target.Tenant != key.Tenant || target.ChatID != key.ChatID {
			return target, workauth.ErrUnauthorized
		}
		target.ReplyToMessageID = key.MessageID
		mapping, e := tx.GetPlatformMessageMapping(key.Tenant, key.ChatID, key.MessageID)
		if e == nil {
			if mapping.ScopeID == scope.ID {
				target.RootMessageID = mapping.RootMessageID
			}
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return target, e
		}
		if target.TaskID == "" {
			target.TaskID = input.RelatedTaskID
		}
	}
	if target.TaskID != "" {
		conversation, e := tx.GetTaskConversation(scope.ID, target.TaskID)
		if e == nil {
			if conversation.Tenant != target.Tenant || conversation.ChatID != target.ChatID || conversation.ChannelGeneration != target.ChannelGeneration {
				return target, fmt.Errorf("task conversation needs explicit channel reconciliation")
			}
			if target.RootMessageID != "" && target.RootMessageID != conversation.RootMessageID {
				return target, fmt.Errorf("reply target belongs to another task conversation")
			}
			target.RootMessageID = conversation.RootMessageID
			if target.ReplyToMessageID == "" {
				target.ReplyToMessageID = conversation.RootMessageID
			}
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return target, e
		}
	}
	if target.Tenant == "" || target.ChatID == "" {
		return target, fmt.Errorf("no unambiguous conversation destination")
	}
	return target, nil
}
