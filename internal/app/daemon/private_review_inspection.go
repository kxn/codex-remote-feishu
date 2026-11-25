package daemon

import (
	"context"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func (h *privateNativeHost) ephemeralReviewVerifier(ctx context.Context, rec privateCarrierRecord) func(string, agentproto.RuntimeEphemeralReviewProof) bool {
	return func(session string, expected agentproto.RuntimeEphemeralReviewProof) bool {
		proof, err := runtimejournal.ReadPrivateEphemeralReviewProof(ctx, rec.Snapshot, h.stateDir, session)
		return err == nil && proof != nil && *proof == expected
	}
}
