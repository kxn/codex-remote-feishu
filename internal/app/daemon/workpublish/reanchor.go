package workpublish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// ReanchorRequest is a trusted SDK action, never a worker tool. The clicked
// recovery card itself becomes the explicit new root; the original send stays
// unknown, with its frozen payload and topic reservation intact for audit.
type ReanchorRequest struct {
	InteractionID, ViewID, OutboxID, OperationID, TaskID string
	OperationRevision, OutboxRevision, TopicRevision     int64
}

func (p *Publisher) Reanchor(ctx context.Context, req ReanchorRequest) error {
	return p.store.Within(ctx, func(tx *workstore.Tx) error {
		interaction, err := tx.GetHumanInteraction(req.InteractionID)
		if err != nil {
			return err
		}
		view, err := tx.GetUIView(req.ViewID)
		if err != nil {
			return err
		}
		if err = tx.ValidateViewInteraction(view, interaction); err != nil {
			return err
		}
		if interaction.Envelope.Values["team_view"] != req.ViewID || interaction.Envelope.Values["action"] != "publication.reanchor" {
			return workstore.ErrScopeDenied
		}
		if view.Kind != "publication.recovery" || view.TargetID != req.OutboxID || view.TargetRevision != req.OperationRevision {
			return workstore.ErrScopeDenied
		}
		auditID := "topic-reanchor:" + interaction.ID
		if old, e := tx.GetOperation(auditID); e == nil {
			if old.TargetID != req.TaskID || old.ScopeID != view.ScopeID || old.ExternalReceipt != interaction.Envelope.MessageID {
				return workstore.ErrIdempotencyConflict
			}
			return nil
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		scope, err := tx.GetScope(view.ScopeID)
		if err != nil {
			return err
		}
		if scope.Kind == work.ScopeProject {
			if err = tx.RequireProjectPrincipalAccess(scope.ID, interaction.PrincipalID, p.now()); err != nil {
				return err
			}
		}
		item, err := tx.GetOutbox(req.OutboxID)
		if err != nil {
			return err
		}
		op, err := tx.GetOperation(req.OperationID)
		if err != nil {
			return err
		}
		if item.ScopeID != view.ScopeID || item.OperationID != op.ID || item.Revision != req.OutboxRevision || op.Revision != req.OperationRevision {
			return workstore.ErrConflict
		}
		for _, step := range op.Steps {
			if step.ID == "topic_reanchored" {
				return workstore.ErrConflict
			}
		}
		body, err := tx.GetContent(item.ScopeID, item.PayloadRef)
		if err != nil {
			return err
		}
		var projection workservice.MessageProjection
		if err = json.Unmarshal(body.Data, &projection); err != nil {
			return err
		}
		if req.TaskID == "" || projection.TaskID != req.TaskID || projection.ChatID != view.ChatID || projection.Tenant != view.Tenant || projection.ChannelGeneration != view.ChannelGeneration || projection.RoleID != view.Binding.RoleID || projection.AppID != view.Binding.AppID || projection.BotGeneration != view.Binding.IdentityGeneration {
			return workstore.ErrScopeDenied
		}
		if req.TopicRevision == 0 {
			if item.State != work.OutboxUnknown || op.State != work.OperationUnknown {
				return workstore.ErrConflict
			}
			reservation, e := tx.GetTaskTopicReservation(item.ScopeID, req.TaskID)
			if e != nil {
				return e
			}
			if reservation != op.ID {
				return workstore.ErrConflict
			}
			if _, e = tx.GetTaskConversation(item.ScopeID, req.TaskID); e == nil {
				return workstore.ErrConflict
			} else if !errors.Is(e, workstore.ErrNotFound) {
				return e
			}
		} else {
			if item.State != work.OutboxUnknown && item.State != work.OutboxTerminal && item.State != work.OutboxRetryable {
				return workstore.ErrConflict
			}
			if op.State != work.OperationUnknown && op.State != work.OperationTerminal && op.State != work.OperationRetryable {
				return workstore.ErrConflict
			}
			conversation, e := tx.GetTaskConversation(item.ScopeID, req.TaskID)
			if e != nil {
				return e
			}
			if conversation.Revision != req.TopicRevision || conversation.Tenant != view.Tenant || conversation.ChatID != view.ChatID || conversation.ChannelGeneration != view.ChannelGeneration {
				return workstore.ErrConflict
			}
			recalled, e := tx.TaskTopicUnavailable(conversation)
			if e != nil {
				return e
			}
			if !recalled {
				return fmt.Errorf("topic root has no authenticated recall or exact deletion observation")
			}
		}
		// A confirmed recovery-card receipt is needed. A click on a card whose own
		// send is unknown must first reconcile that card, not fabricate its mapping.
		mapping, err := tx.GetPlatformMessageMapping(view.Tenant, view.ChatID, interaction.Envelope.MessageID)
		if err != nil {
			return err
		}
		if mapping.ScopeID != view.ScopeID || mapping.OperationID != view.OperationID || mapping.RoleID != view.Binding.RoleID || mapping.BotGeneration != view.Binding.IdentityGeneration {
			return fmt.Errorf("recovery card has no matching publication receipt")
		}
		raw, err := json.Marshal(req)
		if err != nil {
			return err
		}
		content := work.NewContent(auditID+":request", view.ScopeID, "application/json", raw, p.now())
		if err = tx.PutContent(content); err != nil {
			return err
		}
		audit := work.Operation{ID: auditID, ScopeID: view.ScopeID, Actor: work.Actor{Kind: work.ActorPrincipal, ID: interaction.PrincipalID}, Kind: "task.topic.reanchor", TargetID: req.TaskID, TargetGeneration: 1, IdempotencyKey: interaction.ID, RequestHash: content.SHA256, RequestRef: content.ID, State: work.OperationConfirmed, ExternalReceipt: interaction.Envelope.MessageID, Revision: 1, CreatedAt: p.now(), UpdatedAt: p.now()}
		if _, _, err = tx.PutOperation(audit, 0); err != nil {
			return err
		}
		op.Steps = append(op.Steps, work.OperationStep{ID: "topic_reanchored", State: work.OperationConfirmed, Receipt: audit.ID})
		op.Revision++
		op.UpdatedAt = p.now()
		if _, _, err = tx.PutOperation(op, req.OperationRevision); err != nil {
			return err
		}
		if req.TopicRevision > 0 {
			return tx.ReanchorRecalledConversation(view.ScopeID, req.TaskID, audit.ID, req.TopicRevision)
		}
		return tx.PutTaskConversation(work.TaskConversation{ScopeID: view.ScopeID, TaskID: req.TaskID, Tenant: view.Tenant, ChatID: view.ChatID, RootMessageID: interaction.Envelope.MessageID, ChannelGeneration: view.ChannelGeneration, Revision: 1}, 0)
	})
}
