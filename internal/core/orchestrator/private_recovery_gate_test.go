package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateRecoveryGateRequiresAllOtherFactsClear(t *testing.T) {
	for _, tc := range []struct {
		name    string
		held    PrivateCheckpoint
		unknown bool
	}{
		{"cleared", PrivateCheckpoint{}, false},
		{"unknown input", PrivateCheckpoint{Holds: []PrivateRecoveryHold{{Kind: "pending_remote"}}}, true},
		{"migration queue", PrivateCheckpoint{QueuedQueueItemIDs: []string{"q"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService(nil, Config{PrivateApplication: true}, nil)
			s.MaterializeSurface("s", "g", "chat", "user")
			if tc.name == "migration queue" {
				s.root.Surfaces["s"].QueuedQueueItemIDs = []string{"q"}
				s.root.Surfaces["s"].QueueItems = map[string]*state.QueueItemRecord{"q": {ID: "q", SurfaceSessionID: "s", Status: state.QueueItemQueued}}
			}
			s.privateRecoveryHolds["s"] = tc.held
			if _, err := s.ReconcilePrivateRecoveryGate("s", tc.name != "migration queue"); err != nil {
				t.Fatal(err)
			}
			if allowed := s.PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: "s", Kind: control.ActionTextMessage}); allowed == tc.unknown {
				t.Fatalf("allowed=%v unknown=%v", allowed, tc.unknown)
			}
		})
	}
	s := NewService(nil, Config{PrivateApplication: true}, nil)
	s.MaterializeSurface("s", "g", "chat", "user")
	s.root.Surfaces["s"].AutoContinue.Episode = &state.PendingAutoContinueEpisodeRecord{}
	s.privateRecoveryHolds["s"] = PrivateCheckpoint{}
	if _, err := s.ReconcilePrivateRecoveryGate("s", false); err != nil {
		t.Fatal(err)
	}
	if s.PrivateRecoveryActionAllowed(control.Action{SurfaceSessionID: "s", Kind: control.ActionTextMessage}) {
		t.Fatal("auto continuation still requires recovery")
	}
}
