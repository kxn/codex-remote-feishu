package runtimejournal

import (
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestPrivateDisplayPublicSummaryIsCoalescedAndFlushesBeforeCompletion(t *testing.T) {
	root, cwd := t.TempDir(), t.TempDir()
	store := openTestStore(t, filepath.Join(root, "journal.db"), root, cwd, "carrier-1")
	defer store.Close()

	public := agentproto.Event{
		Kind: agentproto.EventItemDelta, ThreadID: "session", TurnID: "turn", ItemID: "summary",
		ItemKind: "reasoning_summary", Delta: "Public first part.",
		Metadata: map[string]any{"publicReasoningSummary": true, "summaryIndex": 0},
	}
	first, err := store.PersistEvents([]agentproto.Event{public})
	if err != nil || len(first) != 2 || first[0].Journal == nil || first[0].Delta != public.Delta || first[1].Journal != nil {
		t.Fatalf("in-progress summary update was not independently durable: %+v, %v", first, err)
	}
	public.Delta = " Buffered continuation."
	hidden := public
	hidden.ItemKind, hidden.Delta, hidden.Metadata = "reasoning_content", "hidden native reasoning", nil
	next, err := store.PersistEvents([]agentproto.Event{public, hidden})
	if err != nil || len(next) != 2 || next[0].Journal != nil || next[1].Journal != nil {
		t.Fatalf("individual deltas reached the durable journal: %+v, %v", next, err)
	}
	complete := agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "session", TurnID: "turn", ItemID: "summary", ItemKind: "reasoning_summary"}
	end, err := store.PersistEvents([]agentproto.Event{complete})
	if err != nil || len(end) != 2 || end[0].Delta != public.Delta || end[0].Journal == nil || end[1].Journal == nil || end[1].Metadata["publicSummaryStreamed"] != true {
		t.Fatalf("trailing summary did not precede completed evidence: %+v, %v", end, err)
	}
	replay, _, err := store.Replay(0, 32)
	if err != nil || len(replay) != 3 {
		t.Fatalf("journal replay retained raw tokens or lost summaries: %+v, %v", replay, err)
	}
}
