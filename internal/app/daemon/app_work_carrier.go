package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// stopWorkCarrier is called only after the host has separately proven native,
// continuation and background quiescence. A transport disconnect is not exit.
func (a *App) stopWorkCarrier(ctx context.Context, worker work.WorkerSession) error {
	r := a.teamRuntime
	if r == nil {
		return fmt.Errorf("team runtime is not configured")
	}
	var identity agentproto.RuntimeProcessIdentity
	var command agentproto.Command
	var operation work.Operation
	send := false
	err := r.store.Within(ctx, func(tx *workstore.Tx) error {
		fresh, e := tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		if fresh.CarrierIncarnation != worker.CarrierIncarnation || fresh.Generation != worker.Generation || fresh.ControllerGeneration != worker.ControllerGeneration {
			return workstore.ErrConflict
		}
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if !carrier.Matches(fresh) {
			gate, e := tx.GetRuntimeGate(fresh.ID)
			if e != nil {
				return e
			}
			if !carrier.MatchesPhysical(fresh) || fresh.Controller != work.ControllerAutomatic || fresh.State != work.WorkerRecovering || gate.Reason != "snapshot_rebuild_required" || gate.RecoveryReason != "" {
				return workstore.ErrConflict
			}
		}
		identity, e = tx.GetCarrierProcess(carrier.Incarnation)
		if e != nil {
			return e
		}
		location, e := tx.GetLocation(fresh.LocationID)
		if e != nil {
			return e
		}
		if r.host == nil || r.host.LocalHostID() == "" || location.HostID != r.host.LocalHostID() || identity.HostID != r.host.LocalHostID() {
			return fmt.Errorf("carrier exit requires proof from its actual host")
		}
		id := teamID("carrier_exit", carrier.Incarnation)
		if old, e := tx.GetOperation(id); e == nil {
			operation = old
			body, e := tx.GetContent(old.ScopeID, old.RequestRef)
			if e != nil {
				return e
			}
			return json.Unmarshal(body.Data, &command)
		} else if !errors.Is(e, workstore.ErrNotFound) {
			return e
		}
		command = agentproto.Command{CommandID: id, Kind: agentproto.CommandProcessExit, Execution: &carrier.Owner, IssuedAt: time.Now().UTC(), Target: agentproto.Target{ThreadID: worker.NativeSessionID}}
		body, e := json.Marshal(command)
		if e != nil {
			return e
		}
		content := work.NewContent(id+":body", worker.ScopeID, "application/json", body, time.Now().UTC())
		if e = tx.PutContent(content); e != nil {
			return e
		}
		operation = work.Operation{ID: id, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "work-host"}, Kind: string(command.Kind), TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: id, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
		_, _, e = tx.PutOperation(operation, 0)
		send = e == nil
		return e
	})
	if err != nil {
		return err
	}
	if operation.State == work.OperationConfirmed {
		return nil
	}
	var sendErr error
	if send {
		sendErr = r.driver.SendCommand(ctx, worker.InstanceID, command)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	exitErr := hostinventory.WaitProcessExit(waitCtx, hostinventory.ProcessIdentity{Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity})
	persistErr := r.store.Within(context.WithoutCancel(ctx), func(tx *workstore.Tx) error {
		op, e := tx.GetOperation(operation.ID)
		if e != nil {
			return e
		}
		if op.State == work.OperationConfirmed {
			return nil
		}
		previous := op.Revision
		op.Revision++
		op.UpdatedAt = time.Now().UTC()
		if exitErr == nil {
			evidence, _ := json.Marshal(identity)
			content := work.NewContent(op.ID+":exit-proof", op.ScopeID, "application/json", evidence, time.Now().UTC())
			if e = tx.PutContent(content); e != nil {
				return e
			}
			op.State = work.OperationConfirmed
			op.ExternalReceipt = content.ID
		} else {
			op.State = work.OperationUnknown
			op.LastError = errors.Join(sendErr, exitErr).Error()
		}
		_, _, e = tx.PutOperation(op, previous)
		return e
	})
	// The exact OS identity can prove exit even if the transport response was
	// lost. Conversely a successful send alone never authorizes replacement.
	return errors.Join(exitErr, persistErr)
}
