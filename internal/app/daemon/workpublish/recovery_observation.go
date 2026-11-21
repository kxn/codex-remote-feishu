package workpublish

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type publicationObserver interface {
	ReadPublishedMessage(context.Context, string) (feishu.ObservedPublication, error)
}

// ObserveUnknownCardInteraction proves an exact card version from its original
// signed SDK callback plus same-identity GetMessage. Text-only equality is never
// sufficient, and this method performs no send/patch or topic-root substitution.
func (p *Publisher) ObserveUnknownCardInteraction(ctx context.Context, outboxID, interactionID string) error {
	var job publication
	var interaction workstore.HumanInteraction
	var observation work.Operation
	err := p.store.Within(ctx, func(tx *workstore.Tx) error {
		var err error
		job.Outbox, err = tx.GetOutbox(outboxID)
		if err != nil {
			return err
		}
		job.Operation, err = tx.GetOperation(job.Outbox.OperationID)
		if err != nil {
			return err
		}
		if job.Outbox.State == work.OutboxSent && job.Operation.State == work.OperationConfirmed {
			return nil
		}
		if (job.Outbox.State != work.OutboxUnknown && job.Outbox.State != work.OutboxSending) || (job.Operation.State != work.OperationUnknown && job.Operation.State != work.OperationSending) {
			return workstore.ErrConflict
		}
		if job.Operation.Kind != "feishu.publish" || job.Operation.ScopeID != job.Outbox.ScopeID || job.Operation.RequestRef != job.Outbox.PayloadRef {
			return workstore.ErrScopeDenied
		}
		frozen := ""
		for _, step := range job.Operation.Steps {
			if step.ID == "send_projection" {
				frozen = step.Receipt
			}
		}
		if frozen == "" {
			return fmt.Errorf("actual send projection was never frozen")
		}
		body, err := tx.GetContent(job.Operation.ScopeID, frozen)
		if err != nil {
			return err
		}
		if err = json.Unmarshal(body.Data, &job.Projection); err != nil {
			return err
		}
		if job.Projection.UIViewID == "" || len(job.Projection.Card) == 0 {
			return fmt.Errorf("plain text has no unique card-version callback proof")
		}
		view, err := tx.GetUIView(job.Projection.UIViewID)
		if err != nil {
			return err
		}
		if view.OperationID != job.Operation.ID || view.ScopeID != job.Operation.ScopeID || view.BodyRef != job.Operation.RequestRef {
			return workstore.ErrScopeDenied
		}
		original, err := tx.GetContent(view.ScopeID, view.BodyRef)
		if err != nil {
			return err
		}
		var expected struct {
			Card json.RawMessage `json:"card"`
		}
		if err = json.Unmarshal(original.Data, &expected); err != nil {
			return err
		}
		if string(expected.Card) != string(job.Projection.Card) {
			return fmt.Errorf("card body does not match the immutable view")
		}
		interaction, err = tx.GetHumanInteraction(interactionID)
		if err != nil {
			return err
		}
		actualView, _ := interaction.Envelope.Values["team_view"].(string)
		if actualView != view.ID || interaction.Envelope.Kind != "card" || interaction.Gateway.AppID != job.Projection.AppID || interaction.Gateway.GatewayRef != job.Projection.GatewayRef || interaction.Gateway.BotGeneration != job.Projection.BotGeneration || interaction.Gateway.RoleID != job.Projection.RoleID || interaction.Envelope.ChatID != job.Projection.ChatID || interaction.Envelope.Tenant != job.Projection.Tenant || interaction.Envelope.MessageID == "" {
			return workstore.ErrScopeDenied
		}
		if view.PrincipalID != "" && view.PrincipalID != interaction.PrincipalID {
			return workstore.ErrScopeDenied
		}
		if !cardHasActualCallback(job.Projection.Card, interaction.Envelope.Values) {
			return fmt.Errorf("SDK callback does not match any callback in the original frozen card")
		}
		if view.ExternalMessageID != "" && interaction.Envelope.MessageID != view.ExternalMessageID {
			return workstore.ErrScopeDenied
		}
		if job.Projection.ReplaceMessageID != "" && interaction.Envelope.MessageID != job.Projection.ReplaceMessageID {
			return workstore.ErrScopeDenied
		}
		job.Binding = work.RoleBotBinding{RoleID: job.Projection.RoleID, GatewayRef: job.Projection.GatewayRef, AppID: job.Projection.AppID, IdentityGeneration: job.Projection.BotGeneration}
		current, err := tx.GetRoleBotBinding(job.Binding.RoleID)
		if err != nil {
			return err
		}
		if current != job.Binding {
			return workstore.ErrConflict
		}
		request, _ := json.Marshal(map[string]any{"outbox_id": outboxID, "operation_id": job.Operation.ID, "interaction_id": interactionID, "send_projection_ref": frozen, "send_projection_sha256": body.SHA256, "message_id": interaction.Envelope.MessageID, "binding": job.Binding})
		id := "card-observation:" + job.Operation.ID + ":" + interaction.ID
		intent := work.NewContent(id+":request", view.ScopeID, "application/json", request, p.now())
		if err = tx.PutContent(intent); err != nil {
			return err
		}
		observation = work.Operation{ID: id, ScopeID: view.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "publication-observer"}, Kind: "feishu.message.observe", TargetID: interaction.Envelope.MessageID, TargetGeneration: job.Binding.IdentityGeneration, IdempotencyKey: id, RequestRef: intent.ID, RequestHash: intent.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: p.now(), UpdatedAt: p.now()}
		old, created, err := tx.PutOperation(observation, 0)
		if err != nil {
			return err
		}
		if !created {
			observation = old
		}
		return nil
	})
	if err != nil {
		return err
	}
	if job.Outbox.State == work.OutboxSent {
		return nil
	}
	if observation.State == work.OperationTerminal {
		return fmt.Errorf("previous card observation failed; a fresh SDK observation is required")
	}
	var actual feishu.ObservedPublication
	if observation.State == work.OperationConfirmed {
		err = p.store.Within(ctx, func(tx *workstore.Tx) error {
			content, err := tx.GetContent(observation.ScopeID, observation.ExternalReceipt)
			if err != nil {
				return err
			}
			var saved struct {
				Actual feishu.ObservedPublication `json:"actual"`
			}
			if err = json.Unmarshal(content.Data, &saved); err != nil {
				return err
			}
			actual = saved.Actual
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		sender, err := p.resolver.Resolve(ctx, job.Binding)
		if err != nil {
			return p.failCardObservation(ctx, observation.ID, err)
		}
		reader, ok := sender.(publicationObserver)
		if !ok {
			return p.failCardObservation(ctx, observation.ID, fmt.Errorf("bound sender cannot observe exact platform messages"))
		}
		actual, err = reader.ReadPublishedMessage(ctx, interaction.Envelope.MessageID)
		if err != nil {
			return p.failCardObservation(ctx, observation.ID, err)
		}
	}

	responseRef := observation.ExternalReceipt
	if observation.State != work.OperationConfirmed {
		raw, _ := json.Marshal(struct {
			InteractionID string                     `json:"interaction_id"`
			Actual        feishu.ObservedPublication `json:"actual"`
		}{interaction.ID, actual})
		content := work.NewContent(observation.ID+":response", observation.ScopeID, "application/json", raw, p.now())
		if err = p.store.Within(ctx, func(tx *workstore.Tx) error {
			if err := tx.PutContent(content); err != nil {
				return err
			}
			current, err := tx.GetOperation(observation.ID)
			if err != nil {
				return err
			}
			if current.State == work.OperationConfirmed {
				responseRef = current.ExternalReceipt
				return nil
			}
			expected := current.Revision
			current.State = work.OperationConfirmed
			current.Revision++
			current.ExternalReceipt = content.ID
			current.UpdatedAt = p.now()
			source, _ := json.Marshal(work.SourceRef{Kind: "broker_message_read", ID: current.ID, Version: content.SHA256, EvidenceRef: content.ID})
			current.Steps = []work.OperationStep{{ID: "source", State: work.OperationConfirmed, Receipt: string(source)}}
			_, _, err = tx.PutOperation(current, expected)
			responseRef = content.ID
			return err
		}); err != nil {
			return err
		}
	}
	if actual.Deleted || actual.MessageID != interaction.Envelope.MessageID || actual.ChatID != job.Projection.ChatID || actual.MessageType != "interactive" || actual.SenderType != "app" || actual.SenderIDType != "app_id" || actual.SenderID != job.Binding.AppID {
		return fmt.Errorf("actual message metadata does not match original card publication")
	}
	if job.Projection.ReplyToMessageID != "" {
		if actual.ParentID != job.Projection.ReplyToMessageID {
			return fmt.Errorf("actual reply parent differs from frozen send")
		}
	} else if actual.ParentID != "" || actual.RootID != "" && actual.RootID != actual.MessageID {
		return fmt.Errorf("actual message belongs to a different topic")
	}
	if job.Projection.RootMessageID != "" && actual.RootID != job.Projection.RootMessageID && actual.MessageID != job.Projection.RootMessageID {
		return fmt.Errorf("actual topic root differs from frozen send")
	}
	err = p.store.Within(ctx, func(tx *workstore.Tx) error {
		currentOp, err := tx.GetOperation(job.Operation.ID)
		if err != nil {
			return err
		}
		currentBox, err := tx.GetOutbox(job.Outbox.ID)
		if err != nil {
			return err
		}
		if currentOp.Revision != job.Operation.Revision || currentBox.Revision != job.Outbox.Revision {
			return workstore.ErrConflict
		}
		// Record the observation without changing the immutable send_projection. The
		// finalizer below remains the sole transaction for sent/map/topic receipts.
		stepID := "sdk_card_observation:" + interaction.ID
		for _, step := range currentOp.Steps {
			if step.ID == stepID {
				if step.Receipt != responseRef {
					return workstore.ErrIdempotencyConflict
				}
				job.Operation = currentOp
				return nil
			}
		}
		expected := currentOp.Revision
		currentOp.Revision++
		currentOp.Steps = append(currentOp.Steps, work.OperationStep{ID: stepID, State: work.OperationConfirmed, Receipt: responseRef})
		if _, _, err = tx.PutOperation(currentOp, expected); err != nil {
			return err
		}
		job.Operation = currentOp
		return nil
	})
	if err != nil {
		return err
	}
	return p.finish(ctx, job, feishu.ProjectPublishReceipt{MessageID: actual.MessageID, ChatID: actual.ChatID, RootID: actual.RootID, ParentID: actual.ParentID}, nil, false)
}

func cardHasActualCallback(card json.RawMessage, actual map[string]any) bool {
	var document any
	if json.Unmarshal(card, &document) != nil {
		return false
	}
	wanted, _ := json.Marshal(actual)
	var visit func(any) bool
	visit = func(value any) bool {
		switch node := value.(type) {
		case map[string]any:
			if node["type"] == "callback" {
				if candidate, ok := node["value"].(map[string]any); ok {
					raw, _ := json.Marshal(candidate)
					if string(raw) == string(wanted) {
						return true
					}
				}
			}
			for _, child := range node {
				if visit(child) {
					return true
				}
			}
		case []any:
			for _, child := range node {
				if visit(child) {
					return true
				}
			}
		}
		return false
	}
	return visit(document)
}

func (p *Publisher) failCardObservation(ctx context.Context, id string, cause error) error {
	receiptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := p.store.Within(receiptCtx, func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(id)
		if err != nil {
			return err
		}
		if op.State != work.OperationSending {
			return nil
		}
		expected := op.Revision
		op.State = work.OperationTerminal
		op.Revision++
		op.LastError = cause.Error()
		op.UpdatedAt = p.now()
		_, _, err = tx.PutOperation(op, expected)
		return err
	})
	return errors.Join(cause, err)
}
