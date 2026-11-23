package privatechat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/wrapper/runtimejournal"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

type prewriteReader struct {
	receipt agentproto.RuntimeCommandReceipt
}

func (r prewriteReader) ReadNativeCommandReceipt(context.Context, agentproto.NativeOwner, string) (*agentproto.RuntimeCommandReceipt, error) {
	copy := r.receipt
	return &copy, nil
}
func TestPrivatePrewriteRejectionRequiresZeroStartedFrames(t *testing.T) {
	for _, stage := range []string{"prepared", "child_write_started"} {
		t.Run(stage, func(t *testing.T) {
			ctx := t.Context()
			j, err := OpenJournal(filepath.Join(t.TempDir(), "chat.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			surface := Owner{"gateway", "surface", "chat", "user", "tenant", "app"}
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
			lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
			if err != nil {
				t.Fatal(err)
			}
			seq, _, err := j.Admit(ctx, Ingress{Owner: surface, Generation: 1, EventID: "message", Lane: "input", Payload: []byte(`{"text":"hello"}`), Sources: []string{"message"}})
			if err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "turn"}
			command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{ExecutionMode: agentproto.PromptExecutionModeStartNew}}
			d := Dispatch{ID: "dispatch", Owner: surface, Generation: 1, CommandID: "start", Lane: "input", Mode: "start", InputSequences: []int64{seq}, Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1}}
			if err = lease.prepareNativeDispatch(ctx, d, command); err != nil {
				t.Fatal(err)
			}
			if err = lease.MarkSending(ctx, d.ID); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(command)
			sum := sha256.Sum256(raw)
			reader := prewriteReader{agentproto.RuntimeCommandReceipt{Version: 1, CarrierIncarnation: "inc", CommandID: "start", PayloadHash: hex.EncodeToString(sum[:]), Stage: stage}}
			if err = lease.RecoverNativeCommandReceipt(ctx, reader, "start"); err != nil {
				t.Fatal(err)
			}
			problem := agentproto.ErrorInfo{Code: "command_rejected", CommandID: "start", Message: "rejected"}
			event := agentproto.Event{Seq: 1, Kind: agentproto.EventNativeCommandRejected, CommandID: "start", NativeBinding: &binding, Status: "failed", Action: "wrapper.before_child_write", Problem: &problem}
			hash, _ := agentproto.RuntimeEventPayloadHash(event)
			event.Journal = &agentproto.RuntimeEventIdentity{Version: 1, CarrierIncarnation: "inc", Sequence: 1, PayloadHash: hash}
			if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
			state, err := j.DispatchState(ctx, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stage == "prepared" && state != "failed" || stage != "prepared" && state != "sending" {
				t.Fatalf("wrong prewrite outcome %s", state)
			}
			if _, err = lease.ReceiveNativeEvent(ctx, event); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPrivateDurablePrewriteRejectionRollsBackOldQueue(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	o := Owner{"g", "s", "c", "u", "tenant", "app"}
	native := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	j, err := OpenJournal(filepath.Join(root, "private.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
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
	inst.ObservedFocusedThreadID = "thread"
	inst.Threads["thread"] = &state.ThreadRecord{ThreadID: "thread", CWD: "/tmp/workspace"}
	attach := privateAction(o, control.ActionAttachInstance)
	attach.InstanceID = "instance"
	if _, err = p.app.Action(attach); err != nil {
		t.Fatal(err)
	}
	message := privateAction(o, control.ActionTextMessage)
	message.MessageID = "message"
	message.Text = "hello"
	seq := admittedAction(t, j, o, "message", "input", message)
	effects, err := p.ApplyIngress(ctx, o, 1, seq)
	if err != nil {
		t.Fatal(err)
	}
	var effectID string
	for _, effect := range effects {
		event, e := DecodeEffectEvent(effect.Effect.Body)
		if e != nil {
			t.Fatal(e)
		}
		if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend {
			effectID = effect.Effect.ID
		}
	}
	if effectID == "" {
		t.Fatal("missing prompt effect")
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: native, ExecutionID: "execution"}
	dispatch, command, err := lease.PrepareEffectDispatch(ctx, effectID, binding)
	if err != nil {
		t.Fatal(err)
	}
	if err = lease.MarkSending(ctx, dispatch.ID); err != nil {
		t.Fatal(err)
	}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: native, CWD: "/tmp/workspace", JournalPath: filepath.Join(root, "runtime.db")}
	wrapper, err := runtimejournal.Open(ctx, snapshot.JournalPath, root, snapshot.CWD, "inc")
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
	if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snapshot: snapshot, stateDir: root}, command.CommandID); err != nil {
		t.Fatal(err)
	}
	problem := agentproto.ErrorInfo{Code: "command_rejected", CommandID: command.CommandID, Message: "unsupported"}
	ref := persistNative(t, ctx, lease, binding, 1, agentproto.Event{Kind: agentproto.EventNativeCommandRejected, CommandID: command.CommandID, Status: "failed", Action: "wrapper.before_child_write", Problem: &problem})
	if _, err = p.ApplyNative(ctx, lease, ref, CoreNativeReducer{}); err != nil {
		t.Fatal(err)
	}
	surface := p.app.Service().Surface(o.SurfaceID)
	if surface.ActiveQueueItemID != "" {
		t.Fatal("failed command retained active queue")
	}
	failed := 0
	for _, item := range surface.QueueItems {
		if item.Status == state.QueueItemFailed {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("queue not failed: %+v", surface.QueueItems)
	}
	if _, err = p.ApplyNative(ctx, lease, ref, CoreNativeReducer{}); err != nil {
		t.Fatalf("replay: %v", err)
	}
}
