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

func TestPrivateTokenUsageIsDurableWhileTeamClassificationStaysUnchanged(t *testing.T) {
	for _, backend := range []agentproto.Backend{agentproto.BackendCodex, agentproto.BackendClaude, agentproto.BackendOpenCode} {
		t.Run(string(backend), func(t *testing.T) {
			ctx := t.Context()
			root := t.TempDir()
			cwd := t.TempDir()
			path := filepath.Join(root, "runtime.db")
			owner := agentproto.NativeOwner{Version: 1, Application: "private", OwnerID: "o", Epoch: 1, CarrierIncarnation: "inc", InstanceID: "i", HostID: "h", InstanceGeneration: 1, ProcessGeneration: 1, Backend: backend, HistoryNamespace: "history"}
			s, err := Open(ctx, path, root, cwd, "inc")
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if err = s.BindPrivateLaunchSnapshot(agentproto.PrivateNativeLaunchSnapshot{Version: 1, Owner: owner, CWD: cwd, JournalPath: path}); err != nil {
				t.Fatal(err)
			}
			event := agentproto.Event{Kind: agentproto.EventThreadTokenUsageUpdated, ThreadID: "session", TokenUsage: &agentproto.ThreadTokenUsage{Total: agentproto.TokenUsageBreakdown{TotalTokens: 500}}}
			if Critical(event) {
				t.Fatal("team token usage classification changed")
			}
			events, err := s.PersistEvents([]agentproto.Event{event})
			if err != nil || len(events) != 1 || events[0].Journal == nil || events[0].Seq != 1 || events[0].Live != nil {
				t.Fatalf("private usage not durable: %+v %v", events, err)
			}
		})
	}
}

func TestPrivateDeliveryClassCoversTeamNoncriticalAndEvidence(t *testing.T) {
	if Critical(agentproto.Event{Kind: agentproto.EventItemDelta}) || Critical(agentproto.Event{Kind: agentproto.EventThreadTokenUsageUpdated}) {
		t.Fatal("team noncritical classification changed")
	}
	if agentproto.PrivateDeliveryForEvent(agentproto.EventItemDelta) != agentproto.PrivateEventLive {
		t.Fatal("delta must use private live stream")
	}
	for _, kind := range []agentproto.EventKind{agentproto.EventThreadTokenUsageUpdated, agentproto.EventTurnPlanUpdated, agentproto.EventTurnDiffUpdated, agentproto.EventItemStarted, agentproto.EventItemReasoningSummaryPartAdded, agentproto.EventItemTerminalInteraction, agentproto.EventItemFileChangePatchUpdated, agentproto.EventItemCompleted, agentproto.EventTurnCompleted, agentproto.EventRequestStarted, agentproto.EventRequestResolved, agentproto.EventNativeCommandRejected, agentproto.EventNativeInputReceipt, agentproto.EventSystemError} {
		if agentproto.PrivateDeliveryForEvent(kind) != agentproto.PrivateEventDurable {
			t.Fatalf("%s must be durable", kind)
		}
	}
}
