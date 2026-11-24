package privatechat

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"encoding/json"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestHistoricalInputsOwnerEpochAndIntegrity(t *testing.T) {
	ctx := t.Context()
	j, err := OpenJournal(filepath.Join(t.TempDir(), "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	o := Owner{TenantID: "tenant", AppID: "app", GatewayID: "gateway", SurfaceID: "surface", ChatID: "chat", UserID: "user"}
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES('i','s','hash','proof',?,?,?,?,?,?,1,'{}')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID)
	if err != nil {
		t.Fatal(err)
	}
	inbox := []byte(`{"id":"inbox","message_id":"message","scope_id":"s"}`)
	msg := []byte(`{"id":"message","scope_id":"s","body_ref":"body"}`)
	body := []byte("原始输入")
	for _, r := range []struct {
		table, key    string
		payload, data []byte
	}{{"inbox", "inbox", inbox, nil}, {"messages", "message", msg, nil}, {"content", "body", []byte(`{"media_type":"text/plain"}`), body}} {
		_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_records(import_id,source_table,source_key,source_scope,source_order,source_revision,payload,payload_hash,data,data_hash) VALUES('i',?,?,'s',1,1,?,?,?,?)`, r.table, r.key, r.payload, migrationSHA(r.payload), r.data, migrationSHA(r.data))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('i','inbox','message','claimed_input_requires_native_reconciliation',?)`, migrationSHA(inbox))
	if err != nil {
		t.Fatal(err)
	}
	got, err := j.HistoricalInputs(ctx, o, 1, 10)
	if err != nil || len(got) != 1 || got[0].Text != string(body) {
		t.Fatalf("read: %v %v", got, err)
	}
	other := o
	other.UserID = "other"
	got, err = j.HistoricalInputs(ctx, other, 1, 10)
	if err != nil || len(got) != 0 {
		t.Fatal("cross-owner read")
	}
	got, err = j.HistoricalInputs(ctx, o, 2, 10)
	if err != nil || len(got) != 0 {
		t.Fatal("cross-generation read")
	}
	_, err = j.db.ExecContext(ctx, `UPDATE private_migration_records SET data=? WHERE import_id='i' AND source_table='content'`, []byte("changed"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.HistoricalInputs(ctx, o, 1, 10); err == nil {
		t.Fatal("tampered content accepted")
	}
}

func TestHistoricalConfirmationCommitsOnceAndDoesNotReuseOldSession(t *testing.T) {
	ctx := t.Context()
	j, p, lease, o, binding := effectFixture(t)
	_, err := j.db.ExecContext(ctx, `INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES('i','source','hash','proof',?,?,?,?,?,?,1,'{}')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID)
	if err != nil {
		t.Fatal(err)
	}
	inbox := []byte(`{"id":"inbox","message_id":"message","scope_id":"source","recipient_context_id":"front"}`)
	msg, _ := json.Marshal(map[string]any{"id": "message", "scope_id": "source", "body_ref": "body", "source": map[string]string{"kind": "principal", "id": "principal"}, "platform_key": map[string]string{"tenant": o.TenantID, "chat_id": o.ChatID, "message_id": "source-message"}})
	body := []byte("原始需求")
	for _, r := range []struct {
		table, key    string
		payload, data []byte
	}{{"inbox", "inbox", inbox, nil}, {"messages", "message", msg, nil}, {"scopes", "source", []byte(`{"principal_id":"principal"}`), nil}, {"private_navigation", "navigation", []byte(`{"mode":"personal","front_context_id":"front"}`), nil}, {"content", "body", []byte(`{"media_type":"text/plain"}`), body}, {"workers", "worker", []byte(`{"native_session_id":"old-thread"}`), nil}} {
		_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_records(import_id,source_table,source_key,source_scope,source_order,source_revision,payload,payload_hash,data,data_hash) VALUES('i',?,?,'source',1,1,?,?,?,?)`, r.table, r.key, r.payload, migrationSHA(r.payload), r.data, migrationSHA(r.data))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('i','inbox','message','claimed_input_requires_native_reconciliation',?)`, migrationSHA(inbox))
	if err != nil {
		t.Fatal(err)
	}
	suspended := orchestrator.PrivateCheckpoint{SurfaceID: o.SurfaceID, GatewayID: o.GatewayID, ChatID: o.ChatID, ActorUserID: o.UserID, QueuedQueueItemIDs: []string{"queue-1"}, QueueItems: map[string]*state.QueueItemRecord{"queue-1": {ID: "queue-1", ActorUserID: o.UserID, Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: "排队输入"}}}}}
	frozen, _ := json.Marshal(suspended)
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_suspended_queue(import_id,item_count,payload,payload_hash) VALUES('i',1,?,?)`, frozen, migrationSHA(frozen))
	if err != nil {
		t.Fatal(err)
	}
	action := privateAction(o, control.ActionTextMessage)
	action.Text = "/recover confirm 1"
	surface := p.app.Service().Surface(o.SurfaceID)
	selected := surface.SelectedThreadID
	surface.SelectedThreadID = "old-thread"
	old := action
	old.MessageID = "old-confirmation"
	_, oldEffects := applyEffectAction(t, j, p, o, "old-confirmation", "input", []string{"old-confirmation"}, old)
	for _, effect := range oldEffects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			t.Fatal("old source session accepted")
		}
	}
	surface.SelectedThreadID = selected
	newAction := privateAction(o, control.ActionNewThread)
	newAction.Text = "/new"
	applyEffectAction(t, j, p, o, "new-thread", "control", nil, newAction)
	action.MessageID = "confirmation"
	seq, effects := applyEffectAction(t, j, p, o, "confirmation", "input", []string{"confirmation"}, action)
	var command bool
	var effectID string
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = true
			effectID = effect.Effect.ID
		}
	}
	if !command {
		t.Fatal("confirmed text did not reach normal queue dispatch")
	}
	dispatch, nativeCommand, err := lease.PrepareEffectDispatch(ctx, effectID, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	root, cwd := t.TempDir(), t.TempDir()
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: binding.Owner, CWD: cwd, JournalPath: filepath.Join(root, "wrapper.db")}
	wrapper, err := runtimejournal.Open(ctx, snapshot.JournalPath, root, cwd, binding.Owner.CarrierIncarnation)
	if err != nil {
		t.Fatal(err)
	}
	defer wrapper.Close()
	if err = wrapper.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err = wrapper.PreparePrivateCommand(nativeCommand); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"native-1","method":"turn/start","params":{}}`)
	frameID, err := wrapper.PrepareFrame(nativeCommand.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = wrapper.StartFrame(frameID, frame); err != nil {
		t.Fatal(err)
	}
	ref := persistNative(t, ctx, lease, binding, 1, agentproto.Event{Kind: agentproto.EventNativeTurnBindingObserved, CommandID: nativeCommand.CommandID, RequestID: "native-1", ThreadID: "created-thread", TurnID: "turn", Status: "accepted"})
	if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snapshot, root}, nativeCommand.CommandID); err != nil {
		t.Fatal(err)
	}
	if err = lease.ResolveNativeDispatch(ctx, ref); err != nil {
		t.Fatal(err)
	}
	proof, err := j.historicalNewSessionProof(ctx, o, 1, "instance", "created-thread", agentproto.BackendCodex)
	if err != nil || proof == nil {
		t.Fatalf("native receipt did not prove new session: %v", err)
	}
	wrong, err := j.historicalNewSessionProof(ctx, o, 1, "instance", "old-thread", agentproto.BackendCodex)
	if err != nil || wrong != nil {
		t.Fatal("foreign old session accepted")
	}
	wrong, err = j.historicalNewSessionProof(ctx, o, 1, "instance", "created-thread", agentproto.BackendClaude)
	if err != nil || wrong != nil {
		t.Fatal("foreign backend accepted")
	}
	foreign := o
	foreign.UserID = "someone-else"
	wrong, err = j.historicalNewSessionProof(ctx, foreign, 1, "instance", "created-thread", agentproto.BackendCodex)
	if err != nil || wrong != nil {
		t.Fatal("foreign owner accepted")
	}
	if _, err = p.ApplyNative(ctx, lease, ref, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	started := persistNative(t, ctx, lease, binding, 2, agentproto.Event{Kind: agentproto.EventTurnStarted, CommandID: nativeCommand.CommandID, ThreadID: "created-thread", TurnID: "turn", Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown}})
	if _, err = p.ApplyNative(ctx, lease, started, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	completed := persistNative(t, ctx, lease, binding, 3, agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: nativeCommand.CommandID, ThreadID: "created-thread", TurnID: "turn", Status: "completed"})
	if _, err = p.ApplyNative(ctx, lease, completed, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	instance := p.app.Service().Instance("instance")
	instance.Threads["created-thread"] = &state.ThreadRecord{ThreadID: "created-thread", CWD: "/tmp/workspace"}
	use := privateAction(o, control.ActionUseThread)
	use.ThreadID = "created-thread"
	applyEffectAction(t, j, p, o, "use-created", "control", nil, use)
	surface = p.app.Service().Surface(o.SurfaceID)
	if surface.SelectedThreadID != "created-thread" {
		t.Fatalf("new thread not selected: %q", surface.SelectedThreadID)
	}
	queued := action
	queued.Text = "/recover confirm 2"
	queued.MessageID = "queued-confirmation"
	applyEffectAction(t, j, p, o, "queued-confirmation", "input", []string{"queued-confirmation"}, queued)
	var queuedCount int
	if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovered_inputs WHERE inbox_id='queue:queue-1'`).Scan(&queuedCount); err != nil || queuedCount != 1 {
		t.Fatalf("same proven session did not accept suspended input: %v", err)
	}
	var proofCount int
	if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovery_session_proofs WHERE import_id='i' AND inbox_id='queue:queue-1'`).Scan(&proofCount); err != nil || proofCount != 1 {
		t.Fatal("session receipt was not durably bound to recovery")
	}
	items, err := j.HistoricalInputs(ctx, o, 1, 100)
	if err != nil || len(items) != 2 || items[1].Kind != "queue" || items[1].Text != "排队输入" {
		t.Fatalf("suspended queue unavailable: %#v %v", items, err)
	}
	second, e := j.HistoricalInputPage(ctx, o, 1, 2, 1)
	if e != nil || len(second) != 1 || second[0].Kind != "queue" {
		t.Fatal("pagination crossed source queue incorrectly")
	}
	at, found, e := j.HistoricalInputAt(ctx, o, 1, 2)
	if e != nil || !found || at.Kind != "queue" {
		t.Fatal("stable input index lost")
	}
	var count int
	if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovered_inputs WHERE ingress_seq=?`, seq).Scan(&count); err != nil || count != 1 {
		t.Fatalf("missing atomic marker %v", err)
	}
	again := action
	again.MessageID = "confirmation-again"
	_, effects = applyEffectAction(t, j, p, o, "confirmation-again", "input", []string{"confirmation-again"}, again)
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			t.Fatal("second confirmation dispatched again")
		}
	}
	if err = j.db.QueryRowContext(ctx, `SELECT count(*) FROM private_migration_recovered_inputs`).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate marker")
	}
	var seqNo int
	var databaseName, path string
	if err = j.db.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&seqNo, &databaseName, &path); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restoredProof, e := reopened.historicalNewSessionProof(ctx, o, 1, "instance", "created-thread", agentproto.BackendCodex)
	if e != nil || restoredProof == nil || restoredProof.Hash != proof.Hash {
		t.Fatal("restart lost exact native session proof")
	}
	restarted, err := NewProcessor(New(nil, orchestrator.Config{}), reopened, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	again.MessageID = "after-restart"
	_, effects = applyEffectAction(t, reopened, restarted, o, "after-restart", "input", []string{"after-restart"}, again)
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			t.Fatal("restart dispatched recovered input again")
		}
	}
}

func TestHistoricalAssetRequiresExactBytesAndPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset")
	body := []byte("asset bytes")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyHistoricalAsset(path, migrationSHA(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyHistoricalAsset(path, migrationSHA(body), int64(len(body))); err == nil {
		t.Fatal("changed asset accepted")
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if err := verifyHistoricalAsset(path, migrationSHA(body), int64(len(body))); err == nil {
		t.Fatal("public asset accepted")
	}
}

func TestHistoricalRecoveryServerRejectsClaimedControlAndSlash(t *testing.T) {
	ctx := t.Context()
	j, p, _, o, _ := effectFixture(t)
	_, err := j.db.ExecContext(ctx, `INSERT INTO private_migration_imports(import_id,source_scope,source_hash,proof_hash,tenant,app_id,gateway,surface,chat,user_id,generation,receipt) VALUES('guard','source','hash','proof',?,?,?,?,?,?,1,'{}')`, o.TenantID, o.AppID, o.GatewayID, o.SurfaceID, o.ChatID, o.UserID)
	if err != nil {
		t.Fatal(err)
	}
	add := func(table, key string, order int, payload, data []byte) {
		t.Helper()
		_, e := j.db.ExecContext(ctx, `INSERT INTO private_migration_records(import_id,source_table,source_key,source_scope,source_order,source_revision,payload,payload_hash,data,data_hash) VALUES('guard',?,?, 'source',?,1,?,?,?,?)`, table, key, order, payload, migrationSHA(payload), data, migrationSHA(data))
		if e != nil {
			t.Fatal(e)
		}
	}
	add("scopes", "source", 1, []byte(`{"principal_id":"principal"}`), nil)
	add("private_navigation", "source", 1, []byte(`{"mode":"personal","front_context_id":"front"}`), nil)
	for i, v := range []struct{ id, text, extra string }{{"control", "旧控制内容", `,"control_of_input_id":"input-old"`}, {"slash", "/stop", ""}} {
		inbox := []byte(fmt.Sprintf(`{"id":%q,"message_id":%q,"scope_id":"source","recipient_context_id":"front"%s}`, v.id, v.id, v.extra))
		msg, _ := json.Marshal(map[string]any{"id": v.id, "scope_id": "source", "body_ref": v.id, "source": map[string]string{"kind": "principal", "id": "principal"}, "platform_key": map[string]string{"tenant": o.TenantID, "chat_id": o.ChatID, "message_id": v.id}})
		add("inbox", v.id, i+1, inbox, nil)
		add("messages", v.id, i+1, msg, nil)
		add("content", v.id, i+1, []byte(`{"media_type":"text/plain"}`), []byte(v.text))
		_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('guard',?,?,'claimed_input_requires_native_reconciliation',?)`, v.id, v.id, migrationSHA(inbox))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = j.db.ExecContext(ctx, `INSERT INTO private_migration_input_holds(import_id,inbox_id,message_id,reason,payload_hash) VALUES('guard','internal','internal','internal_component_nonreplayable','unused')`)
	if err != nil {
		t.Fatal(err)
	}
	visible, err := j.HistoricalInputs(ctx, o, 1, 10)
	if err != nil || len(visible) != 2 {
		t.Fatalf("internal component entered recoverable list: %v %v", visible, err)
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	surface.RouteMode = state.RouteModeNewThreadReady
	surface.SelectedThreadID = ""
	for _, n := range []string{"1", "2"} {
		action := privateAction(o, control.ActionTextMessage)
		action.Text = "/recover confirm " + n
		got, ref, e := p.prepareHistoricalInput(ctx, o, 1, action)
		if e != nil || ref != nil || got.Text != "/recover unavailable" {
			t.Fatalf("unsafe historical recovery %s: %q %+v %v", n, got.Text, ref, e)
		}
	}
}
