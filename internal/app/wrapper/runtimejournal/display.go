package runtimejournal

import (
	"maps"
	"strings"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

type displayTurnKey struct{ session, turn string }

type displayEvidence struct {
	usage  *agentproto.ThreadTokenUsage
	text   map[string]string
	public map[string]*displaySummary
}

type displaySummary struct {
	parts map[int]string
	sent  [maxUnfinishedDisplayItems]int
	last  time.Time
}

const maxUnfinishedDisplayItems = 32
const maxUnfinishedDisplayBytes = 1 << 20

// Display evidence is compacted in memory before the corresponding durable
// item or terminal event. Neither individual text deltas nor usage counters
// accumulate rows in the runtime journal.
func (s *Store) foldDisplayEvidence(event agentproto.Event) agentproto.Event {
	if event.Journal != nil || event.ThreadID == "" || event.TurnID == "" || event.TrafficClass == agentproto.TrafficClassInternalHelper {
		return event
	}
	key := displayTurnKey{event.ThreadID, event.TurnID}
	var evidence *displayEvidence
	switch event.Kind {
	case agentproto.EventThreadTokenUsageUpdated:
		if event.TokenUsage != nil {
			s.displayFor(key).usage = agentproto.CloneThreadTokenUsage(event.TokenUsage)
		}
	case agentproto.EventItemDelta:
		if event.ItemID != "" && event.ItemKind == "agent_message" {
			evidence = s.displayFor(key)
			appendDisplayText(evidence.text, event.ItemID, event.Delta)
		}
		public, _ := event.Metadata["publicReasoningSummary"].(bool)
		if event.ItemID != "" && event.ItemKind == "reasoning_summary" && public {
			evidence = s.displayFor(key)
			summary := evidence.public[event.ItemID]
			if summary == nil && len(evidence.public) < maxUnfinishedDisplayItems {
				summary = &displaySummary{parts: make(map[int]string)}
				evidence.public[event.ItemID] = summary
			}
			index := xutil.LookupIntFromAny(event.Metadata["summaryIndex"])
			if summary != nil && index >= 0 && index < maxUnfinishedDisplayItems && len(summary.parts[index])+len(event.Delta) <= maxUnfinishedDisplayBytes/maxUnfinishedDisplayItems {
				event.Metadata = maps.Clone(event.Metadata)
				event.Metadata["publicSummaryOffset"] = len(summary.parts[index])
				summary.parts[index] += event.Delta
			}
		}
	case agentproto.EventItemCompleted:
		event = s.freezeImageOutput(event)
		evidence = s.display[key]
		if evidence == nil {
			break
		}
		switch event.ItemKind {
		case "agent_message":
			event = attachDisplayValue(event, "text", evidence.text[event.ItemID])
		}
	case agentproto.EventTurnCompleted:
		evidence = s.display[key]
		if evidence != nil && event.TokenUsage == nil {
			event.TokenUsage = evidence.usage
		}
	}
	return event
}

func (s *Store) discardCommittedDisplayEvidence(events []agentproto.Event) {
	for _, event := range events {
		key := displayTurnKey{event.ThreadID, event.TurnID}
		evidence := s.display[key]
		if evidence == nil {
			continue
		}
		switch event.Kind {
		case agentproto.EventTurnCompleted:
			delete(s.display, key)
		case agentproto.EventItemCompleted:
			delete(evidence.text, event.ItemID)
			delete(evidence.public, event.ItemID)
		}
	}
}

func (s *Store) displayFor(key displayTurnKey) *displayEvidence {
	if s.display == nil {
		s.display = make(map[displayTurnKey]*displayEvidence)
	}
	if evidence := s.display[key]; evidence != nil {
		return evidence
	}
	if len(s.display) >= 4 {
		// Managed workers run one native turn at a time. Keep a small bound
		// for orphan events whose matching completion never reached us.
		for oldest := range s.display {
			delete(s.display, oldest)
			break
		}
	}
	evidence := &displayEvidence{text: make(map[string]string), public: make(map[string]*displaySummary)}
	s.display[key] = evidence
	return evidence
}

func appendDisplayText(items map[string]string, itemID, value string) {
	old, exists := items[itemID]
	if !exists && len(items) >= maxUnfinishedDisplayItems {
		return
	}
	if len(old)+len(value) <= maxUnfinishedDisplayBytes {
		items[itemID] = old + value
	}
}

func attachDisplayValue(event agentproto.Event, key, value string) agentproto.Event {
	if value == "" || xutil.MetadataString(event.Metadata, key) != "" {
		return event
	}
	event.Metadata = maps.Clone(event.Metadata)
	if event.Metadata == nil {
		event.Metadata = make(map[string]any)
	}
	event.Metadata[key] = value
	return event
}

// publicSummaryEvents emits complete windows rather than writing every native
// token to either journal. The terminal item flushes any trailing public text.
func (s *Store) publicSummaryEvents(event *agentproto.Event, now time.Time) ([]agentproto.Event, func()) {
	if event.Journal != nil || event.ItemKind != "reasoning_summary" || event.TrafficClass == agentproto.TrafficClassInternalHelper {
		return nil, nil
	}
	complete := event.Kind == agentproto.EventItemCompleted
	public, _ := event.Metadata["publicReasoningSummary"].(bool)
	if !complete && (event.Kind != agentproto.EventItemDelta || !public) {
		return nil, nil
	}
	evidence := s.display[displayTurnKey{event.ThreadID, event.TurnID}]
	if evidence == nil {
		return nil, nil
	}
	summary := evidence.public[event.ItemID]
	if summary == nil || !complete && now.Sub(summary.last) < time.Second {
		return nil, nil
	}
	oldSent, oldLast := summary.sent, summary.last
	var frames []agentproto.Event
	for i := 0; i < maxUnfinishedDisplayItems; i++ {
		text := summary.parts[i]
		if trailing := text[summary.sent[i]:]; strings.TrimSpace(trailing) != "" {
			summary.sent[i] = len(text)
			frames = append(frames, agentproto.Event{
				Kind: agentproto.EventItemReasoningSummaryPartAdded, ItemKind: "reasoning_summary", ItemID: event.ItemID,
				ThreadID: event.ThreadID, TurnID: event.TurnID, CommandID: event.CommandID, Execution: event.Execution,
				TrafficClass: event.TrafficClass, Initiator: event.Initiator, Delta: trailing,
				Metadata: map[string]any{"publicReasoningSummary": true, "summaryIndex": i, "publicSummaryOffset": oldSent[i]},
			})
		}
	}
	if len(frames) > 0 {
		summary.last = now
	}
	if complete && !summary.last.IsZero() {
		event.Metadata = maps.Clone(event.Metadata)
		if event.Metadata == nil {
			event.Metadata = make(map[string]any)
		}
		event.Metadata["publicSummaryStreamed"] = true
	}
	return frames, func() { summary.sent, summary.last = oldSent, oldLast }
}
