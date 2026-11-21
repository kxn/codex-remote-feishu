package workhost

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type failingRegistry struct {
	fail bool
	testRegistry
}

func (r *failingRegistry) RegisterCarrier(ctx context.Context, c workstore.CarrierRegistration) error {
	if r.fail {
		return errors.New("crash before launch")
	}
	return r.testRegistry.RegisterCarrier(ctx, c)
}

type rebuildDriver struct {
	inspections int
	stopped     bool
}

func (d *rebuildDriver) SendCommand(context.Context, string, agentproto.Command) error { return nil }
func (d *rebuildDriver) Inspect(_ context.Context, r workruntime.InspectionRequest) (workruntime.NativeObservation, error) {
	d.inspections++
	return workruntime.NativeObservation{CarrierIncarnation: r.Carrier.Incarnation, InstanceID: r.Carrier.OriginInstanceID, InstanceGeneration: r.Carrier.InstanceGeneration, ProcessGeneration: r.Carrier.ProcessGeneration, LocationGeneration: r.LocationGeneration, SessionID: r.SessionID, TurnID: r.TurnID, NativeStopped: d.stopped, ContinuationStopped: d.stopped, BackgroundWorkStopped: d.stopped, NativeEvidence: json.RawMessage(`{"native":"trusted-test-driver"}`), ContinuationEvidence: json.RawMessage(`{"continuation":"trusted-test-driver"}`), BackgroundEvidence: json.RawMessage(`{"process_inventory":"trusted-test-driver"}`), ObservedAt: time.Now()}, nil
}
func hostWorker(t *testing.T, s *workstore.Store) work.WorkerSession {
	t.Helper()
	var w work.WorkerSession
	if err := s.Within(context.Background(), func(tx *workstore.Tx) error {
		ws, e := tx.ListWorkers("scope")
		if e != nil {
			return e
		}
		if len(ws) != 1 {
			t.Fatalf("workers=%d", len(ws))
		}
		w = ws[0]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return w
}
func readyAndChangeRole(t *testing.T, s *workstore.Store) {
	t.Helper()
	if err := s.Within(context.Background(), func(tx *workstore.Tx) error {
		ws, e := tx.ListWorkers("scope")
		if e != nil {
			return e
		}
		w := ws[0]
		before := w.Revision
		w.State = work.WorkerReady
		w.Revision++
		if e = tx.PutWorker(w, before); e != nil {
			return e
		}
		role, e := tx.GetRole("role")
		if e != nil {
			return e
		}
		before = role.Revision
		role.Description = "new scoped role"
		role.DescriptionVersion++
		role.Revision++
		return tx.PutRole(role, before)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPreparedLaunchResumesOnlyPersistedUnsentIntent(t *testing.T) {
	calls := 0
	h, s := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 1234, nil })
	registry := &failingRegistry{fail: true, testRegistry: testRegistry{store: s}}
	h.config.Registry = registry
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if h.Prepare(context.Background(), need) == nil || calls != 0 {
		t.Fatal("missing prepared-only crash boundary")
	}
	before := hostWorker(t, s)
	registry.fail = false
	// A new Host has no in-memory preparation state.
	recovered, err := New(s, h.service, h.signer, h.config)
	if err != nil {
		t.Fatal(err)
	}
	if err = recovered.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	after := hostWorker(t, s)
	if calls != 1 || after.Generation != before.Generation || after.CarrierIncarnation != before.CarrierIncarnation {
		t.Fatal("prepared recovery changed immutable launch")
	}
}

func TestSafeRebuildRequiresAllProofAndCreatesFreshNativeHistory(t *testing.T) {
	calls := 0
	h, s := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 1234, nil })
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	readyAndChangeRole(t, s)
	before := hostWorker(t, s)
	driver := &rebuildDriver{}
	h.config.Runtime = driver
	stops := 0
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error { stops++; return nil }
	if h.Prepare(context.Background(), need) == nil || stops != 0 || calls != 1 {
		t.Fatal("incomplete proof caused replacement")
	}
	driver.stopped = true
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	after := hostWorker(t, s)
	if calls != 2 || stops != 1 || after.Generation != before.Generation+1 || after.InstanceGeneration != before.InstanceGeneration+1 || after.ProcessGeneration != before.ProcessGeneration+1 || after.ControllerGeneration != before.ControllerGeneration+1 || after.CarrierIncarnation == before.CarrierIncarnation || after.InstanceID == before.InstanceID || after.NativeSessionID != "" || after.NativeNamespace != "" {
		t.Fatalf("unsafe rebuilt worker: %+v", after)
	}
}

func TestUnknownShutdownRetriesOnlyExactExitObservation(t *testing.T) {
	calls := 0
	h, s := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 1234, nil })
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	readyAndChangeRole(t, s)
	before := hostWorker(t, s)
	driver := &rebuildDriver{stopped: true}
	h.config.Runtime = driver
	fail := true
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error {
		if fail {
			return errors.New("exit not yet observed")
		}
		return nil
	}
	if h.Prepare(context.Background(), need) == nil {
		t.Fatal("unknown shutdown reported rebuilt")
	}
	if after := hostWorker(t, s); calls != 1 || after.Generation != before.Generation {
		t.Fatal("unknown shutdown changed physical binding")
	}
	fail = false
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	if calls != 2 || driver.inspections != 1 {
		t.Fatal("shutdown recovery queried stopped native or duplicated launch")
	}
}

func TestUnknownLaunchReconcilesExactPersistedHelloWithoutRespawn(t *testing.T) {
	calls := 0
	h, s := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) {
		calls++
		return 0, errors.New("launcher response lost")
	})
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if h.Prepare(context.Background(), need) == nil {
		t.Fatal("expected unknown launch")
	}
	worker := hostWorker(t, s)
	identity, err := hostinventory.CaptureProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	err = s.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutCarrierProcess(worker.CarrierIncarnation, worker.InstanceID, agentproto.RuntimeProcessIdentity{Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("observed carrier was relaunched")
	}
}
func TestSafeRebuildUsesNewExecutionDirectoryAndRejectsBusyChanges(t *testing.T) {
	calls := 0
	h, s := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 1234, nil })
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	readyAndChangeRole(t, s)
	worker := hostWorker(t, s)
	driver := &rebuildDriver{stopped: true}
	h.config.Runtime = driver
	h.config.StopCarrier = func(context.Context, work.WorkerSession) error { return nil }
	cwd := t.TempDir()
	h.config.Settings = testSettings{location: work.ExecutionLocation{ID: "prepared-worktree", HostID: "local", Cwd: cwd, Generation: 1, Revision: 1}}
	if err := s.Within(context.Background(), func(tx *workstore.Tx) error {
		w, e := tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		w.State = work.WorkerBusy
		w.Revision++
		return tx.PutWorker(w, w.Revision-1)
	}); err != nil {
		t.Fatal(err)
	}
	if h.Prepare(context.Background(), need) == nil || driver.inspections != 0 || calls != 1 {
		t.Fatal("busy worker was inspected or changed for rebuild")
	}
	if err := s.Within(context.Background(), func(tx *workstore.Tx) error {
		w, e := tx.GetWorker(worker.ID)
		if e != nil {
			return e
		}
		w.State = work.WorkerRecovering
		w.Revision++
		return tx.PutWorker(w, w.Revision-1)
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Prepare(context.Background(), need); err != nil {
		t.Fatal(err)
	}
	worker = hostWorker(t, s)
	if worker.LocationID != "prepared-worktree" || calls != 2 {
		t.Fatal("verified new location was not adopted")
	}
	raw, err := os.ReadFile(filepath.Join(h.config.Paths.StateDir, "workers", worker.ID, "2", "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	var snapshot agentproto.RuntimeLaunchSnapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.CWD != cwd || snapshot.Owner.SessionGeneration != 2 {
		t.Fatal("new native process retained old cwd or history generation")
	}
}

func TestPreparedRecoveryReplacesUnsentIntentWhenRoleSettingsChangeAtStart(t *testing.T) {
	ctx := t.Context()
	calls := 0
	host, store := hostFixture(t, func(relayruntime.HeadlessLaunchOptions) (int, error) { calls++; return 1234, nil })
	registry := &failingRegistry{fail: true, testRegistry: testRegistry{store: store}}
	host.config.Registry = registry
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: "role"}
	if err := host.Prepare(ctx, need); err == nil || calls != 0 {
		t.Fatal("missing persisted prepared intent without native start")
	}
	before := hostWorker(t, store)
	registry.fail = false
	refreshed := false
	host.config.SettingsAdmission = func(_ context.Context, roleID string, commit func() error) error {
		if !refreshed {
			refreshed = true
			if err := store.Within(ctx, func(tx *workstore.Tx) error {
				role, err := tx.GetRole(roleID)
				if err != nil {
					return err
				}
				role.SettingsRevision++
				role.Revision++
				return tx.PutRole(role, role.Revision-1)
			}); err != nil {
				return err
			}
		}
		return commit()
	}
	recovered, err := New(store, host.service, host.signer, host.config)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.Prepare(ctx, need); !errors.Is(err, workstore.ErrConflict) || calls != 0 {
		t.Fatalf("recovered old settings started: launches=%d, err=%v", calls, err)
	}
	if err := recovered.Prepare(ctx, need); err != nil {
		t.Fatalf("fresh recovery after settings change: %v", err)
	}
	after := hostWorker(t, store)
	if calls != 1 || after.Generation != before.Generation+1 || after.AppliedSettingsRevision != before.AppliedSettingsRevision+1 {
		t.Fatalf("fresh recovery did not replace stale generation: launches=%d worker=%+v", calls, after)
	}
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		oldLaunch, err := tx.GetOperation(launchID(before))
		if err != nil {
			return err
		}
		count, err := tx.CountNativePoolCarriers(host.config.HostID)
		if err != nil {
			return err
		}
		if oldLaunch.State != work.OperationTerminal || count != 1 {
			t.Fatalf("superseded launch retained its reservation: launch=%s count=%d", oldLaunch.State, count)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
