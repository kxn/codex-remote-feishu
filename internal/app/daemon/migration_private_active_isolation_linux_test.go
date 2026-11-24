//go:build linux

package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/hostinventory"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/platformingress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	relayruntime "github.com/kxn/codex-remote-feishu/internal/runtime"
)

func TestActiveSourceCrashIsolatesOnlyProvenDeadCarrier(t *testing.T) {
	a, store, attempt, now := activePersonalSourceFixture(t)
	r := a.teamRuntime
	ctx := context.Background()
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		reaction, e := tx.GetPrivateReaction("input-reaction:" + attempt.InputIDs[0])
		if e != nil {
			return e
		}
		previous := reaction.Revision
		reaction.Revision++
		reaction.InFlightAction, reaction.InFlightEmoji = "create", "THINKING"
		reaction.UpdatedAt = now
		return tx.PutPrivateReaction(reaction, previous)
	}); err != nil {
		t.Fatal(err)
	}
	var worker work.WorkerSession
	var process agentproto.RuntimeProcessIdentity
	var snapshot agentproto.RuntimeLaunchSnapshot
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		worker, e = tx.GetWorker(attempt.RunRef.WorkerID)
		if e != nil {
			return e
		}
		process, e = tx.GetCarrierProcess(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		snapshot, e = tx.GetNativeLaunchSnapshot(worker.ID, worker.Generation)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Within(ctx, func(tx *workstore.Tx) error {
		carrier, e := tx.GetCarrier(worker.CarrierIncarnation)
		if e != nil {
			return e
		}
		binding := carrier.Owner
		binding.AttemptID = attempt.ID
		binding.RunRef = attempt.RunRef.RunID
		command := agentproto.Command{CommandID: "unknown-control", Kind: agentproto.CommandThreadGoalClear, Execution: &binding, Target: agentproto.Target{ThreadID: "native-thread"}}
		commandRaw, e := json.Marshal(command)
		if e != nil {
			return e
		}
		commandBody := work.NewContent("unknown-control:request", worker.ScopeID, "application/json", commandRaw, now)
		if e = tx.PutContent(commandBody); e != nil {
			return e
		}
		operation := work.Operation{ID: command.CommandID, ScopeID: worker.ScopeID, ContextID: worker.ContextID, Actor: work.Actor{Kind: work.ActorSystem, ID: "admission-gate"}, Kind: "native.gate_stop", TargetID: worker.ID, TargetGeneration: worker.Generation, IdempotencyKey: command.CommandID, RequestHash: commandBody.SHA256, RequestRef: commandBody.ID, State: work.OperationUnknown, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, _, e = tx.PutOperation(operation, 0); e != nil {
			return e
		}
		if _, e = tx.RegisterNativeCommand(worker.CarrierIncarnation, command); e != nil {
			return e
		}
		publish := work.Operation{ID: "uncertain-publication", ScopeID: worker.ScopeID, Actor: work.Actor{Kind: work.ActorSystem, ID: "output"}, Kind: "feishu.publish", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: "uncertain-publication", RequestHash: "hash", State: work.OperationUnknown, Steps: []work.OperationStep{{ID: "first_send", State: work.OperationUnknown}}, Revision: 1, CreatedAt: now, UpdatedAt: now}
		if _, _, e = tx.PutOperation(publish, 0); e != nil {
			return e
		}
		if e = tx.PutOutbox(work.Outbox{ID: "uncertain-outbox", ScopeID: worker.ScopeID, OperationID: publish.ID, Target: "chat", EventID: "uncertain-publication", PayloadRef: commandBody.ID, State: work.OutboxUnknown, Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutContext(work.WorkContext{ID: "historical-context", ScopeID: worker.ScopeID, RoleID: worker.RoleID, Workstream: "legacy:test", Revision: 1, CreatedAt: now, UpdatedAt: now}, 0); e != nil {
			return e
		}
		return tx.PutNativeRequest(workstore.NativeRequest{ID: "stale-request", ScopeID: worker.ScopeID, CarrierIncarnation: worker.CarrierIncarnation, Binding: binding, NativeRequestID: "native-old", State: "stale", Revision: 1, CreatedAt: now, UpdatedAt: now}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	// Simulate the old carrier dying; the test owns this child process.
	proc, err := os.FindProcess(process.PID)
	if err != nil {
		t.Fatal(err)
	}
	if err = proc.Kill(); err != nil {
		t.Fatal(err)
	}
	var status syscall.WaitStatus
	if _, err = syscall.Wait4(process.PID, &status, 0, nil); err != nil {
		t.Fatal(err)
	}
	child := exec.Command("sleep", "60")
	hostinventory.Prepare(child)
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	scope, err := hostinventory.Bind(child)
	if err != nil {
		t.Fatal(err)
	}
	scope.MarkControlledStop()
	if err = child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	scope.MarkWaitObserved()
	nativeScope := scope.Inspect()
	_ = scope.Close()
	if !nativeScope.Complete || nativeScope.RootAlive {
		t.Fatal("native child still alive")
	}
	stateDir := filepath.Dir(filepath.Dir(filepath.Dir(snapshot.JournalPath)))
	a.headlessRuntime.Paths.StateDir = stateDir
	r.codexUpgradeHostID = "local"
	journal, err := runtimejournal.Open(ctx, snapshot.JournalPath, stateDir, snapshot.CWD, snapshot.CarrierIncarnation)
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.BindLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	inventory := map[string]any{"owner": snapshot.Owner, "scopes": []hostinventory.Snapshot{nativeScope}, "active_native_commands": map[string]bool{"unknown": true}, "pending_inputs": map[string]string{}, "active_tools": map[string]bool{}, "goal_active": map[string]bool{}, "goal_known": map[string]bool{}, "session_ids": map[string]bool{}, "tasks": map[string]bool{}, "uncertainties": map[string]bool{}, "task_inventory_known": true, "turns": map[string]any{"old": map[string]bool{"Active": true, "Terminal": false}}}
	raw, _ := json.Marshal(inventory)
	if err = journal.SaveInventory(raw); err != nil {
		t.Fatal(err)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "fixture-secret"}}
	configured, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
	setTestWorkBotSettings(t, configured, state.BotCapabilitySettingsRecord{GatewayID: "gateway", Backend: agentproto.BackendCodex})
	configured.teamRuntime = r
	configured.headlessRuntime.Paths.StateDir = stateDir
	r.app = configured
	loaded, e := configured.loadAdminConfig()
	if e != nil {
		t.Fatal(e)
	}
	configured.mu.Lock()
	settings := configured.workBotSettingsLocked("gateway")
	resolved := configured.resolveWorkBotSettings(settings, loaded.Config)
	configured.mu.Unlock()
	if err = store.Within(ctx, func(tx *workstore.Tx) error { return tx.SyncRoleSettingsReference(worker.RoleID, resolved.fingerprint) }); err != nil {
		t.Fatal(err)
	}
	coordinator := &personalMigrationCoordinator{app: configured}
	router, e := ingressowner.Open(filepath.Join(t.TempDir(), "neutral.db"), nil, coordinator)
	if e != nil {
		t.Fatal(e)
	}
	defer router.Close()
	coordinator.router = router
	verifier := teamMigrationOwnerVerifier{store: store}
	router.SetOwnerScopeBindingVerifier(verifier)
	lock, e := relayruntime.AcquireLock(ctx, filepath.Join(t.TempDir(), "daemon.lock"), false)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Release()
	controller := feishu.NewMultiGatewayController()
	router.SetGatewayCoverageVerifier(applicationCoverageVerifier{controller: controller, previous: daemonLockPreviousVerifier{lock: lock}})
	ingress := &applicationIngress{host: configured, router: router}
	hook, e := ingress.prepareStartupHook()
	if e != nil {
		t.Fatal(e)
	}
	localAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "offline fixture", http.StatusServiceUnavailable)
	}))
	defer localAPI.Close()
	if e = controller.UpsertApp(ctx, feishu.GatewayAppConfig{GatewayID: "gateway", AppID: "app", AppSecret: "fixture-secret", BotOpenID: "bot", Enabled: true, Domain: localAPI.URL, RawIngressHook: &hook}); e != nil {
		t.Fatal(e)
	}
	gatewayCtx, cancelGateway := context.WithCancel(ctx)
	gatewayDone := make(chan error, 1)
	go func() {
		gatewayDone <- controller.Start(gatewayCtx, func(context.Context, control.Action) *feishu.ActionResult { return nil })
	}()
	defer func() { cancelGateway(); <-gatewayDone }()
	var startup feishu.StartupIngressProof
	deadline := time.Now().Add(3 * time.Second)
	for {
		startup, e = controller.StartupIngressProof("gateway")
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("gateway startup: %v", e)
		}
		time.Sleep(time.Millisecond)
	}
	bindings, e := coordinator.PrepareHistoricalBindings(ctx)
	if e != nil || len(bindings) != 1 {
		t.Fatalf("historical binding: %v", e)
	}
	binding := bindings[0]
	migrationID, importID, e := coordinator.MigrationIDs(ctx, binding)
	if e != nil {
		t.Fatal(e)
	}
	coverage := ingressowner.GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: router.RouterID(), Epoch: 1, CutoverID: "active-fixture"}
	previous, _ := json.Marshal(map[string]string{"kind": "exclusive-daemon-lock", "router_id": router.RouterID(), "cutover_id": coverage.CutoverID})
	coverageProof, _ := json.Marshal(startupCoverageEvidence{Startup: startup, Previous: previous})
	if err = router.RecordGatewayCoverage(ctx, coverage, coverageProof); err != nil {
		t.Fatal(err)
	}
	proof, e := router.BeginBoundOwnerMigration(ctx, binding, migrationID)
	if e != nil {
		t.Fatal(e)
	}
	proofRaw, _ := json.Marshal(proof)
	proofID := "migration-intake-proof:" + proof.Digest
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		f, e := tx.GetPersonalMigrationFence("scope")
		if e != nil {
			return e
		}
		if _, e = tx.StartPersonalMigrationDrain("scope", migrationID, f.Revision, now); e != nil {
			return e
		}
		if e = tx.PutContent(work.NewContent(proofID, "scope", "application/json", proofRaw, now)); e != nil {
			return e
		}
		return tx.PreservePersonalMigrationUnsent("scope", "operation", "start", 1, now)
	}); err != nil {
		t.Fatal(err)
	}
	if err = r.isolateExitedPersonalWorkers(ctx, "scope", proofID, now); err != nil {
		t.Fatal(err)
	}
	if err = r.holdUncertainPersonalPublications(ctx, "scope", proofID, now); err != nil {
		t.Fatal(err)
	}
	if err = r.holdUncertainPersonalReactions(ctx, "scope", proofID, now); err != nil {
		t.Fatal(err)
	}
	if err = r.isolatePersonalControlOperations(ctx, "scope", proofID, now); err != nil {
		t.Fatal(err)
	}

	var isolation workstore.PersonalExecutionIsolation
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		var e error
		isolation, e = tx.GetPersonalExecutionIsolation("scope", worker.ID)
		if e != nil {
			return e
		}
		state, e := tx.PersonalMigrationStatus("scope")
		if e != nil {
			return e
		}
		if state.HeldExecutionAttempts != 1 || state.UnsettledAttempts != 0 {
			t.Fatalf("wrong component accounting: %+v", state)
		}
		old, e := tx.GetAttempt(attempt.ID)
		if e != nil {
			return e
		}
		if old.State != work.AttemptRunning {
			t.Fatal("source execution changed")
		}
		in, e := tx.GetInbox(attempt.InputIDs[0])
		if e != nil {
			return e
		}
		if in.State != work.InboxClaimed {
			t.Fatal("claimed input lost")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if isolation.NativeCommandsIdle || isolation.TurnsTerminal || !isolation.ProcessScopesStopped {
		t.Fatal("unknown native state was claimed idle")
	}
	if err = r.verifyPersonalExecutionIsolation(ctx, isolation); err != nil {
		t.Fatal(err)
	}
	// Restart changes coverage proof while each frozen component keeps its
	// original intake proof. Repeat the production drain with all four kinds.
	coverage.Epoch++
	coverage.CutoverID = "active-fixture-restarted"
	previous, _ = json.Marshal(map[string]string{"kind": "exclusive-daemon-lock", "router_id": router.RouterID(), "cutover_id": coverage.CutoverID})
	coverageProof, _ = json.Marshal(startupCoverageEvidence{Startup: startup, Previous: previous})
	if err = router.RecordGatewayCoverage(ctx, coverage, coverageProof); err != nil {
		t.Fatal(err)
	}
	drained, err := r.advanceBoundPersonalSourceDrain(ctx, router, binding, migrationID, now)
	if err != nil {
		t.Fatalf("repeated source drain after coverage renewal: %v", err)
	}
	if drained.Neutral.Digest == proof.Digest {
		t.Fatal("restart did not renew coverage proof")
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		for _, v := range []struct{ kind, id string }{{"execution", worker.ID}, {"control", "unknown-control"}, {"publication", "uncertain-outbox"}, {"reaction", "input-reaction:" + attempt.InputIDs[0]}} {
			id, exists, e := tx.PersonalMigrationComponentProof("scope", v.kind, v.id, "new")
			if e != nil {
				return e
			}
			if !exists || id != proofID {
				return fmt.Errorf("%s lost original proof", v.kind)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// A changed published outbox cannot reuse its old component evidence.
	rollback := errors.New("rollback deliberate drift")
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		out, e := tx.GetOutbox("uncertain-outbox")
		if e != nil {
			return e
		}
		old := out.Revision
		out.Revision++
		out.NextAttemptAt = &now
		if e = tx.PutOutbox(out, old); e != nil {
			return e
		}
		if e = tx.HoldPersonalUncertainPublication("scope", out.ID, proofID, now); e == nil {
			return fmt.Errorf("changed outbox accepted: %v", e)
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatalf("drift fixture: %v", err)
	}
	// A hold for this worker's current generation must not suppress
	// reconciliation of a different-generation occupied attempt.
	rawDB, e := sql.Open("sqlite", filepath.Join(stateDir, "work.db"))
	if e != nil {
		t.Fatal(e)
	}
	var originalAttempt string
	if e = rawDB.QueryRowContext(ctx, "SELECT payload FROM attempts WHERE id=?", attempt.ID).Scan(&originalAttempt); e != nil {
		t.Fatal(e)
	}
	var stopScope, stopMedia, stopHash, stopCreated string
	var stopData []byte
	stopID := "private-stop:" + attempt.ID
	if e = rawDB.QueryRowContext(ctx, "SELECT scope_id,media_type,sha256,data,created_at FROM content WHERE id=?", stopID).Scan(&stopScope, &stopMedia, &stopHash, &stopData, &stopCreated); e != nil {
		t.Fatal(e)
	}
	if _, e = rawDB.ExecContext(ctx, "DELETE FROM content WHERE id=?", stopID); e != nil {
		t.Fatal(e)
	}
	otherGeneration := attempt
	otherGeneration.RunRef.Generation++
	otherRaw, _ := json.Marshal(otherGeneration)
	if _, e = rawDB.ExecContext(ctx, "UPDATE attempts SET payload=? WHERE id=?", string(otherRaw), attempt.ID); e != nil {
		t.Fatal(e)
	}
	_, crossGenerationErr := r.advancePersonalSourceDrain(ctx, "scope", migrationID, now)
	if _, e = rawDB.ExecContext(ctx, "UPDATE attempts SET payload=? WHERE id=?", originalAttempt, attempt.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = rawDB.ExecContext(ctx, "DELETE FROM content WHERE id=?", stopID); e != nil {
		t.Fatal(e)
	}
	if _, e = rawDB.ExecContext(ctx, "INSERT INTO content(id,scope_id,media_type,sha256,data,created_at) VALUES(?,?,?,?,?,?)", stopID, stopScope, stopMedia, stopHash, stopData, stopCreated); e != nil {
		t.Fatal(e)
	}
	if e = rawDB.Close(); e != nil {
		t.Fatal(e)
	}
	if crossGenerationErr == nil || !strings.Contains(crossGenerationErr.Error(), "attempt "+attempt.ID) {
		t.Fatalf("different generation failed to reach reconciliation: %v", crossGenerationErr)
	}
	proof = drained.Neutral
	// The public coordinator cannot bypass invalid coverage and reach seal.
	router.SetGatewayCoverageVerifier(migrationCoverageDeniedFixture{})
	if _, e = coordinator.AdvanceOwner(ctx, binding, migrationID, importID, now); !errors.Is(e, workstore.ErrScopeDenied) {
		t.Fatalf("AdvanceOwner accepted unavailable coverage: %v", e)
	}
	router.SetGatewayCoverageVerifier(applicationCoverageVerifier{controller: controller, previous: daemonLockPreviousVerifier{lock: lock}})
	targetBinding, e := coordinator.ClaimTargetBinding(ctx, binding, migrationID, now)
	if e != nil {
		t.Fatal(e)
	}
	limits := workstore.PersonalExportLimits{MaxRows: 10000, MaxBytes: 8 << 20}
	seal, err := configured.sealPersonalSource(ctx, router, binding, proof, limits)
	if err != nil {
		t.Fatal(err)
	}
	privateDir := t.TempDir()
	targetJournal, err := privatechat.OpenJournal(filepath.Join(privateDir, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer targetJournal.Close()
	coordinator.journal = targetJournal
	coordinator.assetRoot = privateDir
	coordinator.limits = limits
	targetOwner := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "human"), ChatID: "chat", UserID: "human", TenantID: "tenant", AppID: "app"}
	testAdapter := teamSealedImportVerifier{app: configured, router: router, binding: binding, target: targetBinding.Owner, admitter: privateApplicationAdmitter{router: router}, assetRoot: privateDir, limits: limits}
	bundle, e := testAdapter.LoadVerifiedMigration(ctx, privatechat.MigrationImportRequest{ImportID: importID, SourceHandle: migrationID, Owner: targetOwner, Generation: targetBinding.Owner.Epoch})
	if e != nil {
		t.Fatalf("source adapter: %v", e)
	}
	preservedContext, preservedRequest := false, false
	for _, record := range bundle.Records {
		if record.Table == "contexts" && record.Key == "historical-context" {
			preservedContext = true
		}
		if record.Table == "native_ui_requests" && record.Key == "stale-request" {
			preservedRequest = true
		}
	}
	if !preservedContext || !preservedRequest {
		t.Fatal("historical context or stale request evidence lost")
	}
	for _, hold := range bundle.Checkpoint.Holds {
		if hold.Kind == "source_native_ui_requests" {
			t.Fatal("stale request blocked unrelated owner")
		}
		if hold.Kind == "source_operations" || hold.Kind == "source_native_commands" {
			t.Fatalf("isolated control blocked owner: %s", hold.Kind)
		}
	}
	progress, err := coordinator.AdvanceOwner(ctx, binding, migrationID, importID, now)
	if err != nil || progress.Stage != "activated" {
		cp, e := targetJournal.LoadCheckpoint(ctx, targetOwner, targetBinding.Owner.Epoch)
		var kinds []string
		if e == nil {
			for _, h := range cp.Core.Holds {
				kinds = append(kinds, h.Kind)
			}
		}
		t.Fatalf("source through target activation at %s, holds %v: %v", progress.Stage, kinds, err)
	}
	if progress.Source.SourceHash != seal.SourceHash {
		t.Fatal("source seal changed")
	}
	replay, e := coordinator.AdvanceOwner(ctx, binding, migrationID, importID, now)
	if e != nil || replay.Stage != "activated" || replay.Receipt.Digest != progress.Receipt.Digest {
		t.Fatalf("repeated migration changed result: %v", e)
	}
	target := progress.Receipt.Owner
	pending, err := targetJournal.Pending(ctx, target, progress.Receipt.Generation, "input", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range pending {
		if entry.Ingress.EventID == "event" {
			t.Fatal("claimed source input automatically re-admitted")
		}
	}
	summary, err := targetJournal.MigrationRecoverySummary(ctx, target, progress.Receipt.Generation)
	if err != nil || summary.ClaimedInputs != 1 || summary.UnresolvedExecutions != 1 {
		t.Fatalf("missing component recovery: %+v %v", summary, err)
	}
	if summary.UnresolvedControls != 1 {
		t.Fatalf("missing uncertain control explanation: %+v", summary)
	}
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "human"), ChatID: "chat", UserID: "human", TenantID: "tenant", AppID: "app"}
	if target != owner {
		t.Fatal("wrong target identity")
	}
	if held, e := targetJournal.HasUncertainMigratedReaction(ctx, target, progress.Receipt.Generation, "message"); e != nil || !held {
		t.Fatalf("old reaction chain not isolated: %v %v", held, e)
	}
	if held, e := targetJournal.HasUncertainMigratedReaction(ctx, target, progress.Receipt.Generation, "new-message"); e != nil || held {
		t.Fatalf("unrelated reaction chain blocked: %v %v", held, e)
	}
	configured.daemonLifecycleID = "fixture"
	configured.applications = &applicationRuntime{journal: targetJournal}
	app := privatechat.New(nil, orchestrator.Config{})
	configured.service = app.Service()
	processor, err := privatechat.NewProcessor(app, targetJournal, privateActionDecoder{}, privateHostReducer{configured})
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range pending {
		if _, err = processor.ApplyIngress(ctx, owner, progress.Receipt.Generation, entry.Sequence); err != nil {
			t.Fatalf("target pending input: %v", err)
		}
	}
	action := control.Action{Kind: control.ActionTextMessage, Text: "/recover 1", GatewayID: owner.GatewayID, SurfaceSessionID: owner.SurfaceID, ChatID: owner.ChatID, ActorUserID: owner.UserID, MessageID: "recover-sdk"}
	actionRaw, _ := json.Marshal(action)
	seq, _, err := targetJournal.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: progress.Receipt.Generation, EventID: "recover-sdk-event", Lane: "input", Payload: actionRaw, Sources: []string{"recover-sdk"}})
	if err != nil {
		t.Fatal(err)
	}
	effects, err := processor.ApplyIngress(ctx, owner, progress.Receipt.Generation, seq)
	if err != nil {
		t.Fatal(err)
	}
	shown := false
	for _, effect := range effects {
		event, _, e := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Notice != nil && event.Notice.Text == "preserve this" {
			shown = true
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			t.Fatal("view replayed source input")
		}
	}
	if !shown {
		t.Fatal("claimed source input was not visible")
	}
	ingress.resolver = applicationOwnerResolver{router: router}
	ingress.team = teamApplicationAdmitter{host: configured}
	ingress.private = privateApplicationAdmitter{host: configured, router: router, journal: targetJournal, processor: processor, wake: func() {}}
	fresh := platformingress.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "fresh-sdk-event", Tenant: "tenant", SenderTenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "fresh-sdk", SenderType: "user", SenderIDs: platformingress.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"new independent input"}`, ReceivedAt: now}
	if err = ingress.hook().Receive(ctx, fresh); err != nil {
		t.Fatalf("new SDK ingress: %v", err)
	}
	freshPending, err := targetJournal.Pending(ctx, owner, progress.Receipt.Generation, "input", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range freshPending {
		if entry.Ingress.EventID == fresh.EventID {
			found = true
			if _, err = processor.ApplyIngress(ctx, owner, progress.Receipt.Generation, entry.Sequence); err != nil {
				t.Fatalf("new private input blocked by unrelated old execution: %v", err)
			}
		}
	}
	if !found {
		t.Fatal("fresh SDK event did not reach target journal")
	}

	if err = lock.Release(); err != nil {
		t.Fatal(err)
	}
	if err = configured.verifyPersonalSourceSeal(ctx, router, binding, seal, limits); err == nil {
		t.Fatal("source proof survived loss of daemon lock")
	}

}
