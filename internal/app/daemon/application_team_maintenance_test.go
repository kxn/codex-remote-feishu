package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type maintenanceFixtureProof struct{}

func (maintenanceFixtureProof) VerifyStop(context.Context, executionowner.Registration, string) error {
	return nil
}
func (maintenanceFixtureProof) VerifyMaintenanceSources(context.Context, executionowner.Maintenance, string) error {
	return nil
}
func (maintenanceFixtureProof) VerifyMaintenanceInstall(context.Context, executionowner.Maintenance, string) error {
	return nil
}
func TestTeamMaintenanceFrozenReplacementAndAuthenticatedReadiness(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, e := workstore.Open(ctx, filepath.Join(root, "work.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	old := work.WorkerSession{ID: "worker", ContextID: "context", ScopeID: "scope", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, InstanceID: "old-instance", CarrierIncarnation: "old-inc", InstanceGeneration: 1, ProcessGeneration: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, Revision: 1, NativeSessionID: "session", NativeNamespace: "native"}
	binding := func(w work.WorkerSession) agentproto.ExecutionBinding {
		return agentproto.ExecutionBinding{Version: 1, ScopeID: w.ScopeID, ContextID: w.ContextID, RoleID: w.RoleID, WorkerSessionID: w.ID, SessionGeneration: w.Generation, ControllerGeneration: w.ControllerGeneration, BotGeneration: 1}
	}
	carrier := func(w work.WorkerSession) workstore.CarrierRegistration {
		return workstore.CarrierRegistration{Incarnation: w.CarrierIncarnation, OriginInstanceID: w.InstanceID, Owner: binding(w), Backend: w.Backend, InstanceGeneration: w.InstanceGeneration, ProcessGeneration: w.ProcessGeneration}
	}
	saveLaunch := func(tx *workstore.Tx, w work.WorkerSession) error {
		v := workhost.LaunchRecord{SharedRelayCredential: true, NativeHistoryNamespace: "history", WorkerID: w.ID, ScopeID: w.ScopeID, Generation: w.Generation, Backend: w.Backend, Snapshot: agentproto.RuntimeLaunchSnapshot{Owner: binding(w), CarrierIncarnation: w.CarrierIncarnation}}
		raw, _ := json.Marshal(v)
		content := work.NewContent("body-"+w.InstanceID, w.ScopeID, "application/json", raw, time.Now())
		if e := tx.PutContent(content); e != nil {
			return e
		}
		_, _, e := tx.PutOperation(work.Operation{ID: workhost.LaunchOperationID(w.ID, w.Generation), ScopeID: w.ScopeID, ContextID: w.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "test"}, Kind: "worker.launch", TargetID: w.ID, TargetGeneration: w.Generation, IdempotencyKey: "launch-" + w.InstanceID, RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1}, 0)
		return e
	}
	e = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "Role", Revision: 1, DescriptionVersion: 1, SettingsRevision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: "role", PrincipalID: "human", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutContext(work.WorkContext{ID: "context", ScopeID: "scope", RoleID: "role", Workstream: "main", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutWorker(old, 0); e != nil {
			return e
		}
		if e := tx.RegisterCarrier(carrier(old)); e != nil {
			return e
		}
		return saveLaunch(tx, old)
	})
	if e != nil {
		t.Fatal(e)
	}
	registry, e := executionowner.Open(filepath.Join(root, "owner.db"), maintenanceFixtureProof{})
	if e != nil {
		t.Fatal(e)
	}
	defer registry.Close()
	oldReg := teamRegistration(carrier(old), old, "history")
	if e = registry.Register(ctx, oldReg); e != nil {
		t.Fatal(e)
	}
	if e = registry.BindSession(ctx, oldReg, "session"); e != nil {
		t.Fatal(e)
	}
	m, e := registry.BeginMaintenance(ctx, "codex", "upgrade", "2.0")
	if e != nil {
		t.Fatal(e)
	}
	if e = registry.Retire(ctx, oldReg, "stop-proof"); e != nil {
		t.Fatal(e)
	}
	if e = registry.MarkMaintenanceQuiescent(ctx, m, "sources", maintenanceFixtureProof{}); e != nil {
		t.Fatal(e)
	}
	if e = registry.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); e != nil {
		t.Fatal(e)
	}
	if e = registry.MarkMaintenanceInstalled(ctx, m, "installed", maintenanceFixtureProof{}); e != nil {
		t.Fatal(e)
	}
	next := old
	next.Generation = 2
	next.ControllerGeneration = 2
	next.InstanceGeneration = 2
	next.ProcessGeneration = 2
	next.InstanceID = "new-instance"
	next.CarrierIncarnation = "new-inc"
	next.State = work.WorkerRecovering
	next.Revision = 2
	e = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutWorker(next, 1); e != nil {
			return e
		}
		if e := saveLaunch(tx, next); e != nil {
			return e
		}
		return tx.PutNativeUpgrade(workstore.NativeUpgrade{ID: "upgrade", HostID: "host", TargetVersion: "2.0", ManagedVersion: "2.0", InstallationFingerprint: "fingerprint", Stage: "restarting", FrozenWorkers: []work.WorkerSession{old}, Revision: 1}, 0)
	})
	if e != nil {
		t.Fatal(e)
	}
	adapter := &teamExecutionOwnerAdapter{store: store, registry: registry, coordinator: workruntime.New(store, workruntime.Options{}), hostID: "host"}
	nextCarrier := carrier(next)
	if e = adapter.RegisterCarrierAfterUpgrade(ctx, "upgrade", nextCarrier); e != nil {
		t.Fatal(e)
	}
	if e = adapter.RegisterCarrierAfterUpgrade(ctx, "upgrade", nextCarrier); e != nil {
		t.Fatal("replay", e)
	}
	nextReg := teamRegistration(nextCarrier, next, "history")
	if e = registry.BindSession(ctx, nextReg, "session"); e != nil {
		t.Fatal(e)
	}
	if e = adapter.WithMaintenanceRecovery(ctx, m, oldReg, &nextReg, "team-ready:upgrade:new-inc", func() error { return nil }); !errors.Is(e, executionowner.ErrConflict) {
		t.Fatalf("unauthenticated ready accepted %v", e)
	}
	process, e := hostinventory.CaptureProcess(os.Getpid())
	if e != nil {
		t.Fatal(e)
	}
	e = store.Within(ctx, func(tx *workstore.Tx) error {
		w, e := tx.GetWorker(next.ID)
		if e != nil {
			return e
		}
		w.State = work.WorkerReady
		w.Revision++
		if e = tx.PutWorker(w, 2); e != nil {
			return e
		}
		if e = tx.SetCarrierConnected(next.CarrierIncarnation, true); e != nil {
			return e
		}
		return tx.PutCarrierProcess(next.CarrierIncarnation, next.InstanceID, agentproto.RuntimeProcessIdentity{HostID: "host", PID: process.PID, Platform: process.Platform, StartIdentity: process.StartIdentity})
	})
	if e != nil {
		t.Fatal(e)
	}
	adapter.connections = map[string]teamConnection{next.InstanceID: {id: 1, lease: &teamExecutionLease{adapter: adapter, registration: nextReg}}}
	called := false
	if e = adapter.WithMaintenanceRecovery(ctx, m, oldReg, &nextReg, "team-ready:upgrade:new-inc", func() error { called = true; return nil }); e != nil || !called {
		t.Fatalf("real bound readiness %v", e)
	}
	if e = registry.MarkMaintenanceRecovered(ctx, m, oldReg, &nextReg, "team-ready:upgrade:new-inc", adapter); e != nil {
		t.Fatalf("durable recovery receipt: %v", e)
	}
	forged := nextReg
	forged.Carrier.Namespace = "different"
	if e = adapter.WithMaintenanceRecovery(ctx, m, oldReg, &forged, "team-ready:upgrade:new-inc", func() error { t.Fatal("forged callback"); return nil }); e == nil {
		t.Fatal("forged namespace accepted")
	}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- adapter.WithMaintenanceRecovery(ctx, m, oldReg, &nextReg, "team-ready:upgrade:new-inc", func() error { close(entered); <-release; return nil })
	}()
	<-entered
	disconnected := make(chan struct{})
	go func() {
		adapter.connectionMu.Lock()
		delete(adapter.connections, next.InstanceID)
		adapter.connectionMu.Unlock()
		close(disconnected)
	}()
	select {
	case <-disconnected:
		t.Fatal("disconnect crossed readiness commit")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	<-disconnected
	if e = adapter.WithMaintenanceRecovery(ctx, m, oldReg, &nextReg, "team-ready:upgrade:new-inc", func() error { return nil }); e == nil {
		t.Fatal("disconnected replacement accepted")
	}
}
