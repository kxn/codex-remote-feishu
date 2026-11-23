package orchestrator

import (
	"encoding/json"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestPrivateProgressReceiptUpdatesFrozenEvidenceWithoutGoingLive(t *testing.T) {
	s := NewService(nil, Config{PrivateApplication: true}, nil)
	s.MaterializeSurface("s", "g", "c", "u")
	p := state.ExecCommandProgressRecord{ThreadID: "thread", TurnID: "turn", ItemID: "item"}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	s.privateRecoveryHolds["s"] = PrivateCheckpoint{Holds: []PrivateRecoveryHold{{Kind: "exec_progress", Frozen: raw}}}
	if err := s.RecordPrivateProgressDelivery("s", "thread", "turn", "item", "card", 1, 2, false); err != nil {
		t.Fatal(err)
	}
	if s.root.Surfaces["s"].ActiveExecProgress != nil {
		t.Fatal("receipt restored live progress")
	}
	var stored state.ExecCommandProgressRecord
	if err := json.Unmarshal(s.privateRecoveryHolds["s"].Holds[0].Frozen, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Segments) == 0 || stored.Segments[0].MessageID != "card" {
		t.Fatalf("receipt not frozen: %#v", stored)
	}
}
