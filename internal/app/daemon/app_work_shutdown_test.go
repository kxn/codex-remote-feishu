package daemon

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/worktransport"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

type shutdownSettings struct{ location work.ExecutionLocation }

func (s shutdownSettings) ResolveLaunch(context.Context, work.WorkScope, work.Role, work.WorkContext) (workhost.LaunchSettings, error) {
	return workhost.LaunchSettings{Backend: work.BackendCodex, Location: s.location, ProfileID: "default", CodexResume: &agentproto.CodexResumePolicy{Mode: agentproto.CodexResumeApplyTargetProfile, ModelProviderID: "openai"}}, nil
}

type shutdownRelay struct {
	send func(string, agentproto.Command) error
}

func (r *shutdownRelay) SendCommand(instance string, command agentproto.Command) error {
	return r.send(instance, command)
}
func TestShutdownCarrierOSHelper(t *testing.T) {
	if os.Getenv("SHUTDOWN_CARRIER_HELPER") == "1" {
		var b [1]byte
		_, _ = os.Stdin.Read(b[:])
		os.Exit(0)
	}
}

func shutdownFixture(t *testing.T, respond bool) (*App, *workstore.Store, *int) {
	return shutdownFixtureWithWitness(t, respond, false)
}
func shutdownFixtureWithWitness(t *testing.T, respond, witness bool) (*App, *workstore.Store, *int) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	store, err := workstore.Open(ctx, filepath.Join(root, "work.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	txRun := func(fn func(*workstore.Tx) error) {
		t.Helper()
		if err := store.Within(ctx, fn); err != nil {
			t.Fatal(err)
		}
	}
	roleID := "role"
	if witness {
		roleID = teamID("role", "gateway")
	}
	txRun(func(tx *workstore.Tx) error {
		if err := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRole(work.Role{ID: roleID, Name: "Role", Description: "Role", DescriptionVersion: 1, SettingsRevision: 1, Revision: 1}, 0); err != nil {
			return err
		}
		if err := tx.PutRoleBotBinding(work.RoleBotBinding{RoleID: roleID, GatewayRef: "gateway", AppID: "app", IdentityGeneration: 1}, 0); err != nil {
			return err
		}
		principalID := "human"
		var ingressID string
		if witness {
			now := time.Now().UTC()
			raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "witness", Tenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "witness-message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"hello"}`, ReceivedAt: now}
			gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: roleID, BotGeneration: 1}
			ingress, _, e := tx.AcceptRawIngress(raw, gateway)
			if e != nil {
				return e
			}
			principal, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
			if e != nil {
				return e
			}
			principalID = principal.ID
			ingressID = ingress.ID
		}
		if e := tx.PutScope(work.WorkScope{ID: "scope", Kind: work.ScopePersonal, PrincipalID: principalID, RoleID: roleID, Revision: 1}, 0); e != nil {
			return e
		}
		if witness {
			nav, e := tx.EnsurePrivateNavigation("scope", ingressID, time.Now())
			if e != nil {
				return e
			}
			return tx.FreezePrivateIngressNavigation(ingressID, nav)
		}
		return nil
	})
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{}
	r := &teamRuntime{app: a, store: store, service: service, signer: signer}
	a.teamRuntime = r
	relay := &shutdownRelay{}
	driver, err := worktransport.New(store, relay, r.beginPersonalSourceActivity)
	if err != nil {
		t.Fatal(err)
	}
	r.driver = driver
	r.coordinator = workruntime.New(store, workruntime.Options{Driver: driver, StateDir: root, EnqueueReconcile: func(string) {}})
	driver.BindRegistrar(r.coordinator)
	var child *exec.Cmd
	var input io.WriteCloser
	starts := 0
	sequence := uint64(0)
	joined := false
	r.runDone = make(chan struct{})
	var once sync.Once
	r.runCancel = func() { once.Do(func() { joined = true; close(r.runDone) }) }
	r.active.Store(true)
	start := func(relayruntime.HeadlessLaunchOptions) (int, error) {
		starts++
		child = exec.Command(os.Args[0], "-test.run=^TestShutdownCarrierOSHelper$")
		child.Env = append(os.Environ(), "SHUTDOWN_CARRIER_HELPER=1")
		var e error
		input, e = child.StdinPipe()
		if e != nil {
			return 0, e
		}
		if e = child.Start(); e != nil {
			return 0, e
		}
		identity, e := hostinventory.CaptureProcess(child.Process.Pid)
		if e != nil {
			return 0, e
		}
		var worker work.WorkerSession
		txRun(func(tx *workstore.Tx) error {
			workers, e := tx.ListWorkers("scope")
			if e != nil {
				return e
			}
			worker = workers[0]
			return nil
		})
		e = r.coordinator.HandleHello(ctx, agentproto.Hello{WorkerProtocolVersion: 1, JournalProtocolVersion: 1, CarrierIncarnation: worker.CarrierIncarnation, CarrierProcess: &agentproto.RuntimeProcessIdentity{HostID: "local", Platform: identity.Platform, PID: identity.PID, StartIdentity: identity.StartIdentity}, Instance: agentproto.InstanceHello{InstanceID: worker.InstanceID, PID: identity.PID, Backend: agentproto.BackendCodex, Source: "headless", Managed: true}})
		return identity.PID, e
	}
	r.host, err = workhost.New(store, service, signer, workhost.Config{Paths: relayruntime.Paths{StateDir: root}, HostID: "local", BinaryPath: "fake-native-carrier", ToolEndpoint: "http://127.0.0.1:1", Start: start, Settings: shutdownSettings{location: work.ExecutionLocation{ID: "location", HostID: "local", Cwd: t.TempDir(), Generation: 1, Revision: 1}}, Registry: r.coordinator, Runtime: driver, StopCarrier: a.stopWorkCarrier})
	if err != nil {
		t.Fatal(err)
	}
	relay.send = func(instance string, command agentproto.Command) error {
		if !joined {
			t.Fatal("shutdown sent native work before scheduler joined")
		}
		if command.Kind == agentproto.CommandProcessExit {
			if err := input.Close(); err != nil {
				return err
			}
			return child.Wait()
		}
		if command.Kind != agentproto.CommandRuntimeInspect {
			t.Fatalf("unexpected shutdown command: %s", command.Kind)
		}
		if !respond {
			return nil
		}
		sequence++
		var carrier workstore.CarrierRegistration
		txRun(func(tx *workstore.Tx) error {
			worker, e := tx.GetWorker(command.Execution.WorkerSessionID)
			if e != nil {
				return e
			}
			carrier, e = tx.GetCarrier(worker.CarrierIncarnation)
			return e
		})
		bootstrap := agentproto.RPCProbe{Request: json.RawMessage(`{"id":"initialize","method":"initialize"}`), Response: json.RawMessage(`{"id":"initialize","result":{}}`)}
		event := agentproto.Event{Kind: agentproto.EventRuntimeInspectionCompleted, CommandID: command.CommandID, Execution: agentproto.CloneExecutionBinding(command.Execution), RuntimeInspection: &agentproto.RuntimeInspectionResult{Version: 1, RequestCommandID: command.CommandID, CarrierIncarnation: carrier.Incarnation, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, OwnerScope: &agentproto.RuntimeOwnerInspection{Bootstrap: bootstrap, KnownSessionsComplete: true}, Background: agentproto.BackgroundInspection{CarrierIncarnation: carrier.Incarnation, ProcessGeneration: 1, LocationGeneration: 1, Complete: true, NativeInputsComplete: true, NativeTaskSetComplete: true, ContinuationInventoryKnown: true, Evidence: json.RawMessage(`{"controlled_fake_native_inventory":"empty"}`)}, ObservedAt: time.Now().UTC()}, Seq: sequence}
		hash, e := agentproto.RuntimeEventPayloadHash(event)
		if e != nil {
			return e
		}
		event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: carrier.Incarnation, Sequence: sequence, PayloadHash: hash}
		if e = r.coordinator.HandleEvent(ctx, instance, event); e != nil {
			return e
		}
		return driver.DeliverInspection(ctx, event)
	}
	need := workengine.WorkerNeed{ScopeID: "scope", RoleID: roleID}
	if witness {
		txRun(func(tx *workstore.Tx) error {
			nav, e := tx.GetPrivateNavigation("scope")
			if e != nil {
				return e
			}
			need.ContextID = nav.FrontContextID
			return nil
		})
	}
	if err = r.host.Prepare(ctx, need); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if input != nil {
			input.Close()
		}
		if child != nil && child.Process != nil {
			child.Process.Kill()
		}
	})
	return a, store, &starts
}

func TestWorkShutdownJoinsSchedulerThenVerifiesExactCarrierExitAndCanReprepare(t *testing.T) {
	a, store, starts := shutdownFixture(t, true)
	if err := a.shutdownWorkRuntime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if a.teamRuntime.active.Load() {
		t.Fatal("shutdown left dispatch active")
	}
	var worker work.WorkerSession
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		workers, e := tx.ListWorkers("scope")
		if e != nil {
			return e
		}
		worker = workers[0]
		if worker.State != work.WorkerRecovering || worker.Generation != 1 {
			t.Fatal("shutdown fabricated a replacement or ready carrier")
		}
		gate, e := tx.GetRuntimeGate(worker.ID)
		if e != nil {
			return e
		}
		if gate.Reason != "snapshot_rebuild_required" {
			t.Fatal(gate.Reason)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.teamRuntime.host.Prepare(context.Background(), workengine.WorkerNeed{ScopeID: "scope", RoleID: "role", ContextID: worker.ContextID}); err != nil {
		t.Fatal(err)
	}
	if *starts != 2 {
		t.Fatal("safe restart failed to create exactly one replacement carrier")
	}
}

func TestWorkShutdownDeadlineKeepsUnverifiedCarrierAndRecoveryGate(t *testing.T) {
	a, store, _ := shutdownFixture(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := a.shutdownWorkRuntime(ctx); err == nil {
		t.Fatal("unanswered inspection claimed clean shutdown")
	}
	if err := store.Within(context.Background(), func(tx *workstore.Tx) error {
		workers, e := tx.ListWorkers("scope")
		if e != nil {
			return e
		}
		worker := workers[0]
		process, e := tx.GetCarrierProcess(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		if _, e = hostinventory.CaptureProcess(process.PID); e != nil {
			t.Fatal("shutdown killed an unverified carrier")
		}
		_, e = tx.GetRuntimeGate(worker.ID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
}
