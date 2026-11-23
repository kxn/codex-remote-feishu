package daemon

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/executionowner"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestTeamUnspawnedMaintenanceUsesDurableSupersededLaunch(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	store, err := workstore.Open(ctx, filepath.Join(root, "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	registry, err := executionowner.Open(filepath.Join(root, "owner.db"), maintenanceFixtureProof{})
	if err != nil {
		t.Fatal(err)
	}
	defer registry.Close()
	old := work.WorkerSession{ID: "worker", ScopeID: "scope", ContextID: "context", RoleID: "role", Backend: work.BackendCodex, State: work.WorkerPreparing, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "old", CarrierIncarnation: "old-inc", InstanceGeneration: 1, ProcessGeneration: 1, Revision: 1}
	old.NativeSessionID = "session"
	old.NativeNamespace = "history"
	owner := agentproto.ExecutionBinding{Version: 1, ScopeID: old.ScopeID, ContextID: old.ContextID, RoleID: old.RoleID, WorkerSessionID: old.ID, SessionGeneration: 1, ControllerGeneration: 1, BotGeneration: 1}
	launch := workhost.LaunchRecord{SharedRelayCredential: true, NativeHistoryNamespace: "history", WorkerID: old.ID, ScopeID: old.ScopeID, Generation: 1, Backend: old.Backend, Snapshot: agentproto.RuntimeLaunchSnapshot{Owner: owner, CarrierIncarnation: old.CarrierIncarnation}}
	raw, _ := json.Marshal(launch)
	next := old
	next.Generation = 2
	next.ControllerGeneration = 2
	next.InstanceGeneration = 2
	next.ProcessGeneration = 2
	next.InstanceID = "new"
	next.CarrierIncarnation = "new-inc"
	next.Revision = 2
	err = store.Within(ctx, func(tx *workstore.Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); e != nil {
			return e
		}
		if e := tx.PutRole(work.Role{ID: "role", Name: "Role", Description: "Role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); e != nil {
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
		content := work.NewContent("old-body", "scope", "application/json", raw, time.Now())
		if e := tx.PutContent(content); e != nil {
			return e
		}
		op := work.Operation{ID: workhost.LaunchOperationID(old.ID, 1), ScopeID: "scope", ContextID: "context", Actor: work.Actor{Kind: work.ActorSystem, ID: "host"}, Kind: "worker.launch", TargetID: old.ID, TargetGeneration: 1, IdempotencyKey: "old-launch", RequestRef: content.ID, RequestHash: content.SHA256, State: work.OperationPrepared, Revision: 1}
		if _, _, e := tx.PutOperation(op, 0); e != nil {
			return e
		}
		op.State = work.OperationTerminal
		op.LastError = "superseded before launch"
		op.Revision = 2
		if _, _, e := tx.PutOperation(op, 1); e != nil {
			return e
		}
		if e := tx.PutWorker(next, 1); e != nil {
			return e
		}
		return tx.PutNativeUpgrade(workstore.NativeUpgrade{ID: "upgrade", HostID: "host", TargetVersion: "v2", ManagedVersion: "v2", InstallationFingerprint: "fingerprint", Stage: "restarting", FrozenWorkers: []work.WorkerSession{old}, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	prior := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "prior", Incarnation: "prior-inc", Backend: "codex", Namespace: "history", Generation: 1}, Owner: executionowner.Owner{Application: executionowner.Team, ID: "worker", Epoch: 1}}
	if err = registry.Register(ctx, prior); err != nil {
		t.Fatal(err)
	}
	if err = registry.BindSession(ctx, prior, "session"); err != nil {
		t.Fatal(err)
	}
	if err = registry.Retire(ctx, prior, "stop-proof"); err != nil {
		t.Fatal(err)
	}
	m, err := registry.BeginMaintenance(ctx, "codex", "upgrade", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.MarkMaintenanceQuiescent(ctx, m, "source", maintenanceFixtureProof{}); err != nil {
		t.Fatal(err)
	}
	if err = registry.MarkMaintenanceInstalling(ctx, m.Backend, m.ID, m.Epoch); err != nil {
		t.Fatal(err)
	}
	if err = registry.MarkMaintenanceInstalled(ctx, m, "installed", maintenanceFixtureProof{}); err != nil {
		t.Fatal(err)
	}
	oldReg := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "old", Incarnation: "old-inc", Backend: "codex", Namespace: "history", Generation: 1}, Owner: executionowner.Owner{Application: executionowner.Team, ID: "worker", Epoch: 1}}
	nextReg := executionowner.Registration{Carrier: executionowner.Carrier{InstanceID: "new", Incarnation: "new-inc", Backend: "codex", Namespace: "history", Generation: 2}, Owner: executionowner.Owner{Application: executionowner.Team, ID: "worker", Epoch: 2}}
	verifier := teamUnspawnedVerifier{adapter: &teamExecutionOwnerAdapter{store: store, hostID: "host"}}
	if err = registry.RegisterMaintenanceUnspawned(ctx, m, oldReg, nextReg, "team-unspawned:upgrade:old-inc", verifier); err != nil {
		t.Fatal(err)
	}
	if err = registry.ReserveMaintenanceUnspawnedSession(ctx, m, oldReg, nextReg, "session", "team-unspawned:upgrade:old-inc", verifier); err != nil {
		t.Fatal(err)
	}
	if err = registry.BindSession(ctx, nextReg, "session"); err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Resolve(ctx, nextReg.Carrier, nextReg.Owner.Epoch); err != nil {
		t.Fatal(err)
	}
}
