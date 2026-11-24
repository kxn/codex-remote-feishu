package wrapper

import (
	"bytes"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/adapter/relayws"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivatePrewriteRejectionIsDurableAndNeverClaimsStartedFrame(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_write", true: "write_started"}[started], func(t *testing.T) {
			a := privateTestApp(t, agentproto.BackendCodex)
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: a.privateNative.snapshot.Owner, ExecutionID: "exec"}
			command := agentproto.Command{CommandID: "start", Kind: agentproto.CommandPromptSend, NativeBinding: &binding}
			if _, err := a.prepareJournalCommand(command); err != nil {
				t.Fatal(err)
			}
			if started {
				frame := []byte(`{"id":"rpc","method":"turn/start"}`)
				id, err := a.privateNative.journal.PrepareFrame(command.CommandID, frame)
				if err != nil {
					t.Fatal(err)
				}
				if err = a.privateNative.journal.StartFrame(id, frame); err != nil {
					t.Fatal(err)
				}
			}
			client := relayws.NewClient("ws://localhost", agentproto.Hello{}, relayws.ClientCallbacks{})
			client.SetEventJournal(a.privateNative.journal)
			a.recordPrivatePrewriteRejection(client, command, errors.New("unsupported"))
			events, _, err := a.privateNative.journal.Replay(0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if started {
				if len(events) != 0 {
					t.Fatal("uncertain write reported rejected")
				}
				return
			}
			if len(events) != 1 || events[0].Kind != agentproto.EventNativeCommandRejected || events[0].Journal == nil || events[0].NativeBinding == nil || *events[0].NativeBinding != binding || events[0].Problem == nil || events[0].Problem.CommandID != command.CommandID {
				t.Fatalf("missing durable rejection: %+v", events)
			}
		})
	}
}

func TestPrivateCompactIdleRejectionKeepsWriterAlive(t *testing.T) {
	a := privateTestApp(t, agentproto.BackendCodex)
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: a.privateNative.snapshot.Owner, ExecutionID: "compact"}
	command := agentproto.Command{CommandID: "compact", Kind: agentproto.CommandThreadCompactStart, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "thread"}}
	if _, err := a.prepareJournalCommand(command); err != nil {
		t.Fatal(err)
	}
	resume := []byte(`{"id":"resume","method":"thread/resume"}`)
	runtime := a.runtime.(*privateNativeRuntime)
	if err := runtime.registerFrames(command.CommandID, [][]byte{resume}); err != nil {
		t.Fatal(err)
	}
	if err := writeJournaledChildFrame(&bytes.Buffer{}, resume, runtime); err != nil {
		t.Fatal(err)
	}
	if _, err := a.privateNative.journal.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "active"}}); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"rpc","method":"thread/compact/start"}`)
	if err := runtime.registerFrames(command.CommandID, [][]byte{frame}); err != nil {
		t.Fatal(err)
	}
	var child bytes.Buffer
	if err := writeJournaledChildFrame(&child, frame, runtime); err != nil {
		t.Fatal(err)
	}
	if child.Len() != 0 {
		t.Fatal("rejected frame reached child")
	}
	events, _, err := a.privateNative.journal.Replay(0, 10)
	if err != nil || len(events) != 2 || events[1].Kind != agentproto.EventNativeCommandRejected || events[1].CommandID != command.CommandID {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}
