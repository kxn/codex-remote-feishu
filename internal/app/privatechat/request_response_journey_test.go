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
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/orchestrator"
)

type captureRequestEvents struct{ seen *[]agentproto.Event }

func (r captureRequestEvents) ReduceNative(_ *App, _ string, e agentproto.Event) ([]eventcontract.Event, error) {
	*r.seen = append(*r.seen, e)
	return nil, nil
}

func TestPrivateRequestResponseWrittenAndResolvedWithNativeReordering(t *testing.T) {
	for _, before := range []bool{false, true} {
		t.Run(fmt.Sprintf("resolvedBeforeWrite=%v", before), func(t *testing.T) {
			ctx := context.Background()
			root, cwd := t.TempDir(), t.TempDir()
			j, err := OpenJournal(filepath.Join(root, "private.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			surface := Owner{"g", "s", "c", "u", "tenant", "app"}
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
			lease, err := j.OpenNativeLease(ctx, testNativeAuthority{}, owner, surface, 1)
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewProcessor(New(nil, orchestrator.Config{}), j, actionDecoder{}, CoreReducer{})
			if err != nil {
				t.Fatal(err)
			}
			if err = p.RegisterNativeHello(ctx, lease, nativeHello(owner)); err != nil {
				t.Fatal(err)
			}
			attach := privateAction(surface, control.ActionAttachInstance)
			attach.InstanceID = owner.InstanceID
			if _, err = p.app.Action(attach); err != nil {
				t.Fatal(err)
			}
			seq, _, err := j.Admit(ctx, Ingress{Owner: surface, Generation: 1, EventID: "reply", Lane: "control", Payload: []byte(`{}`)})
			if err != nil {
				t.Fatal(err)
			}
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "execution"}
			command := agentproto.Command{CommandID: "response", Kind: agentproto.CommandRequestRespond, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "thread", TurnID: "turn"}, Request: agentproto.Request{RequestID: "request", Response: map[string]any{"type": "approval", "decision": "accept"}}}
			dispatch := Dispatch{ID: "dispatch", Owner: surface, Generation: 1, CommandID: command.CommandID, Lane: "control", Mode: "resume", Session: NativeSession{Backend: "codex", Namespace: "history", CarrierID: "inc", CarrierGeneration: 1, SessionID: "thread"}, InputSequences: []int64{seq}}
			if err = lease.prepareNativeDispatch(ctx, dispatch, command); err != nil {
				t.Fatal(err)
			}
			if err = lease.MarkSending(ctx, dispatch.ID); err != nil {
				t.Fatal(err)
			}
			snap := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: filepath.Join(root, "native.db")}
			native, err := runtimejournal.Open(ctx, snap.JournalPath, root, cwd, "inc")
			if err != nil {
				t.Fatal(err)
			}
			defer native.Close()
			if err = native.BindPrivateLaunchSnapshot(snap); err != nil {
				t.Fatal(err)
			}
			if _, _, err = native.PreparePrivateCommand(command); err != nil {
				t.Fatal(err)
			}
			tr := codex.NewTranslator("instance")
			started, err := tr.ObserveServer([]byte(`{"id":"request","method":"item/commandExecution/requestApproval","params":{"threadId":"thread","turnId":"turn"}}`))
			if err != nil || len(started.Events) != 1 {
				t.Fatalf("start %+v %v", started, err)
			}
			first, err := native.PersistEvents(started.Events)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = lease.ReceiveNativeEvent(ctx, first[0]); err != nil {
				t.Fatal(err)
			}
			frames, err := tr.TranslateCommand(command)
			if err != nil || len(frames) != 1 {
				t.Fatalf("frame %v %v", frames, err)
			}
			var response map[string]any
			if json.Unmarshal(frames[0], &response) != nil || response["id"] != "request" {
				t.Fatalf("bad response %s", frames[0])
			}
			id, err := native.PrepareFrame(command.CommandID, frames[0])
			if err != nil {
				t.Fatal(err)
			}
			if err = native.StartFrame(id, frames[0]); err != nil {
				t.Fatal(err)
			}
			resolve := func() agentproto.Event {
				r, e := tr.ObserveServer([]byte(`{"method":"serverRequest/resolved","params":{"requestId":"request","threadId":"thread","turnId":"turn"}}`))
				if e != nil || len(r.Events) != 1 {
					t.Fatalf("resolve %+v %v", r, e)
				}
				ev, e := native.PersistEvents(r.Events)
				if e != nil {
					t.Fatal(e)
				}
				return ev[0]
			}
			var resolved agentproto.Event
			if before {
				resolved = resolve()
				if _, err = lease.ReceiveNativeEvent(ctx, resolved); err != nil {
					t.Fatal(err)
				}
			}
			sent, err := native.FinishPrivateFrame(id, nil)
			if err != nil || sent == nil {
				t.Fatalf("written fact %+v %v", sent, err)
			}
			if !before {
				resolved = resolve()
				if _, err = lease.ReceiveNativeEvent(ctx, resolved); err != nil {
					t.Fatal(err)
				}
			}
			if err = lease.RecoverNativeCommandReceipt(ctx, testJournalReader{snap, root}, command.CommandID); err != nil {
				t.Fatal(err)
			}
			if _, err = lease.ReceiveNativeEvent(ctx, *sent); err != nil {
				t.Fatal(err)
			}
			state, err := j.DispatchState(ctx, dispatch.ID)
			if err != nil || state != "resolved" {
				t.Fatalf("state=%s %v", state, err)
			}
			events, _, err := native.Replay(0, 10)
			if err != nil || len(events) != 3 {
				t.Fatalf("replay=%d %v", len(events), err)
			}
			var seen []agentproto.Event
			for _, e := range events {
				ref := NativeEventRef{Incarnation: "inc", Sequence: e.Seq, PayloadHash: e.Journal.PayloadHash}
				if _, err = p.ApplyNative(ctx, lease, ref, captureRequestEvents{&seen}); err != nil {
					t.Fatalf("apply %s seq=%d: %v", e.Kind, e.Seq, err)
				}
			}
			if len(seen) != 3 {
				t.Fatalf("projected %d events", len(seen))
			}
			if _, err = lease.ReceiveNativeEvent(ctx, *sent); err != nil {
				t.Fatalf("sent replay after resolution: %v", err)
			}
		})
	}
}
