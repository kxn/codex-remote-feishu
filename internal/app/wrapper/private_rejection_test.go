package wrapper

import (
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
