package codex

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestObserveServerTurnDiffUpdatedProducesCanonicalEvent(t *testing.T) {
	tr := NewTranslator("inst-1")

	if _, err := tr.ObserveServer([]byte(`{"method":"turn/started","params":{"threadId":"thread-1","turnId":"turn-1"}}`)); err != nil {
		t.Fatalf("observe turn started: %v", err)
	}

	result, err := tr.ObserveServer([]byte(`{"method":"turn/diff/updated","params":{"threadId":"thread-1","turnId":"turn-1","diff":"@@ -1 +1 @@\n-old\n+new"}}`))
	if err != nil {
		t.Fatalf("observe turn diff updated: %v", err)
	}
	if len(result.Events) != 1 {
		t.Fatalf("expected one diff event, got %#v", result.Events)
	}
	event := result.Events[0]
	if event.Kind != agentproto.EventTurnDiffUpdated {
		t.Fatalf("unexpected event kind: %#v", event)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" {
		t.Fatalf("unexpected diff identity: %#v", event)
	}
	if event.TurnDiff != "@@ -1 +1 @@\n-old\n+new" {
		t.Fatalf("unexpected diff payload: %#v", event)
	}
}
