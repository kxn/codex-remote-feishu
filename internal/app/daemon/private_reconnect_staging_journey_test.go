package daemon

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/privatechat"
	"github.com/kxn/codex-remote-feishu/internal/app/privatestaging"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateOldStagedPendingReconnectJourney(t *testing.T) {
	for _, scenario := range []string{"success", "changed_route", "damaged_receipt"} {
		t.Run(scenario, func(t *testing.T) { testOldStagedPendingReconnect(t, scenario) })
	}
}

func testOldStagedPendingReconnect(t *testing.T, scenario string) {
	ctx := context.Background()
	workspace := t.TempDir()
	base := t.TempDir()
	path := filepath.Join(t.TempDir(), "journal.db")
	staging, err := privatestaging.Open(base, privatestaging.Owner{Tenant: "tenant", AppID: "bot", Gateway: "g", EventID: "text-event", MessageID: "message"})
	if err != nil {
		t.Fatal(err)
	}
	source, err := staging.Create(strings.NewReader("old attachment"), "")
	if err != nil {
		t.Fatal(err)
	}
	host := New(":0", ":0", &privateCleanupTestGateway{base: base}, agentproto.ServerIdentity{})
	owner := privatechat.Owner{GatewayID: "g", SurfaceID: "s", ChatID: "c", UserID: "u", TenantID: "tenant", AppID: "bot"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "carrier", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	hello := agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: "carrier", Instance: agentproto.InstanceHello{InstanceID: "instance", Backend: agentproto.BackendCodex, WorkspaceRoot: workspace, WorkspaceKey: workspace, Source: "headless", Managed: true}}
	open := func() (*privatechat.Journal, *privatechat.Processor, *privatechat.NativeLease, *privatechat.App) {
		j, e := privatechat.OpenJournal(path)
		if e != nil {
			t.Fatal(e)
		}
		app := privatechat.New(nil, orchestrator.Config{})
		host.service = app.Service()
		p, e := privatechat.NewProcessor(app, j, privateHostTestDecoder{}, privateHostReducer{host})
		if e != nil {
			t.Fatal(e)
		}
		l, e := j.OpenNativeLease(ctx, privateSettingsNativeAuthority{}, native, owner, 1)
		if e != nil {
			t.Fatal(e)
		}
		return j, p, l, app
	}
	j, p, l, app := open()
	if err = p.RegisterNativeHello(ctx, l, hello); err != nil {
		t.Fatal(err)
	}
	inst := app.Service().Instance("instance")
	inst.CapabilitiesDeclared = false
	inst.ObservedFocusedThreadID = "thread"
	inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: workspace}
	admit := func(j *privatechat.Journal, p *privatechat.Processor, id, lane string, a control.Action, sources []string) []privatechat.EffectRecord {
		raw, _ := json.Marshal(a)
		seq, _, e := j.Admit(ctx, privatechat.Ingress{Owner: owner, Generation: 1, EventID: id, Lane: lane, Payload: raw, Sources: sources})
		if e != nil {
			t.Fatal(e)
		}
		out, e := p.ApplyIngress(ctx, owner, 1, seq)
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	action := control.Action{GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u", Kind: control.ActionAttachInstance, InstanceID: "instance"}
	attachEffects := admit(j, p, "attach", "control", action, nil)
	for _, e := range attachEffects {
		event, _ := privatechat.DecodeEffectEvent(e.Effect.Body)
		if event.Notice != nil {
			t.Logf("attach notice %s %s", event.Notice.Code, event.Notice.Text)
		}
	}
	action.Kind = control.ActionUseThread
	action.ThreadID = "thread"
	admit(j, p, "use", "control", action, nil)
	action.Kind = control.ActionTextMessage
	action.ThreadID = ""
	action.MessageID = "message"
	action.Text = "old prompt"
	action.Files = []control.ActionFileAttachment{{SourceMessageID: "message", LocalPath: source, FileName: "ref.txt"}}
	effects := admit(j, p, "text-event", "input", action, []string{"message"})
	stageID := ""
	for _, e := range effects {
		event, x := privatechat.DecodeEffectEvent(e.Effect.Body)
		if x != nil {
			t.Fatal(x)
		}
		if event.DaemonCommand != nil && event.DaemonCommand.Kind == control.DaemonCommandPrivateStageInput {
			stageID = e.Effect.ID
		}
	}
	if stageID == "" {
		for _, e := range effects {
			event, _ := privatechat.DecodeEffectEvent(e.Effect.Body)
			if event.Notice != nil {
				t.Logf("notice=%s text=%s", event.Notice.Code, event.Notice.Text)
			}
		}
		t.Fatal("missing stage effect")
	}
	delete(inst.Threads, "thread")
	stageEffects, err := runPrivateStageInput(ctx, host, p, j, owner, 1, stageID)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range stageEffects {
		event, _ := privatechat.DecodeEffectEvent(e.Effect.Body)
		if event.Notice != nil {
			t.Logf("stage notice %s %s", event.Notice.Code, event.Notice.Text)
		}
	}
	// Construct the legacy checkpoint shape from the real confirmed stage
	// receipt; no recovery gate or journal state is bypassed.
	stageRecord, err := j.LoadEffect(ctx, owner, 1, stageID)
	if err != nil {
		t.Fatal(err)
	}
	var stageReceipt privateStageReceipt
	if err = json.Unmarshal(stageRecord.Receipt, &stageReceipt); err != nil || len(stageReceipt.Files) != 1 {
		t.Fatal("missing real stage receipt")
	}
	file := stageReceipt.Files[0]
	files := []control.ActionFileAttachment{{SourceMessageID: file.SourceMessageID, LocalPath: filepath.Join(workspace, file.Target), FileName: file.Name}}
	pending := state.PendingTextInputRecord{Text: "old prompt", Inputs: []agentproto.Input{{Type: agentproto.InputText, Text: inboundFileAttachmentPrompt(files)}}, SourceMessageID: "message", ActorUserID: "u", ReplyToMessageID: "message"}
	frozen, _ := json.Marshal(pending)
	cp, err := j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	cp.Core.Navigation = nil
	cp.Core.Holds = append(cp.Core.Holds, orchestrator.PrivateRecoveryHold{Kind: "pending_text", InstanceID: "instance", Frozen: frozen})
	beforeRevision := cp.Revision
	cp.Revision++
	if err = j.CommitCheckpoint(ctx, cp, beforeRevision, nil); err != nil {
		t.Fatal(err)
	}

	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, p, l, _ = open()
	effects, err = p.ApplyAuthenticatedNativeHello(ctx, l, hello, "first-reconnect", orchestrator.PrivateNativeProfiles{})
	if err != nil {
		t.Fatal(err)
	}
	queryID := ""
	for _, e := range effects {
		event, x := privatechat.DecodeEffectEvent(e.Effect.Body)
		if x != nil {
			t.Fatal(x)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandThreadHistoryRead {
			queryID = e.Effect.ID
		}
	}
	if queryID == "" {
		t.Fatal("missing exact history query")
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: native}
	dispatch, cmd, err := l.PrepareEffectDispatch(ctx, queryID, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = l.MarkSending(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, p, l, _ = open()
	defer j.Close()
	if _, err = p.ApplyAuthenticatedNativeHello(ctx, l, hello, "second-reconnect", orchestrator.PrivateNativeProfiles{}); err != nil {
		t.Fatal(err)
	}
	if scenario == "changed_route" {
		detach := control.Action{GatewayID: "g", SurfaceSessionID: "s", ChatID: "c", ActorUserID: "u", Kind: control.ActionDetach}
		admit(j, p, "detach-during-recovery", "control", detach, nil)
	}
	if scenario == "damaged_receipt" {
		db, e := sql.Open("sqlite", path)
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.Exec(`UPDATE private_effect SET receipt=? WHERE id=?`, []byte(`{"damaged":true}`), stageID)
		db.Close()
		if e != nil {
			t.Fatal(e)
		}
	}
	event := agentproto.Event{Kind: agentproto.EventThreadHistoryRead, CommandID: cmd.CommandID, ThreadID: "thread", RequestID: "rpc", ThreadHistory: &agentproto.ThreadHistoryRecord{Thread: agentproto.ThreadSnapshotRecord{ThreadID: "thread", CWD: workspace}}, NativeBinding: &binding, Seq: 1}
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "carrier", Sequence: 1, PayloadHash: hash}
	cmdRaw, _ := json.Marshal(cmd)
	digest := sha256.Sum256(cmdRaw)
	receipt := agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "carrier", CommandID: cmd.CommandID, PayloadHash: hex.EncodeToString(digest[:]), PreparedFrames: 1, Frames: []agentproto.RuntimeFrameReceipt{{FrameID: 1, NativeRequestID: "rpc", NativeMethod: "thread/read", Stage: "unknown"}}}
	if err = l.RecoverNativeCommandReceipt(ctx, reconnectFixtureReceipt{native, receipt}, cmd.CommandID); err != nil {
		t.Fatal(err)
	}
	if _, err = l.ReceiveNativeEvent(ctx, event); err != nil {
		t.Fatal(err)
	}
	ref := privatechat.NativeEventRef{Incarnation: "carrier", Sequence: 1, PayloadHash: hash}
	if err = l.ResolveNativeDispatch(ctx, ref); err != nil {
		t.Fatal(err)
	}
	effects, err = p.ApplyNative(ctx, l, ref, privatechat.CoreNativeReducer{})
	if scenario == "damaged_receipt" {
		if err == nil {
			t.Fatal("damaged receipt accepted")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	prompt := 0
	for _, e := range effects {
		v, x := privatechat.DecodeEffectEvent(e.Effect.Body)
		if x != nil {
			t.Fatal(x)
		}
		if v.Notice != nil {
			t.Logf("result notice %s %s", v.Notice.Code, v.Notice.Text)
		}
		if v.Command != nil && v.Command.Kind == agentproto.CommandPromptSend {
			prompt++
			if v.Command.Target.ThreadID != "thread" || v.Command.Origin.MessageID != "message" {
				t.Fatal("lost source/target")
			}
		}
	}
	if scenario == "changed_route" {
		if prompt != 0 {
			t.Fatal("old input retargeted")
		}
		return
	}
	if prompt != 1 {
		t.Fatalf("prompt effects=%d", prompt)
	}
	replay, err := p.ApplyNative(ctx, l, ref, privatechat.CoreNativeReducer{})
	if err != nil || len(replay) != len(effects) {
		t.Fatalf("receipt replay: %v", err)
	}
	cp, err = j.LoadCheckpoint(ctx, owner, 1)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, item := range cp.Core.QueueItems {
		if item.SourceMessageID == "message" {
			n++
			encoded, _ := json.Marshal(item.Inputs)
			if item.ActorUserID != "u" || item.SurfaceSessionID != "s" || !strings.Contains(string(encoded), filepath.Join(workspace, file.Target)) || !strings.Contains(string(encoded), "ref.txt") {
				t.Fatalf("lost owner or staged structured input: %s", encoded)
			}
		}
	}
	if n != 1 {
		t.Fatalf("message items=%d", n)
	}
	_ = os.Remove(source)
}

type reconnectFixtureReceipt struct {
	owner   agentproto.NativeOwner
	receipt agentproto.RuntimeCommandReceipt
}

func (r reconnectFixtureReceipt) ReadNativeCommandReceipt(_ context.Context, o agentproto.NativeOwner, id string) (*agentproto.RuntimeCommandReceipt, error) {
	if o != r.owner || id != r.receipt.CommandID {
		return nil, privatechat.ErrJournalConflict
	}
	return &r.receipt, nil
}
