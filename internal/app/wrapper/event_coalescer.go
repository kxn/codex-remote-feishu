package wrapper

import (
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

const (
	defaultRelayEventCoalescerMaxBytes  = 16 * 1024
	defaultRelayEventCoalescerMaxWindow = 75 * time.Millisecond
)

type relayEventCoalescer struct {
	now       func() time.Time
	maxBytes  int
	maxWindow time.Duration

	pending      *agentproto.Event
	pendingSince time.Time
}

func newRelayEventCoalescer(now func() time.Time, maxBytes int, maxWindow time.Duration) *relayEventCoalescer {
	if now == nil {
		now = time.Now
	}
	if maxBytes <= 0 {
		maxBytes = defaultRelayEventCoalescerMaxBytes
	}
	if maxWindow <= 0 {
		maxWindow = defaultRelayEventCoalescerMaxWindow
	}
	return &relayEventCoalescer{
		now:       now,
		maxBytes:  maxBytes,
		maxWindow: maxWindow,
	}
}

func (c *relayEventCoalescer) Push(events []agentproto.Event) []agentproto.Event {
	if len(events) == 0 {
		return nil
	}
	out := make([]agentproto.Event, 0, len(events)+1)
	for _, event := range events {
		if c.pending != nil && !c.canMerge(event) {
			out = append(out, c.flushOne())
		}
		if c.pending == nil && isCoalescibleRelayDelta(event) {
			pending := event
			c.pending = &pending
			c.pendingSince = c.now()
			continue
		}
		if c.pending != nil && c.canMerge(event) {
			c.pending.Delta += event.Delta
			continue
		}
		out = append(out, event)
	}
	return out
}

func (c *relayEventCoalescer) Flush() []agentproto.Event {
	if c.pending == nil {
		return nil
	}
	return []agentproto.Event{c.flushOne()}
}

func (c *relayEventCoalescer) flushOne() agentproto.Event {
	flushed := *c.pending
	c.pending = nil
	c.pendingSince = time.Time{}
	return flushed
}

func (c *relayEventCoalescer) canMerge(next agentproto.Event) bool {
	if c.pending == nil || !isCoalescibleRelayDelta(next) {
		return false
	}
	if c.maxWindow > 0 && !c.pendingSince.IsZero() && c.now().Sub(c.pendingSince) >= c.maxWindow {
		return false
	}
	if c.maxBytes > 0 && len(c.pending.Delta)+len(next.Delta) > c.maxBytes {
		return false
	}
	return sameCoalescibleRelayDelta(*c.pending, next)
}

func isCoalescibleRelayDelta(event agentproto.Event) bool {
	if event.Kind != agentproto.EventItemDelta || event.ItemID == "" {
		return false
	}
	if event.Seq != 0 ||
		event.RequestID != "" ||
		event.Status != "" ||
		event.ErrorMessage != "" ||
		event.CWD != "" ||
		event.FocusSource != "" ||
		event.Action != "" ||
		event.Name != "" ||
		event.Preview != "" ||
		event.Model != "" ||
		event.ReasoningEffort != "" ||
		event.ConfigScope != "" ||
		event.Loaded ||
		event.Archived ||
		event.Problem != nil ||
		len(event.Metadata) != 0 ||
		len(event.Threads) != 0 {
		return false
	}
	return true
}

func sameCoalescibleRelayDelta(left, right agentproto.Event) bool {
	return left.Kind == right.Kind &&
		left.ThreadID == right.ThreadID &&
		left.TurnID == right.TurnID &&
		left.ItemID == right.ItemID &&
		left.ItemKind == right.ItemKind &&
		left.TrafficClass == right.TrafficClass &&
		left.Initiator == right.Initiator
}
