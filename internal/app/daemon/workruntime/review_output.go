package workruntime

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) applyReviewOutput(tx *workstore.Tx, carrier workstore.CarrierRegistration, event agentproto.Event) error {
	binding, err := c.resolveBinding(tx, carrier, event)
	if err != nil {
		// A final item can precede turn/start's response. Keep its exact journal
		// identity and replay after the native turn mapping becomes proven.
		return tx.QuarantineRuntimeEvent(event, "review_output_ownership_unproven")
	}
	context, err := tx.GetContext(binding.ContextID)
	if err != nil {
		return err
	}
	if context.Purpose != work.PurposeReview {
		return nil
	}
	return c.options.ApplyReviewEvent(tx, carrier, binding, event)
}
