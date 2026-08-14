package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerReasoningCompletionBackfillsMissingSummaryDeltas(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"reason-1","type":"reasoning","summary":["**Planning the fix**","**Checking regressions**"],"content":[]}}}`))
	if err != nil {
		t.Fatalf("observe reasoning completion: %v", err)
	}
	if len(result.Events) != 3 {
		t.Fatalf("expected two fallback deltas and one completion, got %#v", result.Events)
	}
	for index, want := range []string{"**Planning the fix**", "**Checking regressions**"} {
		event := result.Events[index]
		if event.Kind != agentproto.EventItemDelta || event.ItemKind != "reasoning_summary" || event.Delta != want {
			t.Fatalf("unexpected fallback delta %d: %#v", index, event)
		}
		if got := event.Metadata["summaryIndex"]; got != index {
			t.Fatalf("fallback delta %d summaryIndex = %#v, want %d", index, got, index)
		}
	}
	completed := result.Events[2]
	if completed.Kind != agentproto.EventItemCompleted || completed.ItemKind != "reasoning_summary" || completed.ItemID != "reason-1" {
		t.Fatalf("unexpected canonical reasoning completion: %#v", completed)
	}
}

func TestObserveServerReasoningCompletionDoesNotRepeatObservedSummaryDelta(t *testing.T) {
	tr := NewTranslator("inst-1")

	delta, err := tr.ObserveServer([]byte(`{"method":"item/reasoning/summaryTextDelta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"reason-1","summaryIndex":0,"delta":"**Planning the fix**"}}`))
	if err != nil {
		t.Fatalf("observe reasoning delta: %v", err)
	}
	if len(delta.Events) != 1 || delta.Events[0].Kind != agentproto.EventItemDelta {
		t.Fatalf("unexpected reasoning delta events: %#v", delta.Events)
	}

	completed, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"reason-1","type":"reasoning","summary":["**Planning the fix**","**Checking regressions**"],"content":[]}}}`))
	if err != nil {
		t.Fatalf("observe reasoning completion: %v", err)
	}
	if len(completed.Events) != 2 {
		t.Fatalf("expected one missing fallback delta and one completion, got %#v", completed.Events)
	}
	fallback := completed.Events[0]
	if fallback.Kind != agentproto.EventItemDelta || fallback.ItemKind != "reasoning_summary" || fallback.Delta != "**Checking regressions**" || fallback.Metadata["summaryIndex"] != 1 {
		t.Fatalf("unexpected missing-summary fallback: %#v", fallback)
	}
	if event := completed.Events[1]; event.Kind != agentproto.EventItemCompleted || event.ItemKind != "reasoning_summary" {
		t.Fatalf("unexpected canonical reasoning completion: %#v", event)
	}
}

func TestObserveServerReasoningCompletionWithoutSummaryStillClosesCanonicalItem(t *testing.T) {
	tr := NewTranslator("inst-1")

	result, err := tr.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"reason-1","type":"reasoning","summary":[],"content":[]}}}`))
	if err != nil {
		t.Fatalf("observe reasoning completion: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one canonical completion, got %#v", result.Events)
	}
	if event := result.Events[0]; event.Kind != agentproto.EventItemCompleted || event.ItemKind != "reasoning_summary" {
		t.Fatalf("unexpected canonical reasoning completion: %#v", event)
	}
}

func TestObserveServerTurnCompletionClearsReasoningSummaryDedupState(t *testing.T) {
	tr := NewTranslator("inst-1")
	tr.markReasoningSummaryIndexSeen("thread-1", "turn-1", "reason-1", 0)

	_, err := tr.ObserveServer([]byte(`{"method":"turn/completed","params":{"threadId":"thread-1","turnId":"turn-1","turn":{"id":"turn-1","status":"completed"}}}`))
	if err != nil {
		t.Fatalf("observe turn completion: %v", err)
	}
	if tr.reasoningSummaryIndexSeen("thread-1", "turn-1", "reason-1", 0) {
		t.Fatal("reasoning summary dedup state survived turn completion")
	}
}
