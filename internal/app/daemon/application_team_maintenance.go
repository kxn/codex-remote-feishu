package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

// RegisterCarrierAfterUpgrade derives both registrations from the frozen team
// upgrade and its committed launch records. The caller cannot choose an owner
// or an old session; the registry independently checks the maintenance epoch.
func (a *teamExecutionOwnerAdapter) RegisterCarrierAfterUpgrade(ctx context.Context, upgradeID string, carrier workstore.CarrierRegistration) error {
	next, worker, err := a.frozenTeamOwner(ctx, carrier)
	if err != nil {
		return err
	}
	m, err := a.registry.MaintenanceStatus(ctx, string(carrier.Backend))
	if err != nil {
		return err
	}
	if m.ID != upgradeID || m.Phase != "restoring" {
		return executionowner.ErrConflict
	}
	var old executionowner.Registration
	unspawned := false
	err = a.store.Within(ctx, func(tx *workstore.Tx) error {
		upgrade, e := tx.GetNativeUpgrade(upgradeID)
		if e != nil {
			return e
		}
		if upgrade.HostID != a.hostID || upgrade.Stage != "restarting" || upgrade.TargetVersion != m.TargetVersion || upgrade.ManagedVersion != m.TargetVersion {
			return workstore.ErrConflict
		}
		var frozen *work.WorkerSession
		for i := range upgrade.FrozenWorkers {
			if upgrade.FrozenWorkers[i].ID == worker.ID {
				if frozen != nil {
					return workstore.ErrConflict
				}
				frozen = &upgrade.FrozenWorkers[i]
			}
		}
		if frozen == nil || frozen.Backend != work.BackendCodex || frozen.Generation+1 != worker.Generation || frozen.ControllerGeneration+1 != worker.ControllerGeneration || frozen.InstanceGeneration+1 != worker.InstanceGeneration || frozen.ScopeID != worker.ScopeID || frozen.ContextID != worker.ContextID || frozen.RoleID != worker.RoleID {
			return workstore.ErrConflict
		}
		op, e := tx.GetOperation(workhost.LaunchOperationID(frozen.ID, frozen.Generation))
		if e != nil {
			return e
		}
		if op.Kind != "worker.launch" || op.TargetID != frozen.ID || op.TargetGeneration != frozen.Generation {
			return workstore.ErrConflict
		}
		body, e := tx.GetContent(frozen.ScopeID, op.RequestRef)
		if e != nil {
			return e
		}
		var launch workhost.LaunchRecord
		if json.Unmarshal(body.Data, &launch) != nil || !launch.SharedRelayCredential || launch.NativeHistoryNamespace == "" || launch.WorkerID != frozen.ID || launch.Generation != frozen.Generation || launch.ScopeID != frozen.ScopeID || launch.Backend != frozen.Backend || launch.Snapshot.CarrierIncarnation != frozen.CarrierIncarnation {
			return workstore.ErrConflict
		}
		previous, e := tx.GetCarrier(frozen.CarrierIncarnation)
		if errors.Is(e, workstore.ErrNotFound) {
			if op.State != work.OperationTerminal || op.LastError != "superseded before launch" || worker.NativeSessionID != frozen.NativeSessionID {
				return workstore.ErrConflict
			}
			previous = workstore.CarrierRegistration{Incarnation: frozen.CarrierIncarnation, OriginInstanceID: frozen.InstanceID, Owner: launch.Snapshot.Owner, Backend: frozen.Backend, InstanceGeneration: frozen.InstanceGeneration, ProcessGeneration: frozen.ProcessGeneration, LocationGeneration: frozen.LocationGeneration}
			unspawned = true
		} else if e != nil {
			return e
		}
		if !previous.Matches(*frozen) || !launch.Snapshot.Owner.SameOwner(previous.Owner) {
			return workstore.ErrConflict
		}
		old = teamRegistration(previous, *frozen, launch.NativeHistoryNamespace)
		if next.Carrier.Namespace != old.Carrier.Namespace || (worker.NativeSessionID != "" && worker.NativeSessionID != frozen.NativeSessionID) {
			return workstore.ErrConflict
		}
		return nil
	})
	if err != nil {
		return err
	}
	if old.Owner.Application != executionowner.Team || old.Owner.ID != next.Owner.ID {
		return fmt.Errorf("maintenance team owner mismatch")
	}
	if unspawned {
		err = a.registry.RegisterMaintenanceUnspawned(ctx, m, old, next, "team-unspawned:"+upgradeID+":"+old.Carrier.Incarnation, teamUnspawnedVerifier{adapter: a})
	} else {
		err = a.registry.RegisterMaintenanceRecovery(ctx, m, old, next)
	}
	if err != nil {
		return err
	}
	if worker.NativeSessionID != "" {
		if unspawned {
			err = a.registry.ReserveMaintenanceUnspawnedSession(ctx, m, old, next, worker.NativeSessionID, "team-unspawned:"+upgradeID+":"+old.Carrier.Incarnation, teamUnspawnedVerifier{adapter: a})
		} else {
			err = a.registry.ReserveMaintenanceSession(ctx, m, old, next, worker.NativeSessionID)
		}
		if err != nil {
			return err
		}
	}
	return a.registry.WithLease(ctx, next, func() error { return a.coordinator.RegisterCarrier(ctx, carrier) })
}

type teamUnspawnedVerifier struct{ adapter *teamExecutionOwnerAdapter }

func (v teamUnspawnedVerifier) WithMaintenanceUnspawned(ctx context.Context, m executionowner.Maintenance, old, next executionowner.Registration, evidence string, commit func() error) error {
	if evidence != "team-unspawned:"+m.ID+":"+old.Carrier.Incarnation || commit == nil {
		return executionowner.ErrConflict
	}
	return v.adapter.store.Within(ctx, func(tx *workstore.Tx) error {
		upgrade, err := tx.GetNativeUpgrade(m.ID)
		if err != nil {
			return err
		}
		if upgrade.Stage != "restarting" || upgrade.TargetVersion != m.TargetVersion || upgrade.ManagedVersion != m.TargetVersion || upgrade.HostID != v.adapter.hostID {
			return workstore.ErrConflict
		}
		var frozen *work.WorkerSession
		for i := range upgrade.FrozenWorkers {
			if upgrade.FrozenWorkers[i].ID == old.Owner.ID {
				if frozen != nil {
					return workstore.ErrConflict
				}
				frozen = &upgrade.FrozenWorkers[i]
			}
		}
		if frozen == nil || frozen.CarrierIncarnation != old.Carrier.Incarnation || frozen.InstanceID != old.Carrier.InstanceID || frozen.InstanceGeneration != old.Carrier.Generation || frozen.ControllerGeneration != old.Owner.Epoch {
			return workstore.ErrConflict
		}
		op, err := tx.GetOperation(workhost.LaunchOperationID(frozen.ID, frozen.Generation))
		if err != nil {
			return err
		}
		if op.State != work.OperationTerminal || op.LastError != "superseded before launch" || op.ScopeID != frozen.ScopeID || op.TargetID != frozen.ID || op.TargetGeneration != frozen.Generation {
			return workstore.ErrConflict
		}
		body, err := tx.GetContent(frozen.ScopeID, op.RequestRef)
		if err != nil {
			return err
		}
		var launch workhost.LaunchRecord
		if json.Unmarshal(body.Data, &launch) != nil || launch.Snapshot.CarrierIncarnation != old.Carrier.Incarnation || launch.NativeHistoryNamespace != old.Carrier.Namespace || launch.WorkerID != frozen.ID || launch.Generation != frozen.Generation || launch.Backend != frozen.Backend || launch.ScopeID != frozen.ScopeID || launch.Snapshot.Owner.WorkerSessionID != frozen.ID || launch.Snapshot.Owner.ScopeID != frozen.ScopeID || launch.Snapshot.Owner.ContextID != frozen.ContextID || launch.Snapshot.Owner.SessionGeneration != frozen.Generation || launch.Snapshot.Owner.ControllerGeneration != frozen.ControllerGeneration {
			return workstore.ErrConflict
		}
		worker, err := tx.GetWorker(frozen.ID)
		if err != nil {
			return err
		}
		if worker.InstanceID != next.Carrier.InstanceID || worker.CarrierIncarnation != next.Carrier.Incarnation || worker.InstanceGeneration != next.Carrier.Generation || worker.ControllerGeneration != next.Owner.Epoch || worker.Generation != frozen.Generation+1 || worker.NativeSessionID != frozen.NativeSessionID {
			return workstore.ErrConflict
		}
		return commit()
	})
}

// VerifyMaintenanceRecovery accepts only the currently authenticated exact
// replacement and its durable worker/launch/process identity. A disconnected
// or stale carrier cannot stand in for the replacement's real hello.
func (a *teamExecutionOwnerAdapter) WithMaintenanceRecovery(ctx context.Context, m executionowner.Maintenance, old executionowner.Registration, next *executionowner.Registration, evidence string, commit func() error) error {
	if commit == nil || next == nil || evidence != "team-ready:"+m.ID+":"+next.Carrier.Incarnation || old.Owner.Application != executionowner.Team || next.Owner.Application != executionowner.Team || old.Owner.ID != next.Owner.ID || next.Owner.Epoch != old.Owner.Epoch+1 || next.Carrier.Generation != old.Carrier.Generation+1 || next.Carrier.Namespace != old.Carrier.Namespace || old.Carrier.Backend != m.Backend || next.Carrier.Backend != m.Backend {
		return executionowner.ErrConflict
	}
	a.connectionMu.Lock()
	defer a.connectionMu.Unlock()
	conn, ok := a.connections[next.Carrier.InstanceID]
	if !ok || conn.id == 0 || conn.lease == nil || conn.lease.registration != *next {
		return executionowner.ErrConflict
	}
	var process agentproto.RuntimeProcessIdentity
	err := a.store.Within(ctx, func(tx *workstore.Tx) error {
		upgrade, e := tx.GetNativeUpgrade(m.ID)
		if e != nil {
			return e
		}
		if upgrade.HostID != a.hostID || upgrade.Stage != "restarting" || upgrade.TargetVersion != m.TargetVersion || upgrade.ManagedVersion != m.TargetVersion {
			return workstore.ErrConflict
		}
		var frozen *work.WorkerSession
		for i := range upgrade.FrozenWorkers {
			if upgrade.FrozenWorkers[i].ID == old.Owner.ID {
				if frozen != nil {
					return workstore.ErrConflict
				}
				frozen = &upgrade.FrozenWorkers[i]
			}
		}
		if frozen == nil || frozen.CarrierIncarnation != old.Carrier.Incarnation || frozen.InstanceID != old.Carrier.InstanceID || frozen.InstanceGeneration != old.Carrier.Generation || frozen.ControllerGeneration != old.Owner.Epoch {
			return workstore.ErrConflict
		}
		worker, e := tx.GetWorker(old.Owner.ID)
		if e != nil {
			return e
		}
		carrier, e := tx.GetCarrier(next.Carrier.Incarnation)
		if e != nil {
			return e
		}
		if worker.State != work.WorkerReady || worker.Generation != frozen.Generation+1 || !carrier.Matches(worker) || teamRegistration(carrier, worker, next.Carrier.Namespace) != *next {
			return workstore.ErrConflict
		}
		connected, e := tx.CarrierConnected(carrier.Incarnation)
		if e != nil {
			return e
		}
		if !connected {
			return workstore.ErrConflict
		}
		op, e := tx.GetOperation(workhost.LaunchOperationID(worker.ID, worker.Generation))
		if e != nil {
			return e
		}
		body, e := tx.GetContent(worker.ScopeID, op.RequestRef)
		if e != nil {
			return e
		}
		var launch workhost.LaunchRecord
		if json.Unmarshal(body.Data, &launch) != nil || !launch.SharedRelayCredential || launch.NativeHistoryNamespace != next.Carrier.Namespace || launch.Generation != worker.Generation || launch.Snapshot.CarrierIncarnation != next.Carrier.Incarnation || !launch.Snapshot.Owner.SameOwner(carrier.Owner) {
			return workstore.ErrConflict
		}
		process, e = tx.GetCarrierProcess(carrier.Incarnation)
		return e
	})
	if err != nil {
		return err
	}
	if process.HostID != "" && process.HostID != a.hostID {
		return executionowner.ErrConflict
	}
	actual, err := hostinventory.CaptureProcess(process.PID)
	if err != nil {
		return err
	}
	if actual.Platform != process.Platform || actual.StartIdentity != process.StartIdentity {
		return executionowner.ErrConflict
	}
	return commit()
}
