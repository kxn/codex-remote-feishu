package runtimejournal

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"path/filepath"
	"testing"
)

func TestReviewTurnBindingRequiresExactNativeReviewResult(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	path := filepath.Join(root, "native.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "i", HostID: "h", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	s, err := Open(t.Context(), path, root, cwd, "inc")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.BindPrivateLaunchSnapshot(agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}); err != nil {
		t.Fatal(err)
	}
	binding := agentproto.NativeTurnBinding{Version: 1, Owner: owner, ExecutionID: "review-exec"}
	command := agentproto.Command{CommandID: "review", Kind: agentproto.CommandReviewStart, NativeBinding: &binding, Target: agentproto.Target{ThreadID: "parent"}}
	if _, _, err = s.PreparePrivateCommand(command); err != nil {
		t.Fatal(err)
	}
	event := agentproto.Event{Kind: agentproto.EventNativeControlResult, CommandID: "review", RequestID: "rpc", ThreadID: "parent", TurnID: "review-turn", Status: "accepted", Action: "review.start", Metadata: map[string]any{"reviewThreadID": "review-thread"}, NativeBinding: &binding}
	if _, err = s.PersistEvents([]agentproto.Event{event}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		thread, turn string
		want         bool
	}{{"review-thread", "review-turn", true}, {"parent", "review-turn", false}, {"review-thread", "wrong", false}} {
		got, e := s.PrivateBindingForTurn(tc.thread, tc.turn)
		if e != nil || (got != nil) != tc.want {
			t.Fatalf("thread=%s turn=%s binding=%+v err=%v", tc.thread, tc.turn, got, e)
		}
	}
}
