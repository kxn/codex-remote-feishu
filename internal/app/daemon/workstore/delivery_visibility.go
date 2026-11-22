package workstore

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// A saved delivery becomes visible to people when its own notice has a
// confirmed receipt for this conversation generation.
func (t *Tx) GetSentDelivery(scopeID, id, tenant, chatID string, generation int64) (work.Delivery, error) {
	delivery, err := t.GetDelivery(scopeID, id)
	if err != nil {
		return delivery, err
	}
	if err = t.requireSentDelivery(delivery, tenant, chatID, generation); err != nil {
		return work.Delivery{}, err
	}
	return delivery, nil
}

func (t *Tx) ListSentDeliveries(scopeID, tenant, chatID string, generation int64) ([]work.Delivery, error) {
	deliveries, err := t.ListDeliveries(scopeID)
	if err != nil {
		return nil, err
	}
	visible := make([]work.Delivery, 0, len(deliveries))
	for _, delivery := range deliveries {
		err = t.requireSentDelivery(delivery, tenant, chatID, generation)
		if errors.Is(err, ErrScopeDenied) || errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		visible = append(visible, delivery)
	}
	return visible, nil
}

func (t *Tx) requireSentDelivery(delivery work.Delivery, tenant, chatID string, generation int64) error {
	if delivery.ConversationTarget != chatID {
		return ErrScopeDenied
	}
	item, err := t.GetOutbox(delivery.NotificationOutboxID)
	if err != nil {
		return err
	}
	if item.State != work.OutboxSent || item.ExternalMessageID == "" {
		return ErrScopeDenied
	}
	mapping, err := t.GetPlatformMessageMapping(tenant, chatID, item.ExternalMessageID)
	if err != nil {
		return err
	}
	if mapping.OutboxID != item.ID || mapping.OperationID != item.OperationID ||
		mapping.ScopeID != delivery.ScopeID || mapping.ChannelGeneration != generation {
		return ErrScopeDenied
	}
	return nil
}
