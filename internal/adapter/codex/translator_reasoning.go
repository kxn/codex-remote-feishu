package codex

import "strings"

const reasoningSummaryKeySeparator = "\x00"

func reasoningSummaryItemKey(threadID, turnID, itemID string) string {
	return strings.Join([]string{
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
		strings.TrimSpace(itemID),
	}, reasoningSummaryKeySeparator)
}

func (t *Translator) reasoningSummaryIndexSeen(threadID, turnID, itemID string, summaryIndex int) bool {
	if t == nil {
		return false
	}
	return t.reasoningSummaryIndexes[reasoningSummaryItemKey(threadID, turnID, itemID)][summaryIndex]
}

func (t *Translator) markReasoningSummaryIndexSeen(threadID, turnID, itemID string, summaryIndex int) {
	if t == nil {
		return
	}
	key := reasoningSummaryItemKey(threadID, turnID, itemID)
	if t.reasoningSummaryIndexes[key] == nil {
		t.reasoningSummaryIndexes[key] = map[int]bool{}
	}
	t.reasoningSummaryIndexes[key][summaryIndex] = true
}

func (t *Translator) clearReasoningSummaryIndexesForTurn(threadID, turnID string) {
	if t == nil {
		return
	}
	prefix := strings.Join([]string{
		strings.TrimSpace(threadID),
		strings.TrimSpace(turnID),
	}, reasoningSummaryKeySeparator) + reasoningSummaryKeySeparator
	for key := range t.reasoningSummaryIndexes {
		if strings.HasPrefix(key, prefix) {
			delete(t.reasoningSummaryIndexes, key)
		}
	}
}
