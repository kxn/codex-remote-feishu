package orchestrator

import (
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
)

// PrivatePublicationOrderScopes identifies causal operations, not replacement
// message targets. Several different cards can belong to one operation.
// It is read only and is called while freezing the original reduction.
func (s *Service) PrivatePublicationOrderScopes(surface string, event eventcontract.Event) []string {
	if !s.config.PrivateApplication {
		return nil
	}
	return FrozenPrivatePublicationOrderScopes(event)
}

// FrozenPrivatePublicationOrderScopes uses only the immutable event, including
// for journal upgrades. It never borrows the currently active surface.
func FrozenPrivatePublicationOrderScopes(event eventcontract.Event) []string {
	event = event.Normalized()
	var scopes []string
	add := func(kind, id string) {
		if id != "" {
			scopes = append(scopes, kind+":"+id)
		}
	}
	flow := func(context control.FeishuUISurfaceContext) {
		if context.ActiveOwnerCard != nil {
			add("flow", context.ActiveOwnerCard.FlowID)
		}
	}
	if event.PageContext != nil {
		flow(event.PageContext.Surface)
	}
	if event.PathPickerContext != nil {
		flow(event.PathPickerContext.Surface)
	}
	if event.TargetPickerContext != nil {
		flow(event.TargetPickerContext.Surface)
	}
	if event.SelectionContext != nil {
		flow(event.SelectionContext.Surface)
	}
	if event.RequestContext != nil {
		flow(event.RequestContext.Surface)
	}
	if event.ThreadHistoryContext != nil {
		flow(event.ThreadHistoryContext.Surface)
	}
	if event.TargetPickerView != nil {
		add("target-picker", event.TargetPickerView.PickerID)
	}
	if event.PathPickerView != nil {
		add("path-picker", event.PathPickerView.PickerID)
	}
	if event.RequestView != nil {
		add("request", event.RequestView.RequestID)
	}
	if event.PageView != nil {
		add("page", event.PageView.TrackingKey)
	}
	// A recovery acknowledgement and the answer to that same accepted input
	// are causally ordered, while native execution remains independent of UI
	// delivery and unrelated messages retain their own publication lanes.
	if event.TimelineText != nil && event.TimelineText.Type == control.TimelineTextRecoveryInputWaiting || event.Block != nil && event.Block.Final {
		add("input", event.SourceMessageID)
	}
	message := event.Meta.MessageDelivery.TargetMessageID
	if message == "" && event.PageView != nil {
		message = event.PageView.MessageID
	}
	if message == "" && event.PathPickerView != nil {
		message = event.PathPickerView.MessageID
	}
	if message == "" && event.TargetPickerView != nil {
		message = event.TargetPickerView.MessageID
	}
	if message == "" && event.ThreadHistoryView != nil {
		message = event.ThreadHistoryView.MessageID
	}
	if message == "" && event.RequestView != nil {
		message = event.RequestView.MessageID
	}
	if message == "" && event.InlineReplaceCurrentCard {
		message = event.SourceMessageID
	}
	if message != "" {
		add("message", message)
	}
	turn := func(thread, id string) {
		if thread != "" && id != "" {
			add("turn", thread+"\x00"+id)
		}
	}
	if event.Block != nil {
		turn(event.Block.ThreadID, event.Block.TurnID)
	}
	if event.ImageOutput != nil {
		turn(event.ImageOutput.ThreadID, event.ImageOutput.TurnID)
	}
	add(event.Meta.Operation.Kind, event.Meta.Operation.ID)
	return scopes
}
