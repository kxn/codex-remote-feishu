package privatechat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/claude"
	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func nativeHello(owner agentproto.NativeOwner) agentproto.Hello {
	return agentproto.Hello{PrivateNativeProtocolVersion: agentproto.PrivateNativeProtocolVersion, CarrierIncarnation: owner.CarrierIncarnation, Instance: agentproto.InstanceHello{InstanceID: owner.InstanceID, Backend: owner.Backend, WorkspaceRoot: "/tmp/workspace", WorkspaceKey: "/tmp/workspace", Source: "headless", Managed: true}}
}
func persistNative(t *testing.T, ctx context.Context, l *NativeLease, binding agentproto.NativeTurnBinding, seq uint64, event agentproto.Event) NativeEventRef {
	t.Helper()
	// The wrapper annotates command-correlated events; raw notifications may be
	// unbound and must be correlated by a persisted protocol proof instead.
	if event.CommandID != "" {
		event.NativeBinding = &binding
	}
	event.Seq = seq
	hash, err := agentproto.RuntimeEventPayloadHash(event)
	if err != nil {
		t.Fatal(err)
	}
	event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: binding.Owner.CarrierIncarnation, Sequence: seq, PayloadHash: hash}
	receipt, err := l.ReceiveNativeEvent(ctx, event)
	if err != nil || receipt == nil {
		t.Fatalf("durable native receive: %v", err)
	}
	return NativeEventRef{Incarnation: binding.Owner.CarrierIncarnation, Sequence: seq, PayloadHash: hash}
}

func TestCodexNativeAppliedAndFinalAfterAckRestart(t *testing.T) {
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
	// Receiver ACK precedes crash; replay must use the persisted event.
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	lease, err = j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyNative(ctx, lease, ref2, CoreNativeReducer{}); !errors.Is(err, ErrJournalState) {
		t.Fatalf("out-of-order native event: %v", err)
	}
	forged := ref1
	forged.PayloadHash = "wrong"
	if _, err = p.ApplyNative(ctx, lease, forged, CoreNativeReducer{}); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("forged event accepted: %v", err)
	}
	if _, err = j.db.Exec(`CREATE TRIGGER fail_native_checkpoint BEFORE UPDATE ON private_checkpoint BEGIN SELECT RAISE(FAIL,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyNative(ctx, lease, ref1, CoreNativeReducer{}); !errors.Is(err, ErrProcessorPoisoned) {
		t.Fatalf("native commit did not poison: %v", err)
	}
	var applied int
	if err = j.db.QueryRow(`SELECT applied FROM native_events WHERE incarnation=? AND sequence=?`, ref1.Incarnation, ref1.Sequence).Scan(&applied); err != nil || applied != 0 {
		t.Fatalf("native marker escaped rollback: %d %v", applied, err)
	}
	if _, err = j.db.Exec(`DROP TRIGGER fail_native_checkpoint`); err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyNative(ctx, lease, ref1, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyNative(ctx, lease, ref2, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyNative(ctx, lease, ref1, CoreNativeReducer{}); err != nil {
		t.Fatalf("replay: %v", err)
	}
	activeBefore := p.app.Service().Surface(o.SurfaceID).ActiveQueueItemID
	if activeBefore == "" {
		t.Fatal("expected active input before disconnect")
	}
	if _, err = p.ApplyNativeDisconnect(ctx, lease, "host-lifecycle:connection-1"); err != nil {
		t.Fatal(err)
	}
	if p.app.Service().Instance(native.InstanceID).Online || p.app.Service().Surface(o.SurfaceID).ActiveQueueItemID != activeBefore {
		t.Fatal("disconnect consumed work or kept instance online")
	}
	if p.app.Service().PrivateRecoveryActionAllowed(text) {
		t.Fatal("disconnect did not hold unresolved input")
	}
	if _, err = p.ApplyNativeDisconnect(ctx, lease, "host-lifecycle:connection-1"); err != nil {
		t.Fatalf("disconnect replay: %v", err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	delta, err := translator.ObserveServer([]byte(`{"method":"item/agentMessage/delta","params":{"threadId":"thread","turnId":"turn","itemId":"answer","delta":"final "}}`))
	if err != nil || len(delta.Events) != 1 {
		t.Fatalf("translate live delta: %v", err)
	}
	live := delta.Events[0]
	live.NativeBinding = &binding
	liveHash, err := agentproto.HashRuntimeLiveEvent(live)
	if err != nil {
		t.Fatal(err)
	}
	live.Live = &agentproto.RuntimeLiveIdentity{Version: agentproto.RuntimeLiveProtocolVersion, CarrierIncarnation: native.CarrierIncarnation, Sequence: 1, PayloadHash: liveHash}
	if _, err = p.ApplyLiveNative(ctx, lease, live, CoreNativeReducer{}); err != nil {
		t.Fatalf("live delta: %v", err)
	}
	if _, err = p.ApplyLiveNative(ctx, lease, live, CoreNativeReducer{}); err != nil {
		t.Fatalf("duplicate live: %v", err)
	}
	forgedLive := live
	forgedLive.Delta = "wrong"
	if _, err = p.ApplyLiveNative(ctx, lease, forgedLive, CoreNativeReducer{}); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("forged live accepted: %v", err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	lease, err = j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	if _, err = p.ApplyLiveNative(ctx, lease, live, CoreNativeReducer{}); err != nil {
		t.Fatalf("live replay after restart: %v", err)
	}
	events := []string{`{"method":"item/completed","params":{"threadId":"thread","turnId":"turn","item":{"id":"answer","type":"agentMessage","text":"final answer"}}}`, `{"method":"turn/completed","params":{"threadId":"thread","turn":{"id":"turn","status":"completed"}}}`}
	for i, frame := range events {
		result, e := translator.ObserveServer([]byte(frame))
		if e != nil || len(result.Events) != 1 {
			t.Fatalf("translate %d: %v %#v", i, e, result.Events)
		}
		ref := persistNative(t, ctx, lease, binding, uint64(i+3), result.Events[0])
		effects, e := p.ApplyNative(ctx, lease, ref, CoreNativeReducer{})
		if e != nil {
			t.Fatalf("apply %d: %v", i, e)
		}
		if i == 1 {
			if _, e = p.ApplyNative(ctx, lease, ref, CoreNativeReducer{}); e != nil {
				t.Fatalf("terminal replay: %v", e)
			}
			cleanup, e := j.PendingStagingCleanup(ctx, 10)
			if e != nil || len(cleanup) != 1 || cleanup[0].Path != "/private/image.png" || cleanup[0].EventID != "msg" || cleanup[0].MessageID != "msg" {
				t.Fatalf("terminal image source: %#v %v", cleanup, e)
			}
		}
		if i == 0 {
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			j, err = OpenJournal(path)
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			lease, err = j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
			if err != nil {
				t.Fatal(err)
			}
			p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
				t.Fatal(err)
			}
		}
		if i == 1 {
			found := false
			for _, effect := range effects {
				event, e := DecodeEffectEvent(effect.Effect.Body)
				if e != nil {
					t.Fatal(e)
				}
				if event.Block != nil && event.Block.Final && event.Block.Text == "final answer" {
					found = true
				}
			}
			if !found {
				t.Fatalf("no final effect: %#v", effects)
			}
			if !p.app.Service().PrivateRecoveryActionAllowed(text) {
				t.Fatal("exact native terminal did not clear offline hold")
			}
		}
	}
}

func TestClaudeNativeAppliedAndFinalAfterAckRestart(t *testing.T) {
	ctx := context.Background()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "claude-inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendClaude, HistoryNamespace: "history"}
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
	inst := p.app.Service().Instance("instance")
	inst.ObservedFocusedThreadID = "session"
	inst.Threads["session"] = &state.ThreadRecord{ThreadID: "session", CWD: "/tmp/workspace"}
	mode := privateAction(o, control.ActionModeCommand)
	mode.Text = "/mode claude"
	if _, err = p.app.Action(mode); err != nil {
		t.Fatal(err)
	}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "instance"
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	text := privateAction(o, control.ActionTextMessage)
	text.MessageID = "msg"
	text.Text = "hello"
	imagePath := filepath.Join(t.TempDir(), "image.png")
	if err = os.WriteFile(imagePath, []byte("image bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	text.Inputs = []agentproto.Input{{Type: agentproto.InputLocalImage, Path: imagePath, MIMEType: "image/png"}}
	seq := admittedAction(t, j, o, "msg", "input", text)
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
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
		t.Fatalf("no Claude prompt: %#v", effects)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: native, ExecutionID: "execution"}
	d, command, err := lease.PrepareEffectDispatch(ctx, commandEffectID, binding)
	if err != nil {
		t.Fatalf("prepare effect: %v %#v", err, command.Target)
	}
	if err = lease.MarkSending(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	translator := claude.NewTranslator("instance")
	if _, err = translator.ObserveServer([]byte(`{"type":"system","subtype":"init","session_id":"session","permissionMode":"default"}`)); err != nil {
		t.Fatal(err)
	}
	frames, err := translator.TranslateCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	const uuid = "47a986d9-f93a-4e0d-bf3d-e70cc48f8910"
	translator.RegisterUserCommandUUID(uuid, command)
	var userFrame []byte
	for _, frame := range frames {
		var obj map[string]any
		if json.Unmarshal(frame, &obj) == nil && obj["type"] == "user" {
			obj["uuid"] = uuid
			userFrame, _ = json.Marshal(obj)
		}
	}
	if len(userFrame) == 0 {
		t.Fatal("missing Claude user frame")
	}
	lifecycle, err := translator.ObserveServer([]byte(fmt.Sprintf(`{"type":"command_lifecycle","command_uuid":%q,"state":"started","session_id":"session"}`, uuid)))
	if err != nil || len(lifecycle.Events) != 1 {
		t.Fatalf("lifecycle: %v %#v", err, lifecycle.Events)
	}
	ref1 := persistNative(t, ctx, lease, binding, 1, lifecycle.Events[0])
	if _, err = p.ApplyNative(ctx, lease, ref1, CoreNativeReducer{}); err == nil {
		t.Fatal("unproven Claude event applied")
	}
	root, cwd := t.TempDir(), t.TempDir()
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: native, CWD: cwd, JournalPath: filepath.Join(root, "wrapper.db")}
	wrapper, err := runtimejournal.Open(ctx, snapshot.JournalPath, root, cwd, "claude-inc")
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
	frameID, err := wrapper.PrepareFrame(command.CommandID, userFrame)
	if err != nil {
		t.Fatal(err)
	}
	if err = wrapper.StartFrame(frameID, userFrame); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	lease, err = j.OpenNativeLease(ctx, testNativeAuthority{}, native, o, 1)
	if err != nil {
		t.Fatal(err)
	}
	p, err = NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
	if err != nil {
		t.Fatal(err)
	}
	if err = p.RegisterNativeHello(ctx, lease, nativeHello(native)); err != nil {
		t.Fatal(err)
	}
	if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snapshot, root}, command.CommandID); err != nil {
		t.Fatal(err)
	}
	next := uint64(2)
	startRefs := []NativeEventRef{ref1}
	for _, frame := range []string{`{"type":"stream_event","event":{"type":"message_start","message":{"id":"answer"}}}`, `{"type":"stream_event","event":{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}}`} {
		started, e := translator.ObserveServer([]byte(frame))
		if e != nil {
			t.Fatal(e)
		}
		for _, ev := range started.Events {
			if ev.Kind == agentproto.EventTurnStarted && ev.RequestID == "" {
				ev.RequestID = uuid
			}
			ref := persistNative(t, ctx, lease, binding, next, ev)
			next++
			startRefs = append(startRefs, ref)
		}
	}
	for _, ref := range startRefs {
		if _, err = p.ApplyNative(ctx, lease, ref, CoreNativeReducer{}); err != nil {
			t.Fatalf("Claude stream start: %v", err)
		}
	}
	delta, err := translator.ObserveServer([]byte(`{"type":"stream_event","event":{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Claude "}}}`))
	if err != nil || len(delta.Events) != 1 {
		t.Fatalf("Claude delta: %v %#v", err, delta.Events)
	}
	live := delta.Events[0]
	live.NativeBinding = &binding
	liveHash, err := agentproto.HashRuntimeLiveEvent(live)
	if err != nil {
		t.Fatal(err)
	}
	live.Live = &agentproto.RuntimeLiveIdentity{Version: agentproto.RuntimeLiveProtocolVersion, CarrierIncarnation: native.CarrierIncarnation, Sequence: 1, PayloadHash: liveHash}
	if _, err = p.ApplyLiveNative(ctx, lease, live, CoreNativeReducer{}); err != nil {
		t.Fatalf("Claude live: %v", err)
	}
	if _, err = p.ApplyLiveNative(ctx, lease, live, CoreNativeReducer{}); err != nil {
		t.Fatalf("Claude duplicate: %v", err)
	}
	assistant, err := translator.ObserveServer([]byte(`{"type":"assistant","session_id":"session","message":{"id":"answer","content":[{"type":"text","text":"Claude answer"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var refs []NativeEventRef
	for _, ev := range assistant.Events {
		if ev.Kind == agentproto.EventTurnStarted && ev.RequestID == "" {
			ev.RequestID = uuid
		}
		refs = append(refs, persistNative(t, ctx, lease, binding, next, ev))
		next++
	}
	for _, ref := range refs {
		if _, err = p.ApplyNative(ctx, lease, ref, CoreNativeReducer{}); err != nil {
			t.Fatalf("apply Claude seq %d: %v", ref.Sequence, err)
		}
	}
	result, err := translator.ObserveServer([]byte(`{"type":"result","subtype":"success","session_id":"session","result":"Claude answer","is_error":false}`))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range result.Events {
		ref := persistNative(t, ctx, lease, binding, next, ev)
		next++
		out, e := p.ApplyNative(ctx, lease, ref, CoreNativeReducer{})
		if e != nil {
			t.Fatalf("Claude final %d: %v", ref.Sequence, e)
		}
		for _, effect := range out {
			projected, e := DecodeEffectEvent(effect.Effect.Body)
			if e != nil {
				t.Fatal(e)
			}
			if projected.Block != nil && projected.Block.Final && projected.Block.Text == "Claude answer" {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("Claude final missing: %#v", result.Events)
	}
	cleanup, e := j.PendingStagingCleanup(ctx, 10)
	if e != nil || len(cleanup) != 1 || cleanup[0].Path != imagePath || cleanup[0].EventID != "msg" {
		t.Fatalf("Claude terminal image: %#v %v", cleanup, e)
	}
}
