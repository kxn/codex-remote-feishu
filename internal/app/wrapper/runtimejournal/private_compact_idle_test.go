package runtimejournal

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateCompactFrameFreezesDurableEventBoundary(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "native.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "owner", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "instance", HostID: "host", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
	s, err := Open(t.Context(), path, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.BindPrivateLaunchSnapshot(snapshot); err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "exec"}
	prompt := agentproto.Command{CommandID: "prompt", Kind: agentproto.CommandPromptSend, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "thread"}}
	if _, _, err = s.PreparePrivateCommand(prompt); err != nil {
		t.Fatal(err)
	}
	promptFrame := []byte(`{"id":"prompt-request","method":"turn/start"}`)
	promptID, err := s.PrepareFrame(prompt.CommandID, promptFrame)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(promptID, promptFrame); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishFrame(promptID, nil); err != nil {
		t.Fatal(err)
	}
	old, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventItemCompleted, ThreadID: "thread", TurnID: "old", ItemKind: "context_compaction"}, {Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "active"}})
	if err != nil {
		t.Fatal(err)
	}
	command := agentproto.Command{CommandID: "compact", Kind: agentproto.CommandThreadCompactStart, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "thread"}}
	if _, _, err = s.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	frame := []byte(`{"id":"request","method":"thread/compact/start"}`)
	id, err := s.PrepareFrame(command.CommandID, frame)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(id, frame); err == nil {
		t.Fatal("active turn accepted without terminal")
	}
	if _, err = s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnCompleted, ThreadID: "thread", TurnID: "active"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(id, frame); err == nil {
		t.Fatal("terminal without exact prompt binding accepted")
	}
	if _, err = s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnStarted, ThreadID: "thread", TurnID: "finished"}, {Kind: agentproto.EventTurnCompleted, ThreadID: "thread", TurnID: "finished"}, {Kind: agentproto.EventNativeTurnBindingObserved, CommandID: "prompt", ThreadID: "thread", TurnID: "finished", Status: "accepted"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.StartFrame(id, frame); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishFrame(id, nil); err != nil {
		t.Fatal(err)
	}
	receipt, err := ReadPrivateCommandReceipt(t.Context(), snapshot, root, command.CommandID)
	if err != nil || len(receipt.Frames) != 1 || receipt.Frames[0].EventWatermark == nil || *receipt.Frames[0].EventWatermark != 6 || int64(old[0].Seq) > *receipt.Frames[0].EventWatermark {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}
