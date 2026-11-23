package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// archiveStoppedPersonalReads marks no result as successful: it preserves
// immutable evidence that a read-only command's old result is unavailable.
func (r *teamRuntime) archiveStoppedPersonalReads(ctx context.Context, scope string, now time.Time) error {
	var candidates []workstore.PersonalReadArchiveCandidate
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		candidates, e = tx.PersonalReadArchiveCandidates(scope)
		return e
	}); err != nil {
		return err
	}
	for _, v := range candidates {
		if v.Command != nil {
			if v.Process == nil || r.codexUpgradeHostID == "" || v.Process.HostID != r.codexUpgradeHostID {
				continue
			}
			probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
			err := hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: v.Process.Platform, PID: v.Process.PID, StartIdentity: v.Process.StartIdentity})
			cancel()
			if err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				continue
			}
		}
		if err := r.store.Within(ctx, func(tx *workstore.Tx) error { return tx.RecordPersonalReadArchive(scope, v, now) }); err != nil {
			return err
		}
	}
	return nil
}

func (r *teamRuntime) holdUncertainPersonalPublications(ctx context.Context, scope, proofID string, now time.Time) error {
	if err := r.waitPersonalSourceActivities(ctx, scope); err != nil {
		return err
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error {
		boxes, err := tx.ListOutbox(scope)
		if err != nil {
			return err
		}
		for _, out := range boxes {
			if out.State != work.OutboxSending && out.State != work.OutboxUnknown {
				continue
			}
			err = tx.HoldPersonalUncertainPublication(scope, out.ID, proofID, now)
			if errors.Is(err, workstore.ErrConflict) {
				continue
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *teamRuntime) holdUncertainPersonalReactions(ctx context.Context, scope, proofID string, now time.Time) error {
	if err := r.waitPersonalSourceActivities(ctx, scope); err != nil {
		return err
	}
	return r.store.Within(ctx, func(tx *workstore.Tx) error {
		reactions, err := tx.ListUncertainPersonalReactions(scope)
		if err != nil {
			return err
		}
		for _, reaction := range reactions {
			if err = tx.HoldPersonalUncertainReaction(scope, reaction.ID, proofID, now); err != nil {
				return err
			}
		}
		return nil
	})
}
