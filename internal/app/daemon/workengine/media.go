package workengine

import (
	"strings"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func nextPrivateInputBatch(tx *workstore.Tx, inputs []work.Inbox) ([]work.Inbox, error) {
	var batch []work.Inbox
	for _, input := range inputs {
		if input.AwaitingText {
			batch = append(batch, input)
			continue
		}
		candidate := append(batch, input)
		ready, err := tx.MediaBatchHasHumanInstruction(candidate)
		if err != nil {
			return nil, err
		}
		if !ready {
			// Independent notices cannot supply an instruction for staged media.
			// Leave them queued and keep looking for the human's actual request.
			continue
		}
		within, err := tx.MediaBatchWithinBudget(candidate)
		if err != nil {
			return nil, err
		}
		if !within {
			return nil, nil
		}
		// A verified missing-resume notice belongs to the rejected attempt's
		// retried input, not to a separate ordinary conversation turn.
		for _, pending := range inputs {
			const prefix = "native-resume-recovery:"
			name, ok := strings.CutPrefix(pending.ID, prefix)
			attemptID, valid := strings.CutSuffix(name, ":inbox")
			if !ok || !valid || pending.MessageID != prefix+attemptID+":message" {
				continue
			}
			message, err := tx.GetMessage(pending.MessageID)
			if err != nil {
				return nil, err
			}
			if message.Source.Kind != work.ActorSystem || message.Source.ID != "runtime-recovery" {
				continue
			}
			attempt, err := tx.GetAttempt(attemptID)
			if err != nil {
				return nil, err
			}
			if attempt.ScopeID != input.ScopeID || attempt.ContextID != input.RecipientContextID {
				continue
			}
			for _, selected := range candidate {
				for _, originalID := range attempt.InputIDs {
					if selected.ID == originalID {
						candidate = append(candidate, pending)
						goto nextNotice
					}
				}
			}
		nextNotice:
		}
		return candidate, nil
	}
	return nil, nil
}
