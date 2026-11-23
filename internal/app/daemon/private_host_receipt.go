package daemon

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type privateHostReceiptReducer struct{ host *App }

func (r privateHostReceiptReducer) ReduceHostReceipt(app *privatechat.App, effect privatechat.Effect, raw json.RawMessage) ([]eventcontract.Event, error) {
	if err := (privateHostReducer{r.host}).check(app); err != nil {
		return nil, err
	}
	original, _, err := privatechat.DecodeStoredEffect(effect.Body)
	if err != nil {
		return nil, err
	}
	var receipt privatechat.PublicationReceipt
	if err = json.Unmarshal(raw, &receipt); err != nil {
		return nil, err
	}
	if receipt.Version != 1 || receipt.EffectID != effect.ID || strings.TrimSpace(receipt.PlanHash) == "" {
		return nil, fmt.Errorf("invalid publication receipt identity")
	}
	event, retry, err := decodePrivatePublicationPlan(receipt.Plan)
	if err != nil {
		return nil, err
	}
	if event.Kind != original.Kind || event.SurfaceSessionID != original.SurfaceSessionID || original.Command != nil || original.DaemonCommand != nil || event.Command != nil || event.DaemonCommand != nil {
		return nil, fmt.Errorf("publication plan does not match event")
	}
	surface := app.Service().Surface(event.SurfaceSessionID)
	if surface == nil {
		return nil, fmt.Errorf("publication surface missing")
	}
	if event.GatewayID != "" && event.GatewayID != surface.GatewayID {
		return nil, fmt.Errorf("publication event owner mismatch")
	}
	for i, item := range receipt.Results {
		if item.Ordinal != i {
			return nil, fmt.Errorf("invalid publication result order")
		}
		planned, e := feishu.ThawPrivateOperation(item.Operation)
		if e != nil {
			return nil, e
		}
		var op feishu.PrivateOperationReceipt
		if err = json.Unmarshal(item.Receipt, &op); err != nil {
			return nil, err
		}
		if op.Version != 1 || strings.TrimSpace(op.MessageID) == "" || op.SurfaceSessionID != event.SurfaceSessionID || op.GatewayID != surface.GatewayID || op.ChatID != surface.ChatID || op.DeliveredAt.IsZero() || op.Kind != planned.Kind || op.GatewayID != planned.GatewayID || op.SurfaceSessionID != planned.SurfaceSessionID || op.ChatID != planned.ChatID || op.ReplyToMessageID != planned.ReplyToMessageID || op.ProgressCardStartSeq != planned.ProgressCardStartSeq || op.ProgressCardEndSeq != planned.ProgressCardEndSeq || (planned.MessageID != "" && op.MessageID != planned.MessageID) {
			return nil, fmt.Errorf("publication receipt route mismatch")
		}
		if op.Kind == feishu.OperationAddReaction || op.Kind == feishu.OperationRemoveReaction {
			if op.EmojiType == "" || op.EmojiType != planned.EmojiType || (op.ReactionID == "" && !op.ReactionAbsent) {
				return nil, fmt.Errorf("reaction receipt identity missing")
			}
			if op.Kind == feishu.OperationRemoveReaction && (op.ReactionID != planned.ReactionID || op.ReactionAbsent != planned.ReactionAbsent) {
				return nil, fmt.Errorf("reaction removal identity mismatch")
			}
		}
		if err = r.recordDelivery(event, op); err != nil {
			return nil, err
		}
	}
	return privatePreviewRetryEvents(event, retry, receipt)
}

func (r privateHostReceiptReducer) recordDelivery(event eventcontract.Event, op feishu.PrivateOperationReceipt) error {
	service := r.host.service
	message := strings.TrimSpace(op.MessageID)
	if message == "" {
		return nil
	}
	if kind, ok := outboundSurfaceMessageKind(op.Kind); ok {
		service.RecordSurfaceOutboundMessage(event.SurfaceSessionID, message, kind, op.ReplyToMessageID)
	}
	if payload, ok := requestPayloadFromEvent(event); ok && (op.Kind == feishu.OperationSendCard || op.Kind == feishu.OperationUpdateCard) {
		if err := service.RecordPrivateRequestDelivery(orchestrator.RequestDeliveryReport{SurfaceSessionID: event.SurfaceSessionID, RequestID: payload.View.RequestID, MessageID: message, DeliveredAt: op.DeliveredAt}); err != nil {
			return err
		}
	}
	if op.Kind == feishu.OperationSendCard {
		if payload, ok := blockPayloadFromEvent(event); event.Kind == eventcontract.KindBlockCommitted && ok && payload.Block.Final {
			service.RecordFinalCardMessage(event.SurfaceSessionID, payload.Block, event.SourceMessageID, message, event.DaemonLifecycleID)
		}
		if payload, ok := threadHistoryPayloadFromEvent(event); ok {
			service.RecordThreadHistoryMessage(event.SurfaceSessionID, payload.View.PickerID, message)
		}
		if payload, ok := targetPickerPayloadFromEvent(event); ok {
			service.RecordTargetPickerMessage(event.SurfaceSessionID, payload.View.PickerID, message)
		}
		if payload, ok := pathPickerPayloadFromEvent(event); ok {
			service.RecordPathPickerMessage(event.SurfaceSessionID, payload.View.PickerID, message)
		}
		if payload, ok := pagePayloadFromEvent(event); ok && payload.View.TrackingKey != "" {
			service.RecordPageTrackingMessage(event.SurfaceSessionID, payload.View.TrackingKey, message)
		}
	}
	if payload, ok := execCommandProgressPayloadFromEvent(event); ok && (op.Kind == feishu.OperationSendCard || op.Kind == feishu.OperationUpdateCard || op.Kind == feishu.OperationDeleteMessage) {
		return service.RecordPrivateProgressDelivery(event.SurfaceSessionID, payload.Progress.ThreadID, payload.Progress.TurnID, payload.Progress.ItemID, message, op.ProgressCardStartSeq, op.ProgressCardEndSeq, op.Kind == feishu.OperationDeleteMessage)
	}
	return nil
}
