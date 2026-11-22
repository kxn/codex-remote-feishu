package workruntime

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workdisplay"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) applyNativeOutput(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) error {
	if event.TrafficClass == agentproto.TrafficClassInternalHelper || event.ThreadID == "" || event.TurnID == "" {
		return nil
	}
	scope, err := tx.GetScope(carrier.Owner.ScopeID)
	if err != nil || scope.Kind != work.ScopePersonal && event.ItemKind != "agent_message" {
		return err
	}
	turn, err := tx.GetNativeTurn(carrier.Incarnation, event.ThreadID, event.TurnID)
	if errors.Is(err, workstore.ErrNotFound) {
		return tx.QuarantineRuntimeEvent(event, "native_display_ownership_unproven")
	}
	if err != nil {
		return err
	}
	binding, err := c.resolveBinding(tx, carrier, event)
	if err != nil || binding != turn.Binding {
		return nil
	}
	context, err := tx.GetContext(binding.ContextID)
	if err != nil {
		return err
	}
	if context.Purpose == work.PurposeReview {
		if c.options.ApplyReviewEvent != nil && event.Kind == agentproto.EventItemCompleted && event.ItemKind == "agent_message" {
			return c.options.ApplyReviewEvent(tx, carrier, binding, event)
		}
		return nil
	}
	return workdisplay.Apply(tx, carrier, binding, event, c.options.Clock())
}

func (c *Coordinator) applyPrivateDisplay(tx *workstore.Tx, carrier workstore.CarrierRegistration, binding agentproto.ExecutionBinding, event agentproto.Event) error {
	if event.TrafficClass == agentproto.TrafficClassInternalHelper || binding.AttemptID == "" {
		return nil
	}
	return workdisplay.Apply(tx, carrier, binding, event, c.options.Clock())
}
