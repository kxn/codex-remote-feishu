package runtimejournal

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateResponseWrittenIsAtomicAndReplayed(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "written", true: "partial"}[failure], func(t *testing.T) {
			root, cwd := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "journal.db")
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
			open := func() *Store {
				s, e := Open(t.Context(), path, root, cwd, "inc")
				if e != nil {
					t.Fatal(e)
				}
				if e = s.BindPrivateLaunchSnapshot(agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}); e != nil {
					t.Fatal(e)
				}
				return s
			}
			s := open()
			binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "execution"}
			c := agentproto.Command{CommandID: "reply", Kind: agentproto.CommandRequestRespond, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "session", TurnID: "turn"}, Request: agentproto.Request{RequestID: "request"}}
			if _, _, err := s.PreparePrivateCommand(c); err != nil {
				t.Fatal(err)
			}
			frame := []byte(`{"id":"request","result":{"decision":"accept"}}`)
			id, err := s.PrepareFrame(c.CommandID, frame)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.StartFrame(id, frame); err != nil {
				t.Fatal(err)
			}
			var writeErr error
			if failure {
				writeErr = errors.New("partial write")
			}
			event, err := s.FinishPrivateFrame(id, writeErr)
			if err != nil {
				t.Fatal(err)
			}
			if failure && event != nil || !failure && (event == nil || event.Action != "request.response.written" || event.Status != "sent" || event.RequestID != "request" || event.Journal == nil) {
				t.Fatalf("event=%+v", event)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s = open()
			defer s.Close()
			events, _, err := s.Replay(0, 10)
			if err != nil {
				t.Fatal(err)
			}
			if failure && len(events) != 0 || !failure && (len(events) != 1 || events[0].Journal.PayloadHash != event.Journal.PayloadHash) {
				t.Fatalf("replay=%+v err=%v", events, err)
			}
		})
	}
}
