package privatechat

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

// Controlled retired authority exercises the processor contract; host stop
// inspection, process exit and write fence are tested separately.
func TestStoppedNativeProjectionRestoresPendingTerminal(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	path := filepath.Join(t.TempDir(), "db")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	// Controlled thread discovery after the authenticated hello; this is not a
	// second process or a fabricated online registration.
	inst := p.app.Service().Instance("instance")
	inst.ObservedFocusedThreadID = "thread"
	inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: "/tmp/workspace"}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "instance"
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "msg"
	text.Text = "hello"
	text.Inputs = []agentproto.Input{{Type: agentproto.InputLocalImage, Path: "/private/image.png"}}
	seq := admittedAction(t, j, o, "msg", "input", text)
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	toolContext, toolErr := p.ToolSurface(ctx, o, 1, native.InstanceID)
	if toolErr != nil || toolContext.Owner != o || toolContext.WorkspaceRoot != "/tmp/workspace" {
		t.Fatalf("pending private tool context: %#v %v", toolContext, toolErr)
	}
	if _, e := p.ToolSurface(ctx, o, 1, "other-instance"); e == nil {
		t.Fatal("foreign tool caller resolved")
	}
	var command agentproto.Command
	var commandEffectID string
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			command = *event.Command
			commandEffectID = effect.Effect.ID
		}
	}
	if command.CommandID == "" {
		t.Fatal("missing prompt")
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: native, ExecutionID: "execution"}
	d, command, err := lease.PrepareEffectDispatch(ctx, commandEffectID, binding)
	if err != nil {
		t.Fatalf("prepare effect: %v", err)
	}
	if len(d.InputSequences) != 1 || d.InputSequences[0] != seq {
		t.Fatalf("wrong source inputs: %#v", d)
	}
	if err = lease.MarkSending(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	translator := codex.NewTranslator("instance")
	frames, err := translator.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	var request struct {
		ID string `json:"id"`
	}
	if err = json.Unmarshal(frames[0], &request); err != nil {
		t.Fatal(err)
	}
	observed, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"thread":{"id":"thread"},"turn":{"id":"turn"},"model":"fixture-model"}}`, request.ID)))
	turnFrame := frames[0]
	if err == nil && len(observed.OutboundToCodex) > 0 {
		turnFrame = observed.OutboundToCodex[0]
		if err = json.Unmarshal(turnFrame, &request); err != nil {
			t.Fatal(err)
		}
	}
	early, earlyErr := translator.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread","turn":{"id":"turn"}}}`))
	if earlyErr != nil || len(early.Events) != 1 {
		t.Fatalf("early turn: %v %#v", earlyErr, early.Events)
	}
	if early.Events[0].CommandID != "" {
		t.Fatalf("fixture expected unbound notification: %#v", early.Events[0])
	}
	ref1 := persistNative(t, ctx, lease, binding, 1, early.Events[0])
	if _, err = p.ApplyNative(ctx, lease, ref1, CoreNativeReducer{}); err == nil {
		t.Fatal("unbound early turn projected before native proof")
	}
	observed, err = translator.ObserveServer([]byte(fmt.Sprintf(`{"id":%q,"result":{"turn":{"id":"turn"}}}`, request.ID)))
	if err != nil || len(observed.Events) != 1 {
		t.Fatalf("turn binding: %v %#v", err, observed.Events)
	}
	ref2 := persistNative(t, ctx, lease, binding, 2, observed.Events[0])
	if _, err = p.ApplyNative(ctx, lease, ref2, CoreNativeReducer{}); err == nil {
		t.Fatal("event projected before frame receipt")
	}
	if cleanup, e := j.PendingStagingCleanup(ctx, 10); e != nil || len(cleanup) != 0 {
		t.Fatalf("cleanup before terminal: %#v %v", cleanup, e)
	}
	root := t.TempDir()
	cwd := t.TempDir()
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: native, CWD: cwd, JournalPath: filepath.Join(root, "wrapper.db")}
	wrapper, err := runtimejournal.Open(ctx, snapshot.JournalPath, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	defer wrapper.Close()
	if err = wrapper.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	if _, _, err = wrapper.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	frameID, err := wrapper.PrepareFrame(command.CommandID, turnFrame)
	if err != nil {
		t.Fatal(err)
	}
	if err = wrapper.StartFrame(frameID, turnFrame); err != nil {
		t.Fatal(err)
	}
	if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snapshot, root}, command.CommandID); err != nil {
		t.Fatal(err)
	}
	if err = lease.ResolveNativeDispatch(ctx, ref2); err != nil {
		t.Fatal(err)
	}

	if _, err = p.ApplyNative(ctx, lease, ref1, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyNative(ctx, lease, ref2, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	queued := privateAction(o, control.ActionTextMessage)
	queued.MessageID, queued.Text = "queued", "after stop"
	queuedSeq := admittedAction(t, j, o, "queued", "input", queued)
	if _, err = p.ApplyIngress(ctx, o, 1, queuedSeq); err != nil {
		t.Fatal(err)
	}
	if err = p.MarkNativeStopping(ctx, lease); err != nil {
		t.Fatal(err)
	}
	terminal := persistNative(t, ctx, lease, binding, 3, agentproto.Event{Kind: agentproto.EventTurnCompleted, CommandID: command.CommandID, ThreadID: "thread", TurnID: "turn", Status: "interrupted"})
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	wrong := o
	wrong.UserID = "another"
	if err = p.ApplyStoppedNativeEvents(ctx, testNativeAuthority{}, native, wrong, 1); err == nil {
		t.Fatal("foreign owner accepted")
	}
	stopped, err := j.openStoppedNativeProjection(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	forged := terminal
	forged.PayloadHash = "wrong"
	if _, err = p.ApplyNativeForStop(ctx, stopped, forged, CoreNativeReducer{}); err == nil {
		t.Fatal("forged terminal accepted")
	}
	if err = p.ApplyStoppedNativeEvents(ctx, testNativeAuthority{}, native, o, 1); err != nil {
		t.Fatal(err)
	}
	cp, err := j.LoadCheckpoint(ctx, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	if cp.Core.ActiveQueueItemID != "" || len(cp.Core.QueuedQueueItemIDs) != 1 {
		t.Fatalf("terminal did not settle active while preserving queued input: %+v", cp.Core)
	}
	var applied int
	if err = j.db.QueryRow(`SELECT applied FROM native_events WHERE incarnation=? AND sequence=?`, terminal.Incarnation, terminal.Sequence).Scan(&applied); err != nil || applied != 1 {
		t.Fatalf("terminal not durably applied: %d %v", applied, err)
	}
	if err = p.ApplyStoppedNativeEvents(ctx, testNativeAuthority{}, native, o, 1); err != nil {
		t.Fatalf("replay: %v", err)
	}
}
