package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/renderer"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type migrationFixtureVerifier struct {
	bundle MigrationImportBundle
	err    error
}

func (v *migrationFixtureVerifier) LoadVerifiedMigration(context.Context, MigrationImportRequest) (MigrationImportBundle, error) {
	return v.bundle, v.err
}
func (v *migrationFixtureVerifier) WithVerifiedMigrationHold(_ context.Context, _ MigrationImportRequest, _ MigrationImportReceipt, _ orchestrator.PrivateRecoveryHold, commit func(json.RawMessage) error) error {
	return commit(json.RawMessage(`{"fixture":true}`))
}
func migrationFixtureRecord(table, key, scope string, payload []byte, data []byte) MigrationImportedRecord {
	return MigrationImportedRecord{Table: table, Key: key, ScopeID: scope, Revision: 1, Payload: payload, PayloadHash: migrationSHA(payload), Data: data, DataHash: migrationSHA(data)}
}

func TestMigrationImportAtomicReplayAndOwnerIsolation(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	path := filepath.Join(dir, "private.db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	old := Owner{GatewayID: "g", SurfaceID: "existing", ChatID: "old-chat", UserID: "old-user", TenantID: "tenant", AppID: "app"}
	oldCore := orchestrator.PrivateCheckpoint{Version: 1, SurfaceID: old.SurfaceID, GatewayID: old.GatewayID, ChatID: old.ChatID, ActorUserID: old.UserID, RouteMode: state.RouteModeUnbound, PickerWatermarks: &orchestrator.PrivatePickerWatermarks{Path: 7}, NextQueueID: 9, NextImageID: 5}
	if err = j.CommitCheckpoint(ctx, Checkpoint{Owner: old, Generation: 1, Revision: 1, Core: oldCore}, 0, nil); err != nil {
		t.Fatal(err)
	}
	owner := Owner{GatewayID: "g", SurfaceID: "target", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	imagePath := filepath.Join(dir, "image")
	if err = os.WriteFile(imagePath, []byte("image"), 0600); err != nil {
		t.Fatal(err)
	}
	checkpoint := orchestrator.PrivateCheckpoint{Version: 1, SurfaceID: owner.SurfaceID, GatewayID: owner.GatewayID, ChatID: owner.ChatID, ActorUserID: owner.UserID, Backend: agentproto.BackendCodex, RouteMode: state.RouteModeUnbound, PickerWatermarks: &orchestrator.PrivatePickerWatermarks{}, NextQueueID: 1, NextImageID: 1, QueueItems: map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", SurfaceSessionID: owner.SurfaceID, ActorUserID: owner.UserID, SourceKind: state.QueueItemSourceUser, SourceMessageID: "m1", SourceMessageIDs: []string{"m1"}, Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "hello"}, {Type: agentproto.InputLocalImage, Path: imagePath, MIMEType: "image/png"}}, Status: state.QueueItemQueued}}, QueuedQueueItemIDs: []string{"queue-1"}, StagedImages: map[string]*state.StagedImageRecord{"img-1": {ImageID: "img-1", SurfaceSessionID: owner.SurfaceID, SourceMessageID: "m1", ActorUserID: owner.UserID, LocalPath: imagePath, MIMEType: "image/png", State: state.ImageBound}}, Holds: []orchestrator.PrivateRecoveryHold{{Kind: "migration_source_unverified", Frozen: json.RawMessage(`{"reason":"proof"}`)}}}
	action, err := json.Marshal(control.Action{Kind: control.ActionTextMessage, GatewayID: "g", SurfaceSessionID: "target", ChatID: "chat", ActorUserID: "user", MessageID: "m1", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	observation := migrationFixtureRecord("ingress_observations", "obs", "scope", []byte(`{"envelope":{"tenant":"tenant","app_id":"app","event_id":"e1","chat_id":"chat","chat_type":"p2p","message_id":"m1","sender_ids":{"open_id":"user"}}}`), nil)
	scopeRecord := migrationFixtureRecord("scopes", "scope", "scope", []byte(`{"id":"scope","kind":"personal"}`), nil)
	content := migrationFixtureRecord("content", "image-content", "scope", []byte(`{"scope_id":"scope"}`), []byte("image"))
	memory := migrationFixtureRecord("memories", "memory", "scope", []byte(`{"id":"memory","storage_scope_id":"scope","scope":{"kind":"personal"},"revision":3,"state":"valid","content_ref":"memory-content"}`), nil)
	memory.Revision = 3
	memoryContent := migrationFixtureRecord("content", "memory-content", "scope", []byte(`{"scope_id":"scope"}`), []byte("private memory"))
	manifestBytes := []byte(`{"source":{"scope_id":"scope"}}`)
	manifestHash := migrationSHA(manifestBytes)
	manifest := migrationFixtureRecord("migration_manifest", manifestHash, "scope", manifestBytes, nil)
	verifier := &migrationFixtureVerifier{bundle: MigrationImportBundle{SourceScope: "scope", SourceHash: manifestHash, ProofHash: migrationSHA([]byte("proof")), Checkpoint: checkpoint, Events: []MigrationImportedEvent{{Ingress: Ingress{Owner: owner, Generation: 1, EventID: "e1", Lane: "input", Payload: action, Sources: []string{"m1"}}, SourceScope: "scope", SourceKey: "obs", SourceHash: observation.PayloadHash, Applied: true}}, Records: []MigrationImportedRecord{observation, content, memory, memoryContent, scopeRecord, manifest}, Assets: []MigrationImportedAsset{{ContentID: "image-content", SourceMessageID: "m1", Path: imagePath, MediaType: "image/png", SHA256: migrationSHA([]byte("image")), Size: 5}}}}
	importer, err := NewMigrationImporter(j, verifier, dir)
	if err != nil {
		t.Fatal(err)
	}
	req := MigrationImportRequest{ImportID: "import", SourceHandle: "sealed-source", Owner: owner, Generation: 1}
	verifier.bundle.Records[0].PayloadHash = migrationSHA([]byte("forged"))
	if _, err = importer.Import(ctx, req); err == nil {
		t.Fatal("forged source accepted")
	}
	verifier.bundle.Records[0] = observation
	verifier.bundle.Records[1].Payload = json.RawMessage(`{"scope_id":"other"}`)
	verifier.bundle.Records[1].PayloadHash = migrationSHA(verifier.bundle.Records[1].Payload)
	if _, err = importer.Import(ctx, req); err == nil {
		t.Fatal("cross-scope source accepted")
	}
	verifier.bundle.Records[1] = content
	originalItem := *verifier.bundle.Checkpoint.QueueItems["queue-1"]
	brokenItem := originalItem
	brokenItem.SourceMessageID = "missing-source"
	brokenItem.SourceMessageIDs = []string{"missing-source"}
	verifier.bundle.Checkpoint.QueueItems["queue-1"] = &brokenItem
	if _, err = importer.Import(ctx, req); err == nil {
		t.Fatal("unproven checkpoint source accepted")
	}
	var partial int
	if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM ingress WHERE surface='target'`).Scan(&partial); err != nil || partial != 0 {
		t.Fatal("failed import left partial ingress")
	}
	verifier.bundle.Checkpoint.QueueItems["queue-1"] = &originalItem
	receipt, err := importer.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	again, err := importer.Import(ctx, req)
	if err != nil || again != receipt {
		t.Fatalf("replay failed: %v", err)
	}
	loaded, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Core.QueuedQueueItemIDs[0] != "queue-10" || loaded.Core.StagedImages["img-6"] == nil || loaded.Core.PickerWatermarks.Path != 7 {
		t.Fatal("global ID watermarks not preserved")
	}
	target := orchestrator.NewService(nil, orchestrator.Config{PrivateApplication: true}, renderer.NewPlanner())
	if err = target.RestorePrivateCheckpoint(loaded.Core); err != nil {
		t.Fatal(err)
	}
	if _, ok := target.PrivateRecoveryCheckpoint(owner.SurfaceID); !ok {
		t.Fatal("migration recovery hold missing")
	}
	var effects int
	if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_effect`).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("import dispatched an effect")
	}
	memories, err := j.ImportedPersonalMemory(ctx, owner, 1, "", 10)
	if err != nil || len(memories) != 1 || memories[0].Revision != 3 {
		t.Fatal("memory provenance missing")
	}
	_, memoryData, err := j.ReadImportedPersonalMemory(ctx, owner, 1, "memory", 3)
	if err != nil || string(memoryData) != "private memory" {
		t.Fatal("personal memory content unavailable")
	}
	wrongOwner := owner
	wrongOwner.UserID = "other"
	if _, _, err = j.ReadImportedPersonalMemory(ctx, wrongOwner, 1, "memory", 3); err == nil {
		t.Fatal("cross-owner memory read")
	}
	verifier.bundle.ProofHash = migrationSHA([]byte("different"))
	if _, err = importer.Import(ctx, req); !errors.Is(err, ErrJournalConflict) {
		t.Fatal("import identity mismatch accepted")
	}
	verifier.bundle.ProofHash = receipt.ProofHash
	other := req
	other.ImportID = "other"
	if _, err = importer.Import(ctx, other); !errors.Is(err, ErrJournalConflict) {
		t.Fatal("existing target overwritten")
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	saved, err := j.MigrationReceipt(ctx, owner, 1, "import")
	if err != nil || saved != receipt {
		t.Fatal("committed receipt did not survive restart")
	}
	resumed, err := NewMigrationImporter(j, verifier, dir)
	if err != nil {
		t.Fatal(err)
	}
	again, err = resumed.Import(ctx, req)
	if err != nil || again != receipt {
		t.Fatal("post-commit replay did not return receipt")
	}
	loaded, err = j.LoadCheckpoint(ctx, owner, 1)
	if err != nil || len(loaded.Core.Holds) != 1 {
		t.Fatal("missing held queue")
	}
	holdRaw, err := json.Marshal(loaded.Core.Holds[0])
	if err != nil {
		t.Fatal(err)
	}
	holdHash := migrationSHA(holdRaw)
	if _, err = j.db.ExecContext(ctx, `DELETE FROM private_applied WHERE seq=?`, receipt.Cursor); err != nil {
		t.Fatal(err)
	}
	if err = resumed.ResolveMigrationHold(ctx, req, holdHash); err == nil {
		t.Fatal("queue released without applied SDK source")
	}
	if _, err = j.db.ExecContext(ctx, `INSERT INTO private_applied(seq,revision) VALUES(?,1)`, receipt.Cursor); err != nil {
		t.Fatal(err)
	}
	_, err = j.db.ExecContext(ctx, `INSERT INTO dispatch(id,gateway,tenant,app_id,surface,chat,user_id,generation,lane,mode,backend,namespace,session,carrier,carrier_generation,command_id,state) VALUES('d','g','tenant','app','target','chat','user',1,'input','new','codex','ns','session','carrier',1,'cmd','unknown')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = resumed.ResolveMigrationHold(ctx, req, holdHash); !errors.Is(err, ErrRecoveryHeld) {
		t.Fatalf("unknown dispatch not held: %v", err)
	}
	if _, err = j.db.ExecContext(ctx, `DELETE FROM dispatch WHERE id='d'`); err != nil {
		t.Fatal(err)
	}
	if err = resumed.ResolveMigrationHold(ctx, req, holdHash); err != nil {
		t.Fatal(err)
	}
	ready, err := j.MigrationReadyReceipt(ctx, req)
	if err != nil || ready.Import != receipt {
		t.Fatalf("verified queued input not ready: %v", err)
	}
	suspendedJournal, e := OpenJournal(filepath.Join(dir, "suspended.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer suspendedJournal.Close()
	suspendedBundle := verifier.bundle
	suspendedBundle.SuspendQueue = true
	suspendedImporter, e := NewMigrationImporter(suspendedJournal, &migrationFixtureVerifier{bundle: suspendedBundle}, dir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = suspendedImporter.Import(ctx, req); e != nil {
		t.Fatal(e)
	}
	suspendedCheckpoint, e := suspendedJournal.LoadCheckpoint(ctx, owner, 1)
	if e != nil || len(suspendedCheckpoint.Core.QueuedQueueItemIDs) != 0 || len(suspendedCheckpoint.Core.QueueItems) != 0 {
		t.Fatal("unproven history queue remained dispatchable")
	}
	suspendedSummary, e := suspendedJournal.MigrationRecoverySummary(ctx, owner, 1)
	if e != nil || suspendedSummary.SuspendedInputs != 1 {
		t.Fatal("suspended source input not visible")
	}
	// Fixture-only plan tests the checkpoint/effect crash boundary; source
	// authority is tested separately by the daemon's sealed-source verifier.
	plan := MigrationResumeIntent{SourceHash: receipt.SourceHash, SourceScope: "scope", SourceFingerprint: migrationSHA([]byte("settings")), WorkerID: "worker", LaunchContentID: "launch", LaunchContentHash: migrationSHA([]byte("launch")), Backend: "codex", ConnectionIdentity: "account", ThreadID: "thread", CWD: "/workspace", Namespace: "history"}
	planRaw, _ := json.Marshal(plan)
	if _, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_resume(import_id,plan,plan_hash) VALUES(?,?,?)`, req.ImportID, planRaw, migrationSHA(planRaw)); err != nil {
		t.Fatal(err)
	}
	if _, required, e := j.MigrationResumeSelection(ctx, owner, 1, "unbound"); !required || !errors.Is(e, ErrDeferred) {
		t.Fatal("unbound resume did not block launch")
	}
	if _, err = j.MigrationReadyReceipt(ctx, req); !errors.Is(err, ErrRecoveryHeld) {
		t.Fatalf("unbound resume target became ready: %v", err)
	}
	processor, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	resumeEffects, err := processor.PrepareMigrationResume(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	effectID := ""
	for _, effect := range resumeEffects {
		event, _, e := DecodeStoredEffect(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandStartHeadless {
			effectID = effect.Effect.ID
		}
	}
	if effectID == "" {
		t.Fatal("missing real headless effect")
	}
	if got, required, e := j.MigrationResumeSelection(ctx, owner, 1, effectID); e != nil || !required || got != plan {
		t.Fatalf("resume plan binding: %v", e)
	}
	if _, err = j.MigrationReadyReceipt(ctx, req); err != nil {
		t.Fatalf("prepared headless not ready: %v", err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	processor, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = processor.RestoreMigrationHeadlessProjection(ctx, owner, 1, effectID); err != nil {
		t.Fatalf("resume after restart: %v", err)
	}
	if surface := processor.app.Service().Surface(owner.SurfaceID); surface == nil || surface.PendingHeadless == nil {
		t.Fatal("pending headless projection lost")
	}
}
