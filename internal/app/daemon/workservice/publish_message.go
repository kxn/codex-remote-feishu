package workservice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type PublishMessageRequest struct {
	Purpose        PublicationPurpose `json:"purpose,omitempty"`
	GrantID        string             `json:"grant_id,omitempty"`
	Key            string             `json:"key"`
	Body           string             `json:"body"`
	ReplyToInputID string             `json:"reply_to_input_id,omitempty"`
	TaskID         string             `json:"task_id,omitempty"`
}

// MessageProjection freezes the sending identity and destination. A later Bot
// rebinding or channel switch cannot retarget an already accepted publication.
type MessageProjection struct {
	Purpose           PublicationPurpose         `json:"purpose,omitempty"`
	Authority         *PublicationAuthority      `json:"authority,omitempty"`
	PrivateDisplay    *PrivateDisplayPublication `json:"private_display,omitempty"`
	SourceRunRef      string                     `json:"source_run_ref,omitempty"`
	Media             *PrivateMediaPublication   `json:"media,omitempty"`
	ReplaceMessageID  string                     `json:"replace_message_id,omitempty"`
	Card              json.RawMessage            `json:"card,omitempty"`
	UIViewID          string                     `json:"ui_view_id,omitempty"`
	RoleID            string                     `json:"role_id"`
	GatewayRef        string                     `json:"gateway_ref"`
	AppID             string                     `json:"app_id"`
	BotGeneration     int64                      `json:"bot_generation"`
	Tenant            string                     `json:"tenant"`
	ChatID            string                     `json:"chat_id"`
	ChannelGeneration int64                      `json:"channel_generation"`
	ReplyToMessageID  string                     `json:"reply_to_message_id,omitempty"`
	TaskID            string                     `json:"task_id,omitempty"`
	RootMessageID     string                     `json:"root_message_id,omitempty"`
	Body              string                     `json:"body"`
}

func (s *Service) PublishMessage(ctx context.Context, token, runRef string, req PublishMessageRequest) (Result, error) {
	var result Result
	err := s.withMessagePublisher(ctx, token, runRef, req.TaskID, func(tx *workstore.Tx, c Caller) error {
		if strings.TrimSpace(req.Body) == "" {
			return publicationArgument("Publication body is required.")
		}
		projection, err := s.authorizePublication(tx, c, req.ReplyToInputID, req.TaskID, req.GrantID, publicationMessage, nil)
		if err != nil {
			return err
		}
		if err = setPublicationPurpose(&projection, req.Purpose, req.Body); err != nil {
			return err
		}
		id := stableID("publication", c.Worker.ScopeID, c.Worker.RoleID, req.Key)
		if projection.Authority.Kind == publicationDelegated {
			id = delegatedPublicationOperationID(c.Worker.ScopeID, projection.Authority.GrantID, publicationMessage)
		}
		if projection.Authority.InputID != "" && projection.Authority.Kind == publicationDirected {
			id = directedPublicationID(c.Worker.ScopeID, c.Worker.RoleID, projection.Authority.InputID)
		}
		result, err = s.mutate(tx, c, "message.publish", req.Key, id, req, func() (string, int64, error) {
			if projection.Purpose == PublicationProgress && projection.Authority.Kind != publicationPrivate {
				final, e := tx.HasFinalPublication(c.Worker.ScopeID, c.Attempt.ID, projection.TaskID, projection.RootMessageID)
				if e != nil {
					return "", 0, e
				}
				if final {
					return "", 0, fmt.Errorf("%w: the final result has already been accepted for this work", ErrPublicationDenied)
				}
			}
			if projection.Authority.Kind == publicationPrivate {
				existing, found, e := FindPrivateDisplayResult(tx, c.Worker.ScopeID, c.Worker.ContextID, c.Attempt.RunRef.RunID, req.Body)
				if e != nil {
					return "", 0, e
				}
				if found {
					return existing.ID, existing.Revision, nil
				}
			}
			if projection.Authority.Kind == publicationDirected || projection.Authority.Kind == publicationDelegated {
				if e := requireFirstBoundPublication(tx, id); e != nil {
					return "", 0, e
				}
			}
			return id, 1, s.queueRolePublication(tx, c, id, projection)
		})
		return err
	})
	if err == nil {
		result.Status = "accepted"
	}
	return result, err
}

func (s *Service) queueRolePublication(tx *workstore.Tx, c Caller, id string, projection MessageProjection) error {
	body, err := json.Marshal(projection)
	if err != nil {
		return err
	}
	content := work.NewContent(stableID("body", id), c.Worker.ScopeID, "application/json", body, s.now())
	if err = tx.PutContent(content); err != nil {
		return err
	}
	hash := sha256.Sum256(body)
	op := work.Operation{ID: id, ScopeID: c.Worker.ScopeID, ContextID: c.Worker.ContextID, Actor: work.Actor{Kind: work.ActorRole, ID: c.Worker.RoleID}, Kind: "feishu.publish", TargetID: projection.ChatID, TargetGeneration: projection.BotGeneration, IdempotencyKey: id, RequestRef: content.ID, RequestHash: hex.EncodeToString(hash[:]), State: work.OperationPrepared, Revision: 1, CreatedAt: s.now(), UpdatedAt: s.now()}
	if _, _, err = tx.PutOperation(op, 0); err != nil {
		return err
	}
	outbox := work.Outbox{ID: stableID("outbox", id), ScopeID: c.Worker.ScopeID, OperationID: id, Target: projection.ChatID, EventID: id, PayloadRef: content.ID, State: work.OutboxPending, Revision: 1}
	return tx.PutOutbox(outbox, 0)
}

func (s *Service) withMessagePublisher(ctx context.Context, token, runRef, taskID string, apply func(*workstore.Tx, Caller) error) error {
	claims, err := s.signer.Verify(token)
	if err != nil {
		return err
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
		if errors.Is(err, workauth.ErrUnauthorized) && taskID != "" {
			err = s.withCompletedTaskPublication(ctx, token, runRef, taskID, apply)
		}
	}
	return err
}

// A task turn may deliver its own result after recording completion. It still
// needs the live run, current membership and the exact completed task version.
func (s *Service) withCompletedTaskPublication(ctx context.Context, token, runRef, taskID string, fn func(*workstore.Tx, Caller) error) error {
	return s.withWorkerReceipt(ctx, token, runRef, func(tx *workstore.Tx, c Caller) error {
		if c.Attempt.TaskID != taskID {
			return workauth.ErrUnauthorized
		}
		task, err := tx.GetTask(taskID)
		if err != nil {
			return err
		}
		if task.State != work.TaskDone || task.OwnerRoleID != c.Worker.RoleID || task.RequirementsRevision != c.Attempt.FrozenRequirementsRevision || task.AssignmentGeneration != c.Attempt.FrozenAssignmentGeneration {
			return workauth.ErrUnauthorized
		}
		binding, err := tx.GetRoleBotBinding(c.Worker.RoleID)
		if err != nil {
			return err
		}
		gates, err := tx.ListPauseGates(c.Worker.ScopeID)
		if err != nil {
			return err
		}
		if err = workauth.AuthorizeRun(c.Claims, runRef, c.Worker, c.Attempt, binding, gates); err != nil {
			return err
		}
		if err = ensureMember(tx, c.Worker.ScopeID, c.Worker.RoleID); err != nil {
			return err
		}
		return fn(tx, c)
	})
}

func (s *Service) messageDestination(tx *workstore.Tx, c Caller, replyInputID, taskID string) (MessageProjection, error) {
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
	if taskID != "" {
		if scope.Kind != work.ScopeProject {
			return target, workauth.ErrUnauthorized
		}
		wc, err := tx.GetContext(c.Worker.ContextID)
		if err != nil {
			return target, err
		}
		if wc.Purpose == work.PurposeReview || wc.Purpose == work.PurposeControl {
			return target, workauth.ErrUnauthorized
		}
		task, err := tx.GetTask(taskID)
		if err != nil {
			return target, err
		}
		if err = mayManageTask(tx, c, task); err != nil {
			return target, err
		}
		target.TaskID = task.ID
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
	if scope.Kind == work.ScopePersonal {
		destination, e := tx.GetPersonalContextDestination(scope.ID, c.Worker.ContextID, c.Worker.RoleID)
		if e != nil {
			return target, e
		}
		target.Tenant, target.ChatID = destination.Tenant, destination.ChatID
		for _, id := range c.Attempt.InputIDs {
			if replyInputID != "" {
				break
			}
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
		if !slices.Contains(c.Attempt.InputIDs, replyInputID) {
			return target, fmt.Errorf("%w: reply requires an input admitted to this run", ErrPublicationDenied)
		}
		input, e := tx.GetInbox(replyInputID)
		if e != nil {
			return target, e
		}
		if input.ScopeID != scope.ID || input.MessageID == "" || input.RecipientRoleID != c.Worker.RoleID || input.ClaimedAttemptID != c.Attempt.ID {
			return target, workauth.ErrUnauthorized
		}
		if taskID != "" && (input.TaskID != "" && input.TaskID != taskID || input.RelatedTaskID != "" && input.RelatedTaskID != taskID) {
			return target, fmt.Errorf("reply input belongs to a different task")
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
			if message.Source.Kind != work.ActorPrincipal || message.Source.ID != scope.PrincipalID || key.Tenant != target.Tenant || key.ChatID != target.ChatID {
				return target, workauth.ErrUnauthorized
			}
		} else if target.Tenant != key.Tenant || target.ChatID != key.ChatID {
			return target, workauth.ErrUnauthorized
		}
		target.ReplyToMessageID = key.MessageID
		mapping, e := tx.GetPlatformMessageMapping(key.Tenant, key.ChatID, key.MessageID)
		if e == nil {
			if mapping.ScopeID == scope.ID {
				if taskID != "" && mapping.TaskID != "" && mapping.TaskID != taskID {
					return target, fmt.Errorf("reply message belongs to a different task")
				}
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

func directedPublicationID(scopeID, roleID, inputID string) string {
	return stableID("publication-directed", scopeID, roleID, inputID)
}

func requireFirstBoundPublication(tx *workstore.Tx, operationID string) error {
	exists, err := boundPublicationExists(tx, operationID)
	if err == nil && exists {
		return fmt.Errorf("%w: this input or delegation already has a publication", ErrPublicationDenied)
	}
	return err
}
