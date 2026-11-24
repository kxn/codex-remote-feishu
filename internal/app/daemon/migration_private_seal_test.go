package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	gatewaypkg "github.com/kxn/codex-remote-feishu/internal/adapter/feishu/gateway"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/ingressmapper"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type migrationTargetBindingFixture struct{}

type migrationCoverageDeniedFixture struct{}

func (migrationCoverageDeniedFixture) VerifyGatewayCoverage(context.Context, ingressowner.GatewayCoverage, []byte) error {
	return workstore.ErrScopeDenied
}

func (migrationTargetBindingFixture) VerifyPrivateBinding(_ context.Context, _ ingressowner.PrivateBinding, proof []byte) error {
	if string(proof) != "target-fixture" {
		return workstore.ErrScopeDenied
	}
	return nil
}

type migrationTargetReceiptFixture struct {
	journal *privatechat.Journal
	app     *App
	router  *ingressowner.Router
	binding ingressowner.OwnerScopeBinding
	seal    workstore.PersonalMigrationSeal
	owner   privatechat.Owner
	limits  workstore.PersonalExportLimits
}

func (v migrationTargetReceiptFixture) VerifyImport(ctx context.Context, p ingressowner.ImportProof) error {
	receipt, e := v.journal.MigrationReceipt(ctx, v.owner, p.Target.Epoch, p.TransactionID)
	if e != nil {
		return e
	}
	if receipt.SourceHash != p.SourceSnapshotHash || p.SourceSnapshotHash != v.seal.SourceHash {
		return workstore.ErrConflict
	}
	return v.app.verifyPersonalSourceSealLocal(ctx, v.seal, v.limits)
}

func TestPersonalSourceSealRevalidatesNeutralAndSource(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC()
	cfg := config.DefaultAppConfig()
	cfg.Feishu.Apps = []config.FeishuAppConfig{{ID: "gateway", AppID: "app", AppSecret: "secret"}}
	app, _ := newFeishuAdminTestApp(t, cfg, defaultFeishuServices(), &fakeAdminGatewayController{}, false, "")
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
	role := teamID("role", "gateway")
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "message", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"hello"}`, ReceivedAt: now}
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: role, BotGeneration: 1}
		ingress, _, e := tx.AcceptRawIngress(raw, gateway)
		if e != nil {
			return e
		}
		principal, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
		if e != nil {
			return e
		}
		if e = tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, PrincipalID: principal.ID, RoleID: role, Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutPrincipal(work.Principal{ID: "unresolved-person", DisplayName: "Unresolved", Revision: 1}, 0); e != nil {
			return e
		}
		if e = tx.PutScope(work.WorkScope{ID: "unresolved-personal", Kind: work.ScopePersonal, PrincipalID: "unresolved-person", RoleID: role, Revision: 1}, 0); e != nil {
			return e
		}
		nav, e := tx.EnsurePrivateNavigation("personal", ingress.ID, now)
		if e != nil {
			return e
		}
		return tx.FreezePrivateIngressNavigation(ingress.ID, nav)
	}); err != nil {
		t.Fatal(err)
	}
	verifier := teamMigrationOwnerVerifier{store: store}
	binding, witness, err := verifier.historicalBinding(ctx, "personal", "app", "event", 1)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := &personalMigrationCoordinator{app: app}
	routerPath := filepath.Join(t.TempDir(), "neutral.db")
	router, err := ingressowner.Open(routerPath, nil, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	coordinator.router = router
	router.SetOwnerScopeBindingVerifier(verifier)
	router.SetGatewayCoverageVerifier(migrationCoverageFixture{})
	_, _ = binding, witness
	bindings, err := coordinator.PrepareHistoricalBindings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 1 {
		t.Fatal("historical owner not prepared")
	}
	barriers, err := router.ListLegacyScopeBarriers(ctx, "", 10)
	if err != nil || len(barriers) != 1 || barriers[0].Reason != "missing_sdk_identity" {
		t.Fatalf("missing scope guard: %+v %v", barriers, err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertPersonalMigrationOpen("unresolved-personal") }); !errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		t.Fatalf("unresolved source not fenced: %v", err)
	}
	bound := bindings[0]
	// A failed startup can leave the owner committed while rollback lets the
	// old source advance its ingress revision. The next startup must reuse the
	// frozen owner after revalidating both the original and current evidence.
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		record, _, e := tx.PersonalMigrationIngressObservation("personal", "app", "event", 1)
		if e != nil {
			return e
		}
		previous := record.Revision
		record.Revision++
		return tx.PutIngress(record, previous)
	}); err != nil {
		t.Fatal(err)
	}
	if err = router.Close(); err != nil {
		t.Fatal(err)
	}
	router, err = ingressowner.Open(routerPath, nil, coordinator)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	coordinator.router = router
	router.SetOwnerScopeBindingVerifier(verifier)
	router.SetGatewayCoverageVerifier(migrationCoverageFixture{})
	migrationID, importID, err := coordinator.MigrationIDs(ctx, bound)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error { return tx.AssertPersonalMigrationOpen("personal") }); !errors.Is(err, workstore.ErrPersonalMigrationFenced) {
		t.Fatalf("source not fenced before coverage: %v", err)
	}
	again, err := coordinator.PrepareHistoricalBindings(ctx)
	if err != nil || len(again) != 1 || again[0].Owner != bound.Owner {
		t.Fatal("historical owner preparation was not idempotent")
	}
	if err = router.RecordGatewayCoverage(ctx, ingressowner.GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: router.RouterID(), Epoch: 1, CutoverID: "fixture"}, []byte("fixture-only")); err != nil {
		t.Fatal(err)
	}
	targetBinding, err := coordinator.ClaimTargetBinding(ctx, bound, migrationID, now)
	if err != nil {
		t.Fatal(err)
	}
	replayedBinding, err := coordinator.ClaimTargetBinding(ctx, bound, migrationID, now)
	if err != nil || replayedBinding != targetBinding {
		t.Fatal("target claim not idempotent")
	}
	forged := targetBinding
	forged.Owner.ID = "forged"
	claimProof, _ := json.Marshal(personalTargetClaimRef{"personal", migrationID})
	if err = coordinator.VerifyPrivateBinding(ctx, forged, claimProof); err == nil {
		t.Fatal("forged target owner accepted")
	}
	proof, err := router.BeginBoundOwnerMigration(ctx, bound, migrationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		f, e := tx.BeginPersonalMigrationFence("personal", migrationID, now)
		if e != nil {
			return e
		}
		_, e = tx.StartPersonalMigrationDrain("personal", migrationID, f.Revision, now)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	limits := workstore.PersonalExportLimits{MaxRows: 1000, MaxBytes: 4 << 20}
	seal, err := app.sealPersonalSource(ctx, router, bound, proof, limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.verifyPersonalSourceSeal(ctx, router, bound, seal, limits); err != nil {
		t.Fatal(err)
	}
	// A later verified startup is linked without changing the frozen source seal.
	epoch, err := router.NextGatewayCoverageEpoch(ctx, "tenant", "app", "gateway", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = router.RecordGatewayCoverage(ctx, ingressowner.GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: router.RouterID(), Epoch: epoch, CutoverID: "fixture-restart"}, []byte("fixture-only")); err != nil {
		t.Fatal(err)
	}
	if err = app.verifyPersonalSourceSeal(ctx, router, bound, seal, limits); err != nil {
		t.Fatalf("sealed source after coverage renewal: %v", err)
	}
	target := targetBinding.Owner
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: gatewaypkg.SurfaceIDForInbound("gateway", "", "p2p", "human"), ChatID: "chat", UserID: "human", TenantID: "tenant", AppID: "app"}
	privateDir := t.TempDir()
	journal, err := privatechat.OpenJournal(filepath.Join(privateDir, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	adapter := teamSealedImportVerifier{app: app, router: router, binding: bound, target: target, admitter: privateApplicationAdmitter{router: router}, assetRoot: privateDir, limits: limits}
	importer, err := privatechat.NewMigrationImporter(journal, adapter, privateDir)
	if err != nil {
		t.Fatal(err)
	}
	request := privatechat.MigrationImportRequest{ImportID: importID, SourceHandle: migrationID, Owner: owner, Generation: 2}
	receipt, err := importer.Import(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SourceHash != seal.SourceHash || receipt.Cursor != 0 {
		t.Fatal("source seal or pending ingress was not preserved")
	}
	pending, err := journal.Pending(ctx, owner, 2, "input", 0, 10)
	if err != nil || len(pending) != 1 || pending[0].Ingress.EventID != "event" {
		t.Fatal("pending source SDK input lost")
	}
	router.SetMigrationVerifier(migrationTargetReceiptFixture{journal: journal, app: app, router: router, binding: bound, seal: seal, owner: owner, limits: limits})
	checkpoint, err := journal.LoadCheckpoint(ctx, owner, 2)
	if err != nil || len(checkpoint.Core.Holds) != 1 {
		t.Fatal("missing imported recovery barrier")
	}
	hold, err := json.Marshal(checkpoint.Core.Holds[0])
	if err != nil {
		t.Fatal(err)
	}
	holdHash := migrationBytesHash(hold)
	if err = importer.ResolveMigrationHold(ctx, request, holdHash); err == nil {
		t.Fatal("released before transfer")
	}
	if _, err = journal.MigrationReadyReceipt(ctx, request); err == nil {
		t.Fatal("held target reported ready")
	}
	coordinator.journal, coordinator.assetRoot, coordinator.limits = journal, privateDir, limits
	progress, err := coordinator.AdvanceOwner(ctx, bound, migrationID, importID, now)
	if err != nil || progress.Stage != "activated" {
		t.Fatalf("migration advance: %+v %v", progress, err)
	}
	coordinator = &personalMigrationCoordinator{app: app, router: router, journal: journal, assetRoot: privateDir, limits: limits}
	progress, err = coordinator.AdvanceOwner(ctx, bound, migrationID, importID, now)
	if err != nil || progress.Stage != "activated" {
		t.Fatalf("migration restart: %+v %v", progress, err)
	}
	// The history alias must resolve the committed owner and actual target
	// receipt, including the hash of seal evidence (not the drain digest).
	app.applications = &applicationRuntime{migrations: coordinator, journal: journal}
	aliasIngress := &applicationIngress{host: app, router: router}
	aliasSource, aliasSeal, aliasErr := aliasIngress.legacyHistorySource(ctx, owner, 2)
	if aliasErr != nil || aliasSource.Source.ScopeID != bound.ScopeID || aliasSeal.SourceHash != seal.SourceHash {
		t.Fatalf("history source through committed migration: %v", aliasErr)
	}
	if _, _, aliasErr = aliasIngress.legacyHistorySource(ctx, owner, 3); aliasErr == nil {
		t.Fatal("history source accepted a different owner epoch")
	}
	wrongReceipt := receipt
	wrongReceipt.ProofHash = seal.ProofHash
	if legacyHistoryReceiptMatches(wrongReceipt, seal, bound.ScopeID) {
		t.Fatal("drain digest accepted as evidence hash")
	}

	if err = importer.ResolveMigrationHold(ctx, request, holdHash); err != nil {
		t.Fatalf("replayed resolution: %v", err)
	}
	router.SetGatewayCoverageVerifier(migrationCoverageDeniedFixture{})
	if err = importer.ResolveMigrationHold(ctx, request, holdHash); err == nil {
		t.Fatal("replayed resolution accepted invalid coverage")
	}
	router.SetGatewayCoverageVerifier(migrationCoverageFixture{})
	checkpoint, err = journal.LoadCheckpoint(ctx, owner, 2)
	if err != nil || len(checkpoint.Core.Holds) != 0 || !checkpoint.Core.MigrationRecoveryReleased {
		t.Fatal("verified barrier not durably released")
	}
	ready, err := journal.MigrationReadyReceipt(ctx, request)
	if err != nil || ready.Import.SourceHash != seal.SourceHash || ready.CheckpointRevision < 2 {
		t.Fatalf("target ready proof: %+v %v", ready, err)
	}
	processor, err := privatechat.NewProcessor(privatechat.New(nil, orchestrator.Config{}), journal, privateActionDecoder{}, privatechat.CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = processor.ApplyIngress(ctx, owner, 2, pending[0].Sequence); err != nil {
		t.Fatalf("recovered input: %v", err)
	}
	var sourceObservation work.IngressObservation
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		_, o, e := tx.PersonalMigrationIngressObservation("personal", "app", "event", 1)
		sourceObservation = o
		return e
	}); err != nil {
		t.Fatal(err)
	}
	replayedEvent, err := ingressmapper.Message(sourceObservation.Envelope, 1)
	if err != nil {
		t.Fatal(err)
	}
	replayed, duplicate, err := router.Admit(ctx, replayedEvent, nil)
	if err != nil || !duplicate || replayed.State != ingressowner.Historical {
		t.Fatal("historical SDK replay was re-admitted")
	}
	if err = store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutContent(work.NewContent("late", "personal", "text/plain", []byte("late"), now))
	}); err != nil {
		t.Fatal(err)
	}
	if err = app.verifyPersonalSourceSealLocal(ctx, seal, limits); err == nil {
		t.Fatal("late personal content did not invalidate seal")
	}
}
