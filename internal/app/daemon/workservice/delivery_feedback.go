package workservice

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type relatedDelivery struct {
	ID         string             `json:"id"`
	Purpose    string             `json:"purpose"`
	References []work.DeliveryRef `json:"references"`
	ObservedAt time.Time          `json:"feedback_received_at"`
}

func deliveryForFeedback(tx *workstore.Tx, message work.Message) (*relatedDelivery, error) {
	if message.Source.Kind != work.ActorPrincipal || message.PlatformKey == nil {
		return nil, nil
	}
	key := *message.PlatformKey
	record, err := tx.GetIngressByPlatformMessage(key.Tenant, key.ChatID, key.MessageID)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if record.Route.ScopeID != message.ScopeID {
		return nil, nil
	}
	for _, target := range []string{record.Envelope.ParentID, record.Envelope.RootID, record.Envelope.ThreadID} {
		if target == "" {
			continue
		}
		mapping, err := tx.GetPlatformMessageMapping(key.Tenant, key.ChatID, target)
		if errors.Is(err, workstore.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if mapping.ScopeID != message.ScopeID || mapping.ChannelGeneration != record.Route.BindingGeneration {
			return nil, nil
		}
		if mapping.OutboxID == "" {
			continue
		}
		outbox, err := tx.GetOutbox(mapping.OutboxID)
		if err != nil {
			return nil, err
		}
		if outbox.State != work.OutboxSent || outbox.ExternalMessageID != target || outbox.ScopeID != message.ScopeID || outbox.Target != key.ChatID || outbox.OperationID != mapping.OperationID {
			return nil, nil
		}
		operation, err := tx.GetOperation(outbox.OperationID)
		if err != nil {
			return nil, err
		}
		if operation.State != work.OperationConfirmed || operation.ScopeID != message.ScopeID {
			return nil, nil
		}
		deliveryID := outbox.EventID
		ui := operation.Actor.Kind == work.ActorSystem && operation.Actor.ID == "team-ui"
		if ui {
			body, err := tx.GetContent(message.ScopeID, outbox.PayloadRef)
			if err != nil {
				return nil, err
			}
			var projection MessageProjection
			if err = json.Unmarshal(body.Data, &projection); err != nil {
				return nil, err
			}
			view, err := tx.GetUIView(projection.UIViewID)
			if err != nil {
				return nil, err
			}
			if view.Kind != "project.artifacts" || view.ScopeID != message.ScopeID || view.ChatID != key.ChatID || view.ExternalMessageID != target || view.OperationID != operation.ID {
				continue
			}
			deliveryID = view.TargetID
		}
		delivery, err := tx.GetDelivery(message.ScopeID, deliveryID)
		if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrScopeDenied) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if delivery.ConversationTarget != key.ChatID || (!ui && delivery.NotificationOutboxID != outbox.ID) {
			return nil, nil
		}
		return &relatedDelivery{ID: delivery.ID, Purpose: delivery.Purpose, References: delivery.References, ObservedAt: record.Envelope.ReceivedAt}, nil
	}
	return nil, nil
}
