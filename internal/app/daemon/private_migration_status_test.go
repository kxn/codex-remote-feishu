package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type statusMigrationSource struct {
	bundle privatechat.MigrationImportBundle
}

func (s statusMigrationSource) LoadVerifiedMigration(context.Context, privatechat.MigrationImportRequest) (privatechat.MigrationImportBundle, error) {
	return s.bundle, nil
}
func statusMigrationRecord(table, key string, payload []byte) privatechat.MigrationImportedRecord {
	return privatechat.MigrationImportedRecord{Table: table, Key: key, ScopeID: "scope", Payload: payload, PayloadHash: migrationBytesHash(payload), DataHash: migrationBytesHash(nil)}
}
func TestPrivateStatusIncludesMigrationComponentsOnTypedAndCardPaths(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	j, err := privatechat.OpenJournal(filepath.Join(dir, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	owner := privatechat.Owner{GatewayID: "g", SurfaceID: "surface", ChatID: "chat", UserID: "user", TenantID: "tenant", AppID: "app"}
	inbox := []byte(`{"id":"input","scope_id":"scope","message_id":"message","state":"claimed"}`)
	message := []byte(`{"id":"message","scope_id":"scope","body_ref":"body"}`)
	body := []byte("保留的原始输入")
	bodyRecord := statusMigrationRecord("content", "body", []byte(`{"media_type":"text/plain"}`))
	bodyRecord.Data = body
	bodyRecord.DataHash = migrationBytesHash(body)
	publication := []byte(`{"scope_id":"scope","outbox_id":"publication"}`)
	execution := []byte(`{"scope_id":"scope","worker_id":"worker","evidence":{"ProcessScopesStopped":true,"NativeCommandsIdle":false}}`)
	scope := []byte(`{"id":"scope","kind":"personal"}`)
	manifest := []byte(`{"source":{"scope_id":"scope"}}`)
	hash := migrationBytesHash(manifest)
	bundle := privatechat.MigrationImportBundle{SourceScope: "scope", SourceHash: hash, ProofHash: migrationBytesHash([]byte("proof")), Checkpoint: orchestrator.PrivateCheckpoint{Version: 1, SurfaceID: owner.SurfaceID, GatewayID: owner.GatewayID, ChatID: owner.ChatID, ActorUserID: owner.UserID, PickerWatermarks: &orchestrator.PrivatePickerWatermarks{}, Holds: []orchestrator.PrivateRecoveryHold{{Kind: "migration_source_unverified", Frozen: json.RawMessage(`{}`)}}}, Records: []privatechat.MigrationImportedRecord{statusMigrationRecord("scopes", "scope", scope), statusMigrationRecord("migration_manifest", hash, manifest), statusMigrationRecord("inbox", "input", inbox), statusMigrationRecord("messages", "message", message), bodyRecord, statusMigrationRecord("personal_migration_publication_holds", "publication", publication), statusMigrationRecord("personal_migration_execution_holds", "worker", execution)}, InputHolds: []privatechat.MigrationInputHold{{InboxID: "input", MessageID: "message", Reason: "claimed_input_requires_native_reconciliation", PayloadHash: migrationBytesHash(inbox)}}}
	importer, err := privatechat.NewMigrationImporter(j, statusMigrationSource{bundle}, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = importer.Import(ctx, privatechat.MigrationImportRequest{ImportID: "import", SourceHandle: "sealed", Owner: owner, Generation: 1}); err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{})
	host.daemonLifecycleID = "test-lifecycle"
	app := privatechat.New(nil, orchestrator.Config{})
	host.service = app.Service()
	host.applications = &applicationRuntime{journal: j}
	p, err := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
	if err != nil {
		t.Fatal(err)
	}
	admit := func(id string, a control.Action) int64 {
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: 1, EventID: id, Lane: "control", Payload: raw})
		if e != nil {
			t.Fatal(e)
		}
		return seq
	}
	action := control.Action{Kind: control.ActionStatus, Text: "/status", GatewayID: owner.GatewayID, SurfaceSessionID: owner.SurfaceID, ChatID: owner.ChatID, ActorUserID: owner.UserID}
	effects, err := p.ApplyIngress(ctx, owner, 1, admit("typed", action))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, effect := range effects {
		event, _, e := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Snapshot != nil && event.Snapshot.HistoricalRecovery != nil {
			v := event.Snapshot.HistoricalRecovery
			found = v.UnconfirmedReplies == 1 && v.ClaimedInputs == 1 && v.UnresolvedExecutions == 1
		}
	}
	if !found {
		t.Fatal("typed status omitted exact owner recovery summary")
	}
	recovery := control.Action{Kind: control.ActionTextMessage, Text: "/recover 1", GatewayID: owner.GatewayID, SurfaceSessionID: owner.SurfaceID, ChatID: owner.ChatID, ActorUserID: owner.UserID}
	raw, _ := json.Marshal(recovery)
	recoverySeq, _, err := j.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: 1, EventID: "recover", Lane: "input", Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	recoveryEffects, err := p.ApplyIngress(ctx, owner, 1, recoverySeq)
	if err != nil {
		t.Fatal(err)
	}
	recovered := false
	for _, effect := range recoveryEffects {
		event, _, e := privatechat.DecodeStoredEffect(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Notice != nil && strings.Contains(event.Notice.Text, "保留的原始输入") {
			recovered = true
		}
	}
	if !recovered {
		t.Fatal("explicit historical input view omitted original text")
	}
	action.Text = ""
	action.CatalogFamilyID = control.FeishuCommandStatus
	action.CatalogBackend = agentproto.BackendCodex
	action.CatalogVariantID = control.FeishuCommandVariantIDForContext(control.FeishuCommandStatus, control.CatalogContext{Backend: agentproto.BackendCodex})
	action.Inbound = &control.ActionInboundMeta{CardCallback: true, OpenMessageID: "card", CardDaemonLifecycleID: host.daemonLifecycleID}
	seq := admit("callback", action)
	if _, err = p.ApplyIngress(ctx, owner, 1, seq); err != nil {
		t.Fatal(err)
	}
	card, err := p.ReadCommittedCardResponse(ctx, owner, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(card.Response), "历史记录待核对") || !strings.Contains(string(card.Response), "历史答复") || !strings.Contains(string(card.Response), "历史输入") || !strings.Contains(string(card.Response), "历史执行") {
		t.Fatalf("same-card status omitted recovery summary: %s", card.Response)
	}
}
