package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workhost"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/app/ingressowner"
	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/config"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

type migrationCoverageFixture struct{}

func (migrationCoverageFixture) VerifyGatewayCoverage(_ context.Context, c ingressowner.GatewayCoverage, proof []byte) error {
	if c.Tenant != "tenant" || c.AppID != "app" || c.GatewayID != "gateway" || c.BotGeneration != 1 || string(proof) != "fixture-only" {
		return errors.New("invalid fixture coverage")
	}
	return nil
}

func TestPersonalExportMapsPrivateCheckpointAndHoldsMedia(t *testing.T) {
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
	if err = app.syncWorkRoles(ctx, store); err != nil {
		t.Fatal(err)
	}
	roleID := teamID("role", "gateway")
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: roleID, BotGeneration: 1}
		raw := work.RawIngressEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "event", Tenant: "tenant", ChatID: "chat", ChatType: "p2p", MessageID: "sdk", SenderType: "user", SenderIDs: work.SenderIDs{OpenID: "human"}, MessageType: "text", RawContent: `{"text":"hello"}`, ReceivedAt: now}
		ingress, _, e := tx.AcceptRawIngress(raw, gateway)
		if e != nil {
			return e
		}
		principal, _, e := tx.ResolvePrincipal(work.IdentityEvidence{Tenant: "tenant", AppID: "app", Generation: 1, IDs: raw.SenderIDs, ObservationRef: ingress.ID})
		if e != nil {
			return e
		}
		if e = tx.PutScope(work.WorkScope{ID: "personal", Kind: work.ScopePersonal, RoleID: roleID, PrincipalID: principal.ID, Revision: 1}, 0); e != nil {
			return e
		}
		nav, e := tx.EnsurePrivateNavigation("personal", ingress.ID, now)
		if e != nil {
			return e
		}
		if e = tx.FreezePrivateIngressNavigation(ingress.ID, nav); e != nil {
			return e
		}
		worker := work.WorkerSession{ID: "worker", ContextID: nav.FrontContextID, ScopeID: "personal", RoleID: roleID, Backend: work.BackendClaude, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, NativeNamespace: "exact-namespace", NativeSessionID: "native-thread", Revision: 1}
		if e = tx.PutWorker(worker, 0); e != nil {
			return e
		}
		launch := workhost.LaunchRecord{WorkerID: worker.ID, ScopeID: worker.ScopeID, Generation: 1, NativeHistoryNamespace: worker.NativeNamespace, ConnectionIdentity: "account-ref", Snapshot: agentproto.RuntimeLaunchSnapshot{CWD: "/workspace/exact"}}
		rawLaunch, e := json.Marshal(launch)
		if e != nil {
			return e
		}
		body := work.NewContent("launch-body", "personal", "application/json", rawLaunch, now)
		if e = tx.PutContent(body); e != nil {
			return e
		}
		operation := work.Operation{ID: "launch", ScopeID: "personal", Actor: work.Actor{Kind: work.ActorSystem, ID: "host"}, Kind: "worker.launch", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: "launch", RequestRef: body.ID, RequestHash: body.SHA256, State: work.OperationUnknown, Revision: 1}
		if _, _, e = tx.PutOperation(operation, 0); e != nil {
			return e
		}
		sendBody := work.NewContent("send-body", "personal", "text/plain", []byte("answer"), now)
		if e = tx.PutContent(sendBody); e != nil {
			return e
		}
		send := work.Operation{ID: "send", ScopeID: "personal", Actor: work.Actor{Kind: work.ActorSystem, ID: "output"}, Kind: "feishu.publish", TargetID: "chat", TargetGeneration: 1, IdempotencyKey: "send", RequestRef: sendBody.ID, RequestHash: sendBody.SHA256, State: work.OperationUnknown, Revision: 1}
		if _, _, e = tx.PutOperation(send, 0); e != nil {
			return e
		}
		if e = tx.PutOutbox(work.Outbox{ID: "uncertain-send", ScopeID: "personal", OperationID: "send", Target: "chat", EventID: "send", PayloadRef: sendBody.ID, State: work.OutboxUnknown, Revision: 1}, 0); e != nil {
			return e
		}
		for i, id := range []string{"text", "image"} {
			body := work.NewContent(id+"-body", "personal", "text/plain", []byte("message "+id), now)
			if e = tx.PutContent(body); e != nil {
				return e
			}
			m := work.Message{ID: id, ScopeID: "personal", ConversationID: "conversation", Source: work.Actor{Kind: work.ActorPrincipal, ID: principal.ID}, PlatformKey: &work.MessageKey{Tenant: "tenant", ChatID: "chat", MessageID: id + "-sdk"}, BodyRef: body.ID, Revision: 1, CreatedAt: now}
			if id == "image" {
				image := work.NewContent("image-content", "personal", "image/png", []byte("image-bytes"), now)
				if e = tx.PutContent(image); e != nil {
					return e
				}
				m.Attachments = []work.MessageAttachment{{ContentRef: image.ID, SHA256: image.SHA256, Size: int64(len(image.Data)), MediaType: image.MediaType, SourceMessageID: "image-sdk", Kind: "image"}}
			}
			in := work.Inbox{ID: id + "-inbox", ScopeID: "personal", RecipientRoleID: roleID, RecipientContextID: nav.FrontContextID, EventID: id + "-event", MessageID: id, State: work.InboxPending, AvailableAt: now.Add(time.Duration(i) * time.Second), Revision: 1}
			if _, _, e = tx.AcceptMessage(m, []work.Inbox{in}); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := app.capturePersonalMigrationSource(ctx, store, "personal", workstore.PersonalExportLimits{MaxRows: 1000, MaxBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	owner := privatechat.Owner{GatewayID: "gateway", SurfaceID: "private-surface", TenantID: "tenant", AppID: "app", ChatID: "chat", UserID: "human"}
	report, err := mapPersonalMigrationSource(source, owner)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Checkpoint.QueuedQueueItemIDs) != 1 || report.Checkpoint.QueueItems["queue-1"].Inputs[0].Text != "message text" {
		t.Fatal("text queue was not mapped")
	}
	if len(report.History) != 1 || report.History[0].ThreadID != "native-thread" || report.History[0].Namespace != "exact-namespace" || report.History[0].CWD != "/workspace/exact" || report.Checkpoint.SelectedThreadID != "" {
		t.Fatal("native history evidence was lost or prematurely attached")
	}
	if report.History[0].ConnectionIdentity != "account-ref" || report.Checkpoint.PromptOverride.AccessMode != agentproto.AccessModeFullAccess {
		t.Fatal("frozen account reference or default full access lost")
	}
	verifier := teamMigrationOwnerVerifier{store: store}
	binding, proof, err := verifier.historicalBinding(ctx, "personal", "app", "event", 1)
	if err != nil {
		t.Fatal(err)
	}
	router, err := ingressowner.Open(filepath.Join(t.TempDir(), "neutral.db"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer router.Close()
	router.SetOwnerScopeBindingVerifier(verifier)
	bound, err := router.ImportOwnerScopeBinding(ctx, binding, proof)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Owner.ID == "" || bound.Owner.ID == binding.ScopeID {
		t.Fatal("opaque owner was not minted")
	}
	if _, err = router.BeginBoundOwnerMigration(ctx, bound, "migration"); err == nil {
		t.Fatal("missing gateway coverage accepted")
	}
	router.SetGatewayCoverageVerifier(migrationCoverageFixture{})
	if err = router.RecordGatewayCoverage(ctx, ingressowner.GatewayCoverage{Tenant: "tenant", AppID: "app", GatewayID: "gateway", BotGeneration: 1, RouterID: router.RouterID(), Epoch: 1, CutoverID: "fixture-cutover"}, []byte("fixture-only")); err != nil {
		t.Fatal(err)
	}
	forgedBinding := binding
	forgedBinding.OpenID = "other"
	if err = verifier.VerifyOwnerScopeBinding(ctx, forgedBinding, nil, proof); err == nil {
		t.Fatal("source verifier accepted forged principal")
	}
	forgedProof := []byte(`{"ingress_id":"forged","revision":1,"fact_hash":"forged"}`)
	if err = verifier.VerifyOwnerScopeBinding(ctx, binding, nil, forgedProof); err == nil {
		t.Fatal("source verifier accepted forged receipt")
	}
	drain, err := router.BeginBoundOwnerMigration(ctx, bound, "migration")
	if err != nil {
		t.Fatal(err)
	}
	if err = router.VerifyBoundOwnerMigrationDrain(ctx, bound, "migration", drain); err != nil {
		t.Fatal(err)
	}
	forged := bound
	forged.OpenID = "other"
	if err = router.VerifyBoundOwnerMigrationDrain(ctx, forged, "migration", drain); err == nil {
		t.Fatal("forged identity accepted")
	}
	if err = store.Within(ctx, func(tx *workstore.Tx) error {
		gateway := work.IngressGateway{GatewayRef: "gateway", AppID: "app", RoleID: roleID, BotGeneration: 1}
		menu, _, e := tx.AcceptMenuInteraction(work.MenuInteractionEnvelope{GatewayRef: "gateway", AppID: "app", EventID: "access-menu", Tenant: "tenant", SenderIDs: work.SenderIDs{OpenID: "human"}, EventKey: "settings", ReceivedAt: now}, gateway)
		if e != nil {
			return e
		}
		src := workstore.PrivateActionSource{PersonalScopeID: "personal", MenuInteractionID: menu.ID}
		scope, proof, e := tx.ValidatePrivateAction(src)
		if e != nil {
			return e
		}
		nav, e := tx.GetPrivateNavigation(scope.ID)
		if e != nil {
			return e
		}
		access := agentproto.AccessModeConfirm
		return tx.PutContextPreferences(work.ContextPreferences{ContextID: nav.FrontContextID, ScopeID: scope.ID, AccessMode: &access, Source: proof, UpdatedBy: work.Actor{Kind: work.ActorPrincipal, ID: scope.PrincipalID}, Revision: 1, UpdatedAt: now}, 0, src)
	}); err != nil {
		t.Fatal(err)
	}
	overriddenSource, err := app.capturePersonalMigrationSource(ctx, store, "personal", workstore.PersonalExportLimits{MaxRows: 1000, MaxBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	overridden, err := mapPersonalMigrationSource(overriddenSource, owner)
	if err != nil {
		t.Fatal(err)
	}
	if overridden.Checkpoint.PromptOverride.AccessMode != agentproto.AccessModeConfirm || overridden.History[0].ConnectionIdentity != "account-ref" {
		t.Fatal("explicit context override or account identity lost")
	}
	found := false
	for _, b := range report.Blockers {
		if b.Key == "image-inbox" && b.Reason == "media_requires_target_managed_file_import" {
			found = true
		}
	}
	if !found || len(report.Checkpoint.Holds) == 0 {
		t.Fatal("image was not held")
	}
	uncertain := false
	for _, b := range report.Blockers {
		if b.Table == "outbox" && b.Key == "uncertain-send" && b.Reason == "requires_recovery_reconciliation" {
			uncertain = true
		}
	}
	if !uncertain {
		t.Fatal("unknown send was not held")
	}
	target := privatechat.New(nil, orchestrator.Config{})
	if err = target.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	if err = target.Service().RestorePrivateCheckpoint(report.Checkpoint); err != nil {
		t.Fatal(err)
	}
	if _, ok := target.Service().PrivateRecoveryCheckpoint(owner.SurfaceID); !ok {
		t.Fatal("unresolved source did not pause target")
	}
	assetRoot := t.TempDir()
	withAssets, assets, err := mapPersonalMigrationSourceWithAssets(source, owner, assetRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 || assets[0].SourceMessageID != "image-sdk" || len(withAssets.Checkpoint.QueuedQueueItemIDs) != 2 {
		t.Fatal("image source or queue missing")
	}
	info, err := os.Stat(assets[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(assets[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 || string(bytes) != "image-bytes" {
		t.Fatal("asset content or mode mismatch")
	}
	if input := withAssets.Checkpoint.QueueItems["queue-2"].Inputs; len(input) != 2 || input[1].Type != agentproto.InputLocalImage || input[1].Path != assets[0].Path {
		t.Fatal("checkpoint image not mapped")
	}
	if err = os.WriteFile(assets[0].Path, []byte("image-bytEs"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = mapPersonalMigrationSourceWithAssets(source, owner, assetRoot); err == nil {
		t.Fatal("tampered existing asset accepted")
	}
	owner.UserID = "other"
	if _, err = mapPersonalMigrationSource(source, owner); err == nil {
		t.Fatal("unverified owner accepted")
	}
}
