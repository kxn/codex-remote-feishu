package daemon

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
)

// Verify the original component proofs against the current registry state.
// Coverage lineage may advance across daemon restarts, while the immutable
// owner, binding, drain revision and accepted watermark must remain identical.
func (r *teamRuntime) verifyPersonalComponentProofs(ctx context.Context, router *ingressowner.Router, binding ingressowner.OwnerScopeBinding, current ingressowner.DrainProof) error {
	var proofs []ingressowner.DrainProof
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		ids, e := tx.PersonalMigrationComponentProofIDs(binding.ScopeID)
		if e != nil {
			return e
		}
		for _, id := range ids {
			body, e := tx.GetContent(binding.ScopeID, id)
			if e != nil {
				return e
			}
			var p ingressowner.DrainProof
			if json.Unmarshal(body.Data, &p) != nil || id != "migration-intake-proof:"+p.Digest {
				return workstore.ErrConflict
			}
			proofs = append(proofs, p)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err = router.VerifyBoundOwnerMigrationDrain(ctx, binding, current.MigrationID, current); err != nil {
		return err
	}
	for _, p := range proofs {
		if !samePersonalMigrationDrain(p, current) {
			return workstore.ErrConflict
		}
		if err = router.VerifyBoundOwnerMigrationDrain(ctx, binding, current.MigrationID, p); err != nil {
			return fmt.Errorf("verify original component intake proof: %w", err)
		}
	}
	return nil
}

func samePersonalMigrationDrain(a, b ingressowner.DrainProof) bool {
	return a.Owner == b.Owner && a.MigrationID == b.MigrationID && a.Revision == b.Revision && a.AcceptedWatermark == b.AcceptedWatermark && a.BindingDigest == b.BindingDigest
}
