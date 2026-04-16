package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/render"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestFinalCardLookupRetainsRecentTurnAnchors(t *testing.T) {
	now := time.Date(2026, 4, 16, 11, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"

	first := render.Block{
		InstanceID: "inst-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		ItemID:     "item-1",
		Final:      true,
	}
	second := render.Block{
		InstanceID: "inst-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-2",
		ItemID:     "item-2",
		Final:      true,
	}

	svc.RecordFinalCardMessage("surface-1", first, "msg-1", "om-final-1", "life-1")
	svc.RecordFinalCardMessage("surface-1", second, "msg-2", "om-final-2", "life-1")

	gotFirst := svc.LookupFinalCardForBlock("surface-1", first, "life-1")
	if gotFirst == nil || gotFirst.MessageID != "om-final-1" || gotFirst.SourceMessageID != "msg-1" {
		t.Fatalf("expected first final card anchor, got %#v", gotFirst)
	}
	gotSecond := svc.LookupFinalCardForBlock("surface-1", second, "life-1")
	if gotSecond == nil || gotSecond.MessageID != "om-final-2" || gotSecond.SourceMessageID != "msg-2" {
		t.Fatalf("expected second final card anchor, got %#v", gotSecond)
	}
}

func TestFinalCardLookupUsesNewestRecordForSameTurn(t *testing.T) {
	now := time.Date(2026, 4, 16, 11, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"

	block := render.Block{
		InstanceID: "inst-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		ItemID:     "item-1",
		Final:      true,
	}

	svc.RecordFinalCardMessage("surface-1", block, "msg-1", "om-final-1", "life-1")
	now = now.Add(2 * time.Second)
	svc.RecordFinalCardMessage("surface-1", block, "msg-1", "om-final-2", "life-1")

	got := svc.LookupFinalCardForBlock("surface-1", block, "life-1")
	if got == nil || got.MessageID != "om-final-2" {
		t.Fatalf("expected newest final card anchor for same turn, got %#v", got)
	}
	if len(surface.RecentFinalCards) != 1 {
		t.Fatalf("expected same-turn record to replace previous anchor, got %#v", surface.RecentFinalCards)
	}
}

func TestFinalCardLookupRejectsLifecycleMismatchAndClearsOnDetach(t *testing.T) {
	now := time.Date(2026, 4, 16, 11, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	svc.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surface := svc.root.Surfaces["surface-1"]
	surface.AttachedInstanceID = "inst-1"
	svc.root.Instances["inst-1"] = &state.InstanceRecord{InstanceID: "inst-1", Online: true, Threads: map[string]*state.ThreadRecord{}}

	block := render.Block{
		InstanceID: "inst-1",
		ThreadID:   "thread-1",
		TurnID:     "turn-1",
		ItemID:     "item-1",
		Final:      true,
	}

	svc.RecordFinalCardMessage("surface-1", block, "msg-1", "om-final-1", "life-1")

	if got := svc.LookupFinalCardForBlock("surface-1", block, "life-2"); got != nil {
		t.Fatalf("expected lifecycle mismatch to reject lookup, got %#v", got)
	}

	svc.finalizeDetachedSurface(surface)
	if got := svc.LookupFinalCardForBlock("surface-1", block, "life-1"); got != nil {
		t.Fatalf("expected detach to clear retained final cards, got %#v", got)
	}
}
