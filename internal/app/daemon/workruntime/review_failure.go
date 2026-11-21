package workruntime

import (
	"errors"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (c *Coordinator) completeFailedReview(tx *workstore.Tx, attemptID string, request InspectionRequest) error {
	wc, err := tx.GetContext(request.Binding.ContextID)
	if err != nil {
		return err
	}
	if wc.Purpose != work.PurposeReview {
		return nil
	}
	event, err := automationTerminal(tx, request)
	if errors.Is(err, workstore.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if event.TurnCompletionOrigin != agentproto.TurnCompletionOriginRuntime {
		return nil
	}
	reason := workstore.ReviewMissingFinalResult
	if event.Status == "failed" {
		reason = workstore.ReviewFailed
	} else if event.Status == "interrupted" {
		reason = workstore.ReviewInterrupted
	}
	source, err := reviewSource(tx, request.Binding, event)
	if err != nil {
		return err
	}
	return tx.CompleteReviewFailure(attemptID, source, reason)
}
