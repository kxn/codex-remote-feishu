package wrapper

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestRelayEventCoalescerMergesAdjacentItemDelta(t *testing.T) {
	coalescer := newRelayEventCoalescer(func() time.Time { return time.Unix(0, 0) }, 1024, time.Second)
	first := agentproto.Event{
		Kind:         agentproto.EventItemDelta,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		ItemID:       "item-1",
		ItemKind:     "agent_message",
		Delta:        "hello",
		TrafficClass: agentproto.TrafficClassPrimary,
		Initiator: agentproto.Initiator{
			Kind:             agentproto.InitiatorRemoteSurface,
			SurfaceSessionID: "surface-1",
		},
	}
	second := first
	second.Delta = " world"

	if got := coalescer.Push([]agentproto.Event{first}); len(got) != 0 {
		t.Fatalf("expected first delta to stay buffered, got %#v", got)
	}
	if got := coalescer.Push([]agentproto.Event{second}); len(got) != 0 {
		t.Fatalf("expected second delta to merge into buffer, got %#v", got)
	}
	flushed := coalescer.Flush()
	if len(flushed) != 1 {
		t.Fatalf("expected one flushed event, got %#v", flushed)
	}
	if flushed[0].Delta != "hello world" {
		t.Fatalf("unexpected merged delta: %#v", flushed[0])
	}
}

func TestRelayEventCoalescerFlushesBeforeNonDeltaEvent(t *testing.T) {
	coalescer := newRelayEventCoalescer(func() time.Time { return time.Unix(0, 0) }, 1024, time.Second)
	delta := agentproto.Event{
		Kind:         agentproto.EventItemDelta,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		ItemID:       "item-1",
		ItemKind:     "agent_message",
		Delta:        "hello",
		TrafficClass: agentproto.TrafficClassPrimary,
	}
	completed := agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "item-1",
		ItemKind: "agent_message",
	}

	_ = coalescer.Push([]agentproto.Event{delta})
	got := coalescer.Push([]agentproto.Event{completed})
	if len(got) != 2 {
		t.Fatalf("expected buffered delta and completed event, got %#v", got)
	}
	if got[0].Kind != agentproto.EventItemDelta || got[0].Delta != "hello" {
		t.Fatalf("unexpected flushed delta: %#v", got[0])
	}
	if got[1].Kind != agentproto.EventItemCompleted {
		t.Fatalf("unexpected trailing event: %#v", got[1])
	}
}

func TestRelayEventCoalescerFlushesOnMergeKeyChange(t *testing.T) {
	coalescer := newRelayEventCoalescer(func() time.Time { return time.Unix(0, 0) }, 1024, time.Second)
	first := agentproto.Event{
		Kind:         agentproto.EventItemDelta,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		ItemID:       "item-1",
		ItemKind:     "agent_message",
		Delta:        "hello",
		TrafficClass: agentproto.TrafficClassPrimary,
	}
	second := first
	second.ItemID = "item-2"
	second.Delta = "world"

	_ = coalescer.Push([]agentproto.Event{first})
	got := coalescer.Push([]agentproto.Event{second})
	if len(got) != 1 {
		t.Fatalf("expected first delta to flush, got %#v", got)
	}
	if got[0].ItemID != "item-1" {
		t.Fatalf("unexpected flushed event: %#v", got[0])
	}
	flushed := coalescer.Flush()
	if len(flushed) != 1 || flushed[0].ItemID != "item-2" {
		t.Fatalf("expected second delta to remain buffered, got %#v", flushed)
	}
}

func TestRelayEventCoalescerFlushesOnSizeLimit(t *testing.T) {
	coalescer := newRelayEventCoalescer(func() time.Time { return time.Unix(0, 0) }, 8, time.Second)
	first := agentproto.Event{
		Kind:         agentproto.EventItemDelta,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		ItemID:       "item-1",
		ItemKind:     "command_execution_output",
		Delta:        "12345",
		TrafficClass: agentproto.TrafficClassPrimary,
	}
	second := first
	second.Delta = "6789"

	_ = coalescer.Push([]agentproto.Event{first})
	got := coalescer.Push([]agentproto.Event{second})
	if len(got) != 1 || got[0].Delta != "12345" {
		t.Fatalf("expected size-triggered flush, got %#v", got)
	}
	flushed := coalescer.Flush()
	if len(flushed) != 1 || flushed[0].Delta != "6789" {
		t.Fatalf("expected second delta to start new buffer, got %#v", flushed)
	}
}

func TestRelayEventCoalescerFlushesOnWindowLimit(t *testing.T) {
	current := time.Unix(0, 0)
	coalescer := newRelayEventCoalescer(func() time.Time { return current }, 1024, 50*time.Millisecond)
	first := agentproto.Event{
		Kind:         agentproto.EventItemDelta,
		ThreadID:     "thread-1",
		TurnID:       "turn-1",
		ItemID:       "item-1",
		ItemKind:     "plan",
		Delta:        "A",
		TrafficClass: agentproto.TrafficClassPrimary,
	}
	second := first
	second.Delta = "B"

	_ = coalescer.Push([]agentproto.Event{first})
	current = current.Add(51 * time.Millisecond)
	got := coalescer.Push([]agentproto.Event{second})
	if len(got) != 1 || got[0].Delta != "A" {
		t.Fatalf("expected window-triggered flush, got %#v", got)
	}
	flushed := coalescer.Flush()
	if len(flushed) != 1 || flushed[0].Delta != "B" {
		t.Fatalf("expected second delta to remain buffered after window flush, got %#v", flushed)
	}
}
