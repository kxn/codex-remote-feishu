package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func (r *teamRuntime) verifyPersonalControlProcess(ctx context.Context, v workstore.PersonalControlIsolation) error {
	if r.codexUpgradeHostID == "" || v.Process.HostID != r.codexUpgradeHostID {
		return workstore.ErrConflict
	}
	probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	return hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: v.Process.Platform, PID: v.Process.PID, StartIdentity: v.Process.StartIdentity})
}

func (r *teamRuntime) isolatePersonalControlOperations(ctx context.Context, scope, proofID string, now time.Time) error {
	if err := r.waitPersonalSourceActivities(ctx, scope); err != nil {
		return err
	}
	var operations []work.Operation
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error { var e error; operations, e = tx.ListOperations(scope); return e }); err != nil {
		return err
	}
	for _, op := range operations {
		if op.State != work.OperationUnknown || (op.Kind != "thread.goal.clear" && op.Kind != "native.gate_stop") {
			continue
		}
		var candidate workstore.PersonalControlIsolation
		err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			candidate, e = tx.PersonalControlIsolationCandidate(scope, op.ID)
			return e
		})
		if errors.Is(err, workstore.ErrNotFound) || errors.Is(err, workstore.ErrScopeDenied) || errors.Is(err, workstore.ErrConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if err = r.verifyPersonalControlProcess(ctx, candidate); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if err = r.store.Within(ctx, func(tx *workstore.Tx) error { return tx.RecordPersonalControlIsolation(scope, proofID, candidate, now) }); err != nil {
			return err
		}
	}
	return nil
}

func (r *teamRuntime) verifyPersonalControlIsolations(ctx context.Context, scope string) error {
	var holds []workstore.PersonalControlIsolation
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		holds, e = tx.ListPersonalControlIsolations(scope)
		return e
	}); err != nil {
		return err
	}
	for _, hold := range holds {
		if err := r.verifyPersonalControlProcess(ctx, hold); err != nil {
			return err
		}
	}
	return nil
}
