package codex

import (
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/xutil"
)

func (t *Translator) observeItemCompleted(message map[string]any) Result {
	threadID := lookupString(message, "params", "threadId")
	turnID := lookupString(message, "params", "turnId")
	item := lookupMap(message, "params", "item")
	itemID := choose(
		xutil.LookupStringFromAny(item["id"]),
		lookupString(message, "params", "itemId"),
	)
	itemKind := normalizeItemKind(choose(
		xutil.LookupStringFromAny(item["type"]),
		lookupString(message, "params", "itemType"),
	))
	metadata := extractItemMetadata(itemKind, item)
	events := []agentproto.Event{}
	if itemKind == "reasoning" {
		events = append(events, t.reasoningSummaryFallbackEvents(threadID, turnID, itemID, item)...)
		itemKind = "reasoning_summary"
	}
	events = append(events, agentproto.Event{
		Kind:         agentproto.EventItemCompleted,
		ThreadID:     threadID,
		TurnID:       turnID,
		ItemID:       itemID,
		ItemKind:     itemKind,
		Status:       extractItemStatus(item),
		TrafficClass: t.trafficClassForTurn(threadID, turnID),
		Initiator:    t.initiatorForTurn(threadID, turnID),
		Metadata:     metadata,
		Exploration:  extractCommandExecutionExploration(itemKind, item),
		FileChanges:  extractFileChangeRecords(itemKind, item),
	})
	return Result{Events: events}
}

func (t *Translator) reasoningSummaryFallbackEvents(threadID, turnID, itemID string, item map[string]any) []agentproto.Event {
	events := []agentproto.Event{}
	for summaryIndex, summary := range extractStringList(item["summary"]) {
		if t.reasoningSummaryIndexSeen(threadID, turnID, itemID, summaryIndex) {
			continue
		}
		t.markReasoningSummaryIndexSeen(threadID, turnID, itemID, summaryIndex)
		events = append(events, agentproto.Event{
			Kind:         agentproto.EventItemDelta,
			ThreadID:     threadID,
			TurnID:       turnID,
			ItemID:       itemID,
			ItemKind:     "reasoning_summary",
			Delta:        summary,
			TrafficClass: t.trafficClassForTurn(threadID, turnID),
			Initiator:    t.initiatorForTurn(threadID, turnID),
			Metadata:     map[string]any{"summaryIndex": summaryIndex},
		})
	}
	return events
}
