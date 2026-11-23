package runtimejournal

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"path/filepath"
	"testing"
)

func TestPrivateLiveSequenceReservesAcrossRestart(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	cwd := t.TempDir()
	path := filepath.Join(root, "runtime.db")
	owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "i", HostID: "h", InstanceGeneration: 1, ProcessGeneration: 1, Backend: agentproto.BackendCodex, HistoryNamespace: "history"}
	snapshot := agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}
	open := func() *Store {
		s, err := Open(ctx, path, root, cwd, "inc")
		if err != nil {
			t.Fatal(err)
		}
		if err = s.BindPrivateLaunchSnapshot(snapshot); err != nil {
			t.Fatal(err)
		}
		return s
	}
	s := open()
	first, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventItemDelta, Delta: "a"}, {Kind: agentproto.EventItemDelta, Delta: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].Live == nil || first[0].Live.Sequence != 1 || first[1].Live.Sequence != 2 || first[0].Journal != nil || first[0].Seq != 0 {
		t.Fatalf("bad live identity: %+v", first)
	}
	hash, err := agentproto.HashRuntimeLiveEvent(first[0])
	if err != nil || hash != first[0].Live.PayloadHash {
		t.Fatal("invalid live hash")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	defer s.Close()
	second, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventItemDelta, Delta: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Live.Sequence != 1025 {
		t.Fatalf("reservation reused after restart: %+v", second[0].Live)
	}
	critical, err := s.PersistEvents([]agentproto.Event{{Kind: agentproto.EventTurnCompleted}})
	if err != nil {
		t.Fatal(err)
	}
	if critical[0].Journal == nil || critical[0].Journal.Sequence != 1 || critical[0].Live != nil {
		t.Fatal("critical sequence affected")
	}
}
