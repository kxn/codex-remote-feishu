package daemon

import (
	"context"
	"errors"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// isolateExitedPersonalWorkers separates an exact stopped local process scope
// from its potentially unresolved remote execution. It preserves every old
// attempt and never claims the remote task completed.
func (r *teamRuntime) isolateExitedPersonalWorkers(ctx context.Context, scope, proofID string, now time.Time) error {
	if r.app == nil || r.codexUpgradeHostID == "" {
		return nil
	}
	var workers []work.WorkerSession
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error { var e error; workers, e = tx.ListWorkers(scope); return e }); err != nil {
		return err
	}
	for _, worker := range workers {
		if worker.State == work.WorkerRetired || worker.CarrierIncarnation == "" {
			continue
		}
		var snapshot agentproto.RuntimeLaunchSnapshot
		var process agentproto.RuntimeProcessIdentity
		var receipt agentproto.Event
		err := r.store.Within(ctx, func(tx *workstore.Tx) error {
			var e error
			snapshot, e = tx.GetNativeLaunchSnapshot(worker.ID, worker.Generation)
			if e != nil {
				return e
			}
			process, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
			if e != nil {
				return e
			}
			if worker.NativeSessionID == "" {
				return nil
			}
			receipt, e = tx.LegacyCarrierSessionReceipt(scope, worker.ID, worker.CarrierIncarnation, worker.NativeSessionID)
			if errors.Is(e, workstore.ErrNotFound) {
				return nil
			}
			return e
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		if snapshot.HostID != r.codexUpgradeHostID || process.HostID != r.codexUpgradeHostID || snapshot.CarrierIncarnation != worker.CarrierIncarnation {
			continue
		}
		probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		err = hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity})
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			continue
		}
		evidence, err := runtimejournal.VerifyLegacyRetiredInventory(ctx, snapshot, r.app.headlessRuntime.Paths.StateDir)
		if err != nil && receipt.Journal != nil {
			evidence, err = runtimejournal.VerifyLegacyRetiredInventoryWithReceipt(ctx, snapshot, r.app.headlessRuntime.Paths.StateDir, worker.NativeSessionID, receipt)
		}
		if err != nil || !evidence.ProcessScopesStopped {
			continue
		}
		hold := workstore.PersonalExecutionIsolation{Worker: worker, Process: process, LaunchHash: evidence.LaunchHash, JournalLaunchHash: evidence.JournalLaunchHash, SourceReceiptHash: evidence.SourceReceiptHash, InventoryHash: evidence.InventoryHash, NativeCommandsIdle: evidence.NativeCommandsIdle, InputsIdle: evidence.InputsIdle, ToolsIdle: evidence.ToolsIdle, GoalsIdle: evidence.GoalsIdle, TasksIdle: evidence.TasksIdle, TurnsTerminal: evidence.TurnsTerminal, UncertaintiesEmpty: evidence.UncertaintiesEmpty, ProcessScopesStopped: evidence.ProcessScopesStopped}
		if err = r.store.Within(ctx, func(tx *workstore.Tx) error { return tx.HoldPersonalExecutionIsolation(scope, proofID, hold, now) }); err != nil {
			return err
		}
	}
	return nil
}

func (r *teamRuntime) verifyPersonalExecutionIsolation(ctx context.Context, v workstore.PersonalExecutionIsolation) error {
	if r.app == nil || r.codexUpgradeHostID == "" || v.Process.HostID != r.codexUpgradeHostID {
		return workstore.ErrConflict
	}
	var snapshot agentproto.RuntimeLaunchSnapshot
	var process agentproto.RuntimeProcessIdentity
	var receipt agentproto.Event
	if err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		current, e := tx.GetPersonalExecutionIsolation(v.Worker.ScopeID, v.Worker.ID)
		if e != nil {
			return e
		}
		if current != v {
			return workstore.ErrConflict
		}
		snapshot, e = tx.GetNativeLaunchSnapshot(v.Worker.ID, v.Worker.Generation)
		if e != nil {
			return e
		}
		process, e = tx.GetCarrierProcess(v.Worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if v.Worker.NativeSessionID == "" {
			return nil
		}
		receipt, e = tx.LegacyCarrierSessionReceipt(v.Worker.ScopeID, v.Worker.ID, v.Worker.CarrierIncarnation, v.Worker.NativeSessionID)
		if errors.Is(e, workstore.ErrNotFound) {
			return nil
		}
		return e
	}); err != nil {
		return err
	}
	if process != v.Process || snapshot.HostID != r.codexUpgradeHostID {
		return workstore.ErrConflict
	}
	probe, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err := hostinventory.WaitProcessExit(probe, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity})
	cancel()
	if err != nil {
		return err
	}
	evidence, err := runtimejournal.VerifyLegacyRetiredInventory(ctx, snapshot, r.app.headlessRuntime.Paths.StateDir)
	if err != nil && receipt.Journal != nil {
		evidence, err = runtimejournal.VerifyLegacyRetiredInventoryWithReceipt(ctx, snapshot, r.app.headlessRuntime.Paths.StateDir, v.Worker.NativeSessionID, receipt)
	}
	if err != nil {
		return err
	}
	if evidence.LaunchHash != v.LaunchHash || evidence.JournalLaunchHash != v.JournalLaunchHash || evidence.SourceReceiptHash != v.SourceReceiptHash || evidence.InventoryHash != v.InventoryHash || evidence.NativeCommandsIdle != v.NativeCommandsIdle || evidence.InputsIdle != v.InputsIdle || evidence.ToolsIdle != v.ToolsIdle || evidence.GoalsIdle != v.GoalsIdle || evidence.TasksIdle != v.TasksIdle || evidence.TurnsTerminal != v.TurnsTerminal || evidence.UncertaintiesEmpty != v.UncertaintiesEmpty || !evidence.ProcessScopesStopped {
		return workstore.ErrConflict
	}
	return nil
}
