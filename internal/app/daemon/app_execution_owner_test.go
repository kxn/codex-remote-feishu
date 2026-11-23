package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workengine"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workruntime"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workservice"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestNativeHistoryNamespaceUsesHistoryRootAndHost(t *testing.T) {
	root := t.TempDir()
	cwd := t.TempDir()
	for _, backend := range []work.Backend{work.BackendCodex, work.BackendClaude} {
		env := []string{"HOME=" + root}
		first, err := nativeHistoryNamespace(agentproto.Backend(backend), env, cwd, "host")
		if err != nil {
			t.Fatal(err)
		}
		same, err := nativeHistoryNamespace(agentproto.Backend(backend), env, cwd, "host")
		if err != nil || first != same {
			t.Fatalf("unstable: %q %v", same, err)
		}
		other, _ := nativeHistoryNamespace(agentproto.Backend(backend), env, cwd, "host2")
		if first == other {
			t.Fatal("host not scoped")
		}
		key := "CODEX_HOME="
		if backend == work.BackendClaude {
			key = "CLAUDE_CONFIG_DIR="
		}
		different, err := nativeHistoryNamespace(agentproto.Backend(backend), append(env, key+filepath.Join(root, "alternate")), cwd, "host")
		if err != nil || different == first {
			t.Fatalf("history root ignored: %v", err)
		}
	}
}

func TestTeamPrepareOwnerLeaseHelloAndReceipt(t *testing.T) {
	ctx := t.Context()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "cli_team", AppSecret: "secret"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	app.headlessRuntime.BaseEnv = []string{"CLAUDE_CONFIG_DIR=" + filepath.Join(t.TempDir(), "claude")}
	setTestWorkBotSettings(t, app, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendClaude})
	store, err := workstore.Open(ctx, filepath.Join(t.TempDir(), "team.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	app.teamRuntime = &teamRuntime{store: store}
	if err = app.syncWorkRoles(ctx, store); err != nil {
		t.Fatal(err)
	}
	roleID := teamID("role", "gateway")
	scope := work.WorkScope{ID: "scope", Kind: work.ScopePersonal, RoleID: roleID, PrincipalID: "human", Revision: 1}
	pickerTx(t, app, func(tx *workstore.Tx) error {
		if e := tx.PutPrincipal(work.Principal{ID: "human", DisplayName: "Human", Revision: 1}, 0); e != nil {
			return e
		}
		return tx.PutScope(scope, 0)
	})
	signer, err := workauth.New([]byte("0123456789abcdef0123456789abcdef"), nil)
	if err != nil {
		t.Fatal(err)
	}
	app.teamRuntime.signer = signer
	service, err := workservice.New(store, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	var receipts []agentproto.EventReceipt
	failReceipt := true
	coordinator := workruntime.New(store, workruntime.Options{SendReceipt: func(_ string, r agentproto.EventReceipt) error {
		receipts = append(receipts, r)
		if failReceipt {
			return errors.New("receipt transport failed")
		}
		return nil
	}})
	adapter, err := openTeamExecutionOwners(store, coordinator, app.headlessRuntime.Paths.StateDir, "host")
	if err != nil {
		t.Fatal(err)
	}
	defer adapter.Close()
	var launchEnv []string
	host, err := workhost.New(store, service, signer, workhost.Config{Paths: app.headlessRuntime.Paths, BinaryPath: "test-wrapper", HostID: "host", ToolEndpoint: "http://127.0.0.1:9090", Settings: teamSettingsResolver{app: app, store: store, hostID: "host"}, Registry: adapter, Runtime: teamSettingsTestRuntime{}, Start: func(options relayruntime.HeadlessLaunchOptions) (int, error) {
		launchEnv = options.Env
		return 1234, nil
	}, StopCarrier: func(context.Context, work.WorkerSession) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	host.EnableSharedRelayCredential()
	if err = host.Prepare(ctx, workengine.WorkerNeed{ScopeID: scope.ID, RoleID: roleID}); err != nil {
		t.Fatal(err)
	}
	var worker work.WorkerSession
	var carrier workstore.CarrierRegistration
	pickerTx(t, app, func(tx *workstore.Tx) error {
		workers, e := tx.ListWorkers(scope.ID)
		if e != nil {
			return e
		}
		if len(workers) != 1 {
			t.Fatalf("workers %d", len(workers))
		}
		worker = workers[0]
		carrier, e = tx.GetCarrier(worker.CarrierIncarnation)
		return e
	})
	hello := agentproto.Hello{Instance: agentproto.InstanceHello{InstanceID: worker.InstanceID, Backend: agentproto.BackendClaude, Managed: true, Source: "headless"}, WorkerProtocolVersion: agentproto.WorkerProtocolVersion, JournalProtocolVersion: agentproto.JournalProtocolVersion, CarrierIncarnation: worker.CarrierIncarnation}
	privateStore, err := openPrivateNativeHostStore(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer privateStore.Close()
	app.applications = &applicationRuntime{native: &privateNativeHost{store: privateStore}, teamOwners: adapter}
	credential, err := signer.RelayCredential(carrier.Incarnation, carrier.OriginInstanceID, string(carrier.Backend))
	if err != nil {
		t.Fatal(err)
	}
	if !containsEnvEntry(launchEnv, agentproto.TeamRelayCredentialEnv+"="+credential) {
		t.Fatal("exact carrier credential absent from launch")
	}
	if app.authorizeApplicationHello(hello, "") == nil || app.authorizeApplicationHello(hello, "wrong") == nil {
		t.Fatal("missing or wrong team credential accepted")
	}
	if err = app.authorizeApplicationHello(hello, credential); err != nil {
		t.Fatalf("valid team credential rejected: %v", err)
	}
	forged := hello
	forged.CarrierIncarnation = "other"
	if app.authorizeApplicationHello(forged, credential) == nil {
		t.Fatal("credential accepted for another carrier")
	}
	lease, err := adapter.acceptHello(ctx, worker.InstanceID, hello)
	if err != nil {
		t.Fatal(err)
	}
	command := agentproto.Command{CommandID: "model-list", Kind: agentproto.CommandModelList, Execution: &carrier.Owner}
	raw, err := json.Marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	body := work.NewContent("model-list-body", scope.ID, "application/json", raw, now)
	pickerTx(t, app, func(tx *workstore.Tx) error {
		if e := tx.PutContent(body); e != nil {
			return e
		}
		op := work.Operation{ID: command.CommandID, ScopeID: scope.ID, ContextID: carrier.Owner.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "inspector"}, Kind: string(command.Kind), TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: command.CommandID, RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationSending, Revision: 1, CreatedAt: now, UpdatedAt: now}
		_, _, e := tx.PutOperation(op, 0)
		return e
	})
	if err = lease.registerCommand(ctx, command); err != nil {
		t.Fatal(err)
	}
	event := agentproto.Event{Kind: agentproto.EventModelCatalogUpdated, Seq: 1, CommandID: command.CommandID, Execution: &carrier.Owner, ModelCatalog: &agentproto.ModelCatalogSnapshot{}}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: carrier.Incarnation, Sequence: 1, PayloadHash: hash}
	bad := event
	bad.Journal = &agentproto.RuntimeEventIdentity{Version: agentproto.JournalProtocolVersion, CarrierIncarnation: carrier.Incarnation, Sequence: 1, PayloadHash: "invalid"}
	if err = lease.handleEvent(ctx, worker.InstanceID, bad); err == nil || len(receipts) != 0 {
		t.Fatalf("failed commit sent receipt: %v", err)
	}
	if err = lease.handleEvent(ctx, worker.InstanceID, agentproto.Event{Kind: agentproto.EventTurnCompleted}); err == nil {
		t.Fatal("critical unjournaled event accepted")
	}
	if err = lease.handleEvent(ctx, worker.InstanceID, event); err == nil {
		t.Fatal("receipt failure not returned")
	}
	failReceipt = false
	if err = lease.handleEvent(ctx, worker.InstanceID, event); err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 || receipts[0].PayloadHash != hash || receipts[0].CarrierIncarnation != carrier.Incarnation {
		t.Fatalf("receipts: %+v", receipts)
	}
	if err = lease.handleEvent(ctx, "wrong-instance", event); err == nil {
		t.Fatal("cross-instance event accepted")
	}
	exerciseTeamOwnerWebSocket(t, app, adapter, worker, carrier, hello, credential, command, event)
}
