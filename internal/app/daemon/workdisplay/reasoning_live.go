package workdisplay

import (
	"context"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Public summary deltas are buffered at native cadence and flushed at the old
// one-second UI cadence. They are never a durable native/business receipt.
func FlushPublicReasoning(ctx context.Context, store *workstore.Store, now time.Time, limit int) error {
	return store.Within(ctx, func(tx *workstore.Tx) error {
		displays, err := tx.ListDirtyPrivateReasoning(limit)
		if err != nil {
			return err
		}
		for _, display := range displays {
			if err := tx.AssertPersonalMigrationOpen(display.Binding.ScopeID); errors.Is(err, workstore.ErrPersonalMigrationFenced) {
				continue
			} else if err != nil {
				return err
			}
			if !display.Progress.LastEmittedAt.IsZero() && now.Before(display.Progress.LastEmittedAt.Add(time.Second)) {
				continue
			}
			carrier, err := tx.GetCarrier(display.Incarnation)
			if err != nil {
				return err
			}
			worker, err := tx.GetWorker(display.Binding.WorkerSessionID)
			if err != nil {
				return err
			}
			turn, err := tx.GetNativeTurn(display.Incarnation, display.SessionID, display.TurnID)
			if err != nil {
				return err
			}
			bot, err := tx.GetRoleBotBinding(display.Binding.RoleID)
			if err != nil {
				return err
			}
			verbosity, err := executionVerbosity(tx, display.Binding.ContextID)
			if err != nil {
				return err
			}
			if carrier.Matches(worker) && turn.Binding == display.Binding && turn.TerminalStatus == "" && bot == display.Bot && (verbosity == state.SurfaceVerbosityVerbose || verbosity == state.SurfaceVerbosityChatty) {
				display.Progress.Verbosity = verbosity
				p := projection{tx: tx, display: &display, backend: carrier.Backend, now: now, event: agentproto.Event{Kind: agentproto.EventItemDelta, ThreadID: display.SessionID, TurnID: display.TurnID}}
				if err = p.publishProgress(); err != nil {
					return err
				}
			} else {
				display.Progress.Reasoning.LastEmittedRevision = display.Progress.Reasoning.Revision
			}
			before := display.Revision
			display.Revision++
			if err = tx.PutPrivateExecutionDisplay(display, before); err != nil {
				return err
			}
		}
		return nil
	})
}
