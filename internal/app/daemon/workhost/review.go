package workhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (h *Host) freezeReviewLocation(ctx context.Context, wc work.WorkContext, settings *LaunchSettings) error {
	settings.ReadOnly = wc.Purpose == work.PurposeReview
	if !settings.ReadOnly {
		return nil
	}
	if wc.Review == nil || wc.Review.Validate() != nil {
		return fmt.Errorf("review requires a valid frozen target and resource location")
	}
	return h.store.Within(ctx, func(tx *workstore.Tx) error {
		location, err := tx.GetLocation(wc.Review.LocationID)
		if err != nil {
			return err
		}
		if location.Generation != wc.Review.LocationGeneration {
			return fmt.Errorf("review location generation changed; request a new review")
		}
		settings.Location = location
		return nil
	})
}

func reviewInstructionBundle(wc work.WorkContext, bundle agentproto.InstructionBundle) agentproto.InstructionBundle {
	if wc.Purpose != work.PurposeReview || wc.Review == nil {
		return bundle
	}
	target, _ := json.Marshal(wc.Review.Target)
	text := bundle.Text + "\n\nRead-only review capability: inspect the explicit target below; do not modify code, invoke business CLI operations, create Goals, delegate work, or request writable permissions. Return the review findings as your final native response; the runtime records this dedicated review result. A review result is not task acceptance.\nReview target: " + string(target)
	hash := sha256.Sum256([]byte(text))
	return agentproto.InstructionBundle{Revision: hex.EncodeToString(hash[:]), Text: text}
}
