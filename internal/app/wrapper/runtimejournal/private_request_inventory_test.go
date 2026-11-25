package runtimejournal

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateRequestInventoryMatchesDurableStream(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "runtime.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	s, err := Open(t.Context(), path, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.BindPrivateLaunchSnapshot(agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}); err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "execution"}
	events, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventRequestStarted, CommandID: "command", RequestID: "request", ThreadID: "thread", TurnID: "turn", NativeBinding: &binding}})
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]string{"request": "command"}
	snapshot, err := s.PendingPrivateRequests(t.Context(), owner, live)
	if err != nil || !snapshot.Complete || snapshot.EventSequence != events[0].Seq || len(snapshot.Requests) != 1 || snapshot.Requests[0].StartedHash != events[0].Journal.PayloadHash {
		t.Fatalf("snapshot=%+v error=%v", snapshot, err)
	}
	snapshot, err = s.PendingPrivateRequests(t.Context(), owner, map[string]string{})
	if err != nil || snapshot.Complete {
		t.Fatalf("missing live request accepted: %+v %v", snapshot, err)
	}
	if _, err = s.db.Exec(`UPDATE events SET hash='corrupt' WHERE incarnation=? AND sequence=?`, owner.CarrierIncarnation, events[0].Seq); err != nil {
		t.Fatal(err)
	}
	if snapshot, err = s.PendingPrivateRequests(t.Context(), owner, live); err != nil || snapshot.Complete {
		t.Fatalf("corrupt receipt accepted: %+v %v", snapshot, err)
	}
	if _, err = s.db.Exec(`UPDATE events SET hash=? WHERE incarnation=? AND sequence=?`, events[0].Journal.PayloadHash, owner.CarrierIncarnation, events[0].Seq); err != nil {
		t.Fatal(err)
	}
	c := agentproto.Command{CommandID: "reply", Kind: agentproto.CommandRequestRespond, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "thread", TurnID: "turn"}, Request: agentproto.Request{RequestID: "request"}}
	// Once a response is durably prepared, no later snapshot may report the
	// request as safely pending, including concurrent snapshot callers.
	if _, _, err = s.PreparePrivateCommand(c); err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.PendingPrivateRequests(t.Context(), owner, live)
	if err != nil || snapshot.Complete {
		t.Fatalf("prepared response accepted: %+v %v", snapshot, err)
	}
	wrong := owner
	wrong.ProcessGeneration++
	if snapshot, err = s.PendingPrivateRequests(t.Context(), wrong, live); err == nil && snapshot.Complete {
		t.Fatal("wrong generation accepted")
	}
	_, err = s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventRequestResolved, CommandID: "command", RequestID: "request", ThreadID: "thread", TurnID: "turn", NativeBinding: &binding}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.PendingPrivateRequests(t.Context(), owner, map[string]string{})
	if err != nil || !snapshot.Complete || len(snapshot.Requests) != 0 || snapshot.EventSequence != 2 {
		t.Fatalf("resolved snapshot=%+v error=%v", snapshot, err)
	}
	if _, err = s.db.Exec(`DELETE FROM events WHERE incarnation=? AND sequence=1`, owner.CarrierIncarnation); err != nil {
		t.Fatal(err)
	}
	if snapshot, err = s.PendingPrivateRequests(t.Context(), owner, map[string]string{}); err != nil || snapshot.Complete {
		t.Fatalf("gapped durable stream accepted: %+v %v", snapshot, err)
	}
}
