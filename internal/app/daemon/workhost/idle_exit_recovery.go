package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"time"
)

// Recover exact exited idle carriers from their last quiescence proof, or
// prove that a catalog-only wrapper never started execution in either journal.
func (h *Host) reconcileExitedIdleCarrier(ctx context.Context, expected work.WorkerSession) (bool, error) {
	var record LaunchRecord
	var process agentproto.RuntimeProcessIdentity
	unused := false
	check := func(tx *workstore.Tx) error {
		current, err := tx.GetWorker(expected.ID)
		if err != nil {
			return err
		}
		if current.Revision != expected.Revision || current.Controller != work.ControllerAutomatic || current.State != work.WorkerRecovering {
			return workstore.ErrConflict
		}
		gate, err := tx.GetRuntimeGate(current.ID)
		if err != nil {
			return err
		}
		if gate.Reason != "carrier_disconnected_reconcile" || gate.AttemptID != "" || gate.RecoveryReason != "" {
			return workstore.ErrConflict
		}
		carrier, err := tx.GetCarrier(current.CarrierIncarnation)
		if err != nil {
			return err
		}
		connected, err := tx.CarrierConnected(carrier.Incarnation)
		if err != nil {
			return err
		}
		occupied, err := tx.WorkerHasOccupancy(current.ID)
		if err != nil {
			return err
		}
		unresolved, err := tx.CountUnresolvedNativeEvents(carrier.Incarnation)
		if err != nil {
			return err
		}
		if connected || occupied || unresolved != 0 || !carrier.Matches(current) {
			return workstore.ErrConflict
		}
		return nil
	}
	err := h.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := check(tx); err != nil {
			return err
		}
		e := tx.AssertUnusedCarrier(expected)
		unused = e == nil
		if e != nil && !errors.Is(e, workstore.ErrConflict) {
			return e
		}
		var err error
		record, err = h.readLaunch(tx, expected)
		if err != nil {
			return err
		}
		process, err = tx.GetCarrierProcess(expected.CarrierIncarnation)
		return err
	})
	// Other recovery states still follow their native evidence-based paths.
	if errors.Is(err, workstore.ErrConflict) || errors.Is(err, workstore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.Snapshot.HostID != h.config.HostID || process.HostID != h.config.HostID {
		return false, nil
	}
	wait, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err = hostinventory.WaitProcessExit(wait, hostinventory.ProcessIdentity{Platform: process.Platform, PID: process.PID, StartIdentity: process.StartIdentity}); err != nil {
		return false, nil
	}
	var raw []byte
	if unused {
		if err = runtimejournal.VerifyUnusedCarrier(ctx, record.Snapshot, h.config.Paths.StateDir); err != nil {
			return false, err
		}
		raw, err = json.Marshal(struct {
			Process agentproto.RuntimeProcessIdentity `json:"process"`
			Reason  string                            `json:"reason"`
		}{process, "exact exited carrier never admitted native execution"})
	} else {
		observation, e := workruntime.VerifyRetiredIdleCarrier(ctx, h.store, h.config.Paths.StateDir, h.config.HostID, expected)
		if errors.Is(e, workstore.ErrNotFound) || errors.Is(e, workstore.ErrConflict) {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		raw, err = json.Marshal(observation)
	}
	if err != nil {
		return false, err
	}
	err = h.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := check(tx); err != nil {
			return err
		}
		if unused {
			if err := tx.AssertUnusedCarrier(expected); err != nil {
				return err
			}
		} else if _, err := tx.CarrierQuiescenceAttempt(expected); err != nil {
			return err
		}
		now := h.config.Now()
		body := work.NewContent(idFor("exited_idle_carrier", expected.CarrierIncarnation), expected.ScopeID, "application/json", raw, now)
		if err := tx.PutContent(body); err != nil {
			return err
		}
		id := rebuildID(expected)
		retryAt := now
		op := work.Operation{ID: id, ScopeID: expected.ScopeID, ContextID: expected.ContextID, Kind: "worker.rebuild", TargetID: expected.ID, TargetGeneration: expected.Generation, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, IdempotencyKey: id, State: work.OperationConfirmed, RequestRef: body.ID, RequestHash: body.SHA256, ExternalReceipt: body.ID, NextAttemptAt: &retryAt, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, _, err := tx.PutOperation(op, 0); err != nil {
			return err
		}
		return tx.PutRuntimeGate(workstore.RuntimeGate{WorkerID: expected.ID, ScopeID: expected.ScopeID, Reason: "snapshot_rebuild_required", CreatedAt: now})
	})
	if errors.Is(err, workstore.ErrConflict) {
		return false, nil
	}
	return err == nil, err
}
