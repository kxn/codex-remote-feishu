package orchestrator

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/control"
)

func TestPrivateProgressDisplayTargetUsesConfirmedSegment(t *testing.T) {
	previous := control.ExecCommandProgress{ThreadID: "thread", TurnID: "turn", ItemID: "one"}
	current := control.ExecCommandProgress{ThreadID: "thread", TurnID: "turn", ItemID: "two", Verbosity: "verbose"}
	resolved, err := ResolvePrivateProgressDisplayTarget(current, previous, "card-1", 1, 12)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.ItemID != "two" || resolved.Verbosity != "verbose" || len(resolved.Segments) != 1 || resolved.Segments[0].MessageID != "card-1" || resolved.Segments[0].EndSeq != 12 {
		t.Fatalf("target: %#v", resolved)
	}
	previous = resolved
	resolved, err = ResolvePrivateProgressDisplayTarget(current, previous, "card-2", 13, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Segments) != 2 || resolved.ActiveSegmentID != "segment-2" || resolved.Segments[0].EndSeq != 12 || resolved.Segments[1].MessageID != "card-2" {
		t.Fatalf("rollover: %#v", resolved)
	}
	current.TurnID = "other"
	if _, err = ResolvePrivateProgressDisplayTarget(current, previous, "card-2", 13, 20); err == nil {
		t.Fatal("cross-turn predecessor accepted")
	}
}
