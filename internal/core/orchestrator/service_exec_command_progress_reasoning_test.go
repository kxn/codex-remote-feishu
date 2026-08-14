package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	execprogress "github.com/kxn/codex-remote-feishu/internal/core/orchestrator/execprogress"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestReasoningSummaryProgressChattyEmitsEnglishTimelineEntry(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Considering Git commands**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(events) != 1 || events[0].Kind != eventcontract.KindExecCommandProgress || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected one reasoning progress event, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "reasoning_summary" || progress.Timeline[0].Summary != "Considering Git commands" {
		t.Fatalf("expected reasoning timeline item, got %#v", progress.Timeline)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress == nil {
		t.Fatal("expected reasoning to retain shared progress state")
	}
	record := svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning
	if record == nil || record.Text != "Considering Git commands" {
		t.Fatalf("expected reasoning record to keep raw english text, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning)
	}
}

func TestReasoningSummaryProgressChattyAccumulatesPlainTextDeltas(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Considering",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first reasoning delta event, got %#v", first)
	}
	if got := first[0].ExecCommandProgress.Timeline[0].Summary; got != "Considering" {
		t.Fatalf("expected first reasoning frame to surface first fragment, got %#v", first[0].ExecCommandProgress.Timeline)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " possible fixes",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(second) != 0 {
		t.Fatalf("expected second reasoning delta inside throttle window to be coalesced, got %#v", second)
	}
	if record := svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning; record == nil || record.Text != "Considering possible fixes" {
		t.Fatalf("expected reasoning record to keep accumulated plain-text summary, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning)
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")
	now = now.Add(execCommandProgressReasoningFlushInterval)
	tick := svc.Tick(now)
	if len(tick) != 1 || tick[0].ExecCommandProgress == nil {
		t.Fatalf("expected tick to flush coalesced reasoning delta after throttle window, got %#v", tick)
	}
	progress := tick[0].ExecCommandProgress
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected tick flush to update existing progress card, got %#v", progress)
	}
	if got := progress.Timeline[0].Summary; got != "Considering possible fixes" {
		t.Fatalf("expected coalesced reasoning summary on tick flush, got %#v", progress.Timeline)
	}
}

func TestReasoningSummaryProgressChattyKeepsCheckingPhraseInEnglish(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Checking workflow progress**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected english checking progress event, got %#v", events)
	}
	if len(events[0].ExecCommandProgress.Timeline) != 1 || events[0].ExecCommandProgress.Timeline[0].Summary != "Checking workflow progress" {
		t.Fatalf("expected checking phrase to stay in english timeline, got %#v", events[0].ExecCommandProgress.Timeline)
	}
}

func TestReasoningSummaryProgressChattyKeepsDifferentSummaryIndexesAsTimelineRows(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Reviewing existing flow",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first reasoning summary event, got %#v", first)
	}
	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning a safer update",
		Metadata: map[string]any{
			"summaryIndex": 2,
		},
	})
	if len(second) != 0 {
		t.Fatalf("expected second reasoning summary inside throttle window to be coalesced, got %#v", second)
	}
	intermediate := execprogress.Snapshot(surface.ActiveExecProgress).Timeline
	if len(intermediate) != 2 || intermediate[0].Status != "completed" || intermediate[1].Status != "running" {
		t.Fatalf("expected a new summary index to freeze the previous row, got %#v", intermediate)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")
	now = now.Add(execCommandProgressReasoningFlushInterval)
	tick := svc.Tick(now)
	if len(tick) != 1 || tick[0].ExecCommandProgress == nil {
		t.Fatalf("expected tick to flush second reasoning row, got %#v", tick)
	}
	timeline := tick[0].ExecCommandProgress.Timeline
	if len(timeline) != 2 ||
		timeline[0].Kind != "reasoning_summary" ||
		timeline[0].Summary != "Reviewing existing flow" ||
		timeline[1].Kind != "reasoning_summary" ||
		timeline[1].Summary != "Planning a safer update" {
		t.Fatalf("expected separate summary indexes to persist as separate timeline rows, got %#v", timeline)
	}
}

func TestReasoningSummaryProgressChattyDoesNotAnimateWithoutNewDelta(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 6, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Thinking**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning timeline event, got %#v", first)
	}
	if len(first[0].ExecCommandProgress.Timeline) != 1 || first[0].ExecCommandProgress.Timeline[0].Summary != "Thinking" {
		t.Fatalf("expected reasoning timeline to keep raw text, got %#v", first[0].ExecCommandProgress.Timeline)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	now = now.Add(10 * time.Second)
	if tick := svc.Tick(now); len(tick) != 0 {
		t.Fatalf("expected no synthetic reasoning animation update, got %#v", tick)
	}
}

func TestReasoningSummaryProgressChattyClaudeKeepsRawThinkingInsteadOfFirstBold(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**prefix** raw thinking continues",
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected Claude reasoning progress event, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Summary != "**prefix** raw thinking continues" {
		t.Fatalf("expected Claude reasoning to keep raw text, got %#v", progress.Timeline)
	}
}

func TestReasoningSummaryProgressChattyClaudeAccumulatesWithoutSummaryIndex(t *testing.T) {
	now := time.Date(2026, 5, 2, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty
	surface.Backend = agentproto.BackendClaude
	svc.root.Instances["inst-1"].Backend = agentproto.BackendClaude

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Before ",
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected first Claude reasoning event, got %#v", first)
	}
	if got := first[0].ExecCommandProgress.Timeline[0].Summary; got != "Before" {
		t.Fatalf("expected first Claude reasoning fragment, got %#v", first[0].ExecCommandProgress.Timeline)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "after",
	})
	if len(second) != 0 {
		t.Fatalf("expected coalesced Claude reasoning delta inside throttle window, got %#v", second)
	}
	record := svc.root.Surfaces["surface-1"].ActiveExecProgress.Reasoning
	if record == nil || record.Text != "Before after" {
		t.Fatalf("expected Claude reasoning record to accumulate plain text without summaryIndex, got %#v", record)
	}

	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")
	now = now.Add(execCommandProgressReasoningFlushInterval)
	tick := svc.Tick(now)
	if len(tick) != 1 || tick[0].ExecCommandProgress == nil {
		t.Fatalf("expected tick to flush accumulated Claude reasoning, got %#v", tick)
	}
	if got := tick[0].ExecCommandProgress.Timeline[0].Summary; got != "Before after" {
		t.Fatalf("expected accumulated Claude reasoning summary, got %#v", tick[0].ExecCommandProgress.Timeline)
	}
}

func TestReasoningSummaryProgressChattyStartsNewSegmentAfterOrdinaryProgress(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	coalesced := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " changes",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(coalesced) != 0 {
		t.Fatalf("expected dirty reasoning delta to be coalesced before ordinary progress, got %#v", coalesced)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected ordinary progress update, got %#v", second)
	}
	progress := second[0].ExecCommandProgress
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected ordinary progress to reuse the same card, got %#v", progress)
	}
	if len(progress.Timeline) != 2 ||
		progress.Timeline[0].Kind != "reasoning_summary" ||
		progress.Timeline[0].Summary != "Planning changes" ||
		progress.Timeline[1].Kind != "command_execution" ||
		progress.Timeline[1].Summary != "npm test" {
		t.Fatalf("expected reasoning timeline entry to persist before command progress, got %#v", progress.Timeline)
	}

	now = now.Add(execCommandProgressReasoningFlushInterval)
	third := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Checking results**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(third) != 1 || third[0].ExecCommandProgress == nil {
		t.Fatalf("expected reasoning after a tool to emit a new active segment, got %#v", third)
	}
	timeline := third[0].ExecCommandProgress.Timeline
	if len(timeline) != 3 ||
		timeline[0].Kind != "reasoning_summary" ||
		timeline[0].Summary != "Planning changes" ||
		timeline[0].Status != "completed" ||
		timeline[1].Kind != "command_execution" ||
		timeline[2].Kind != "reasoning_summary" ||
		timeline[2].Summary != "Checking results" ||
		timeline[2].Status != "running" {
		t.Fatalf("expected reasoning-tool-reasoning history in arrival order, got %#v", timeline)
	}
}

func TestReasoningSummaryProgressChattyPersistsBeforeExplorationRows(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}

	second := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "cat docs/README.md",
		},
	})
	if len(second) != 1 || second[0].ExecCommandProgress == nil {
		t.Fatalf("expected exploration progress update, got %#v", second)
	}
	timeline := second[0].ExecCommandProgress.Timeline
	if len(timeline) != 2 ||
		timeline[0].Kind != "reasoning_summary" ||
		timeline[0].Summary != "Planning" ||
		timeline[1].Kind != "read" ||
		len(timeline[1].Items) != 1 ||
		timeline[1].Items[0] != "docs/README.md" {
		t.Fatalf("expected reasoning timeline entry to persist before exploration row, got %#v", timeline)
	}
}

func TestReasoningSummaryProgressChattySealsAtFirstAssistantTextDelta(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 20, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Thinking",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	coalesced := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " about response shape",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(coalesced) != 0 {
		t.Fatalf("expected dirty reasoning delta before assistant text to be coalesced, got %#v", coalesced)
	}

	started := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	})
	if len(started) != 0 {
		t.Fatalf("expected assistant message start not to retract reasoning progress, got %#v", started)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress == nil {
		t.Fatal("expected progress state to remain until assistant text is emitted")
	}

	boundary := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Delta:    "先给你结论。",
	})
	if len(boundary) != 1 || boundary[0].Kind != eventcontract.KindExecCommandProgress || boundary[0].ExecCommandProgress == nil {
		t.Fatalf("expected first assistant text delta to flush and seal reasoning progress, got %#v", boundary)
	}
	if progress := boundary[0].ExecCommandProgress; activeProgressMessageID(progress) != "om-progress-1" ||
		len(progress.Timeline) != 1 ||
		progress.Timeline[0].Summary != "Thinking about response shape" {
		t.Fatalf("expected content boundary to emit the latest reasoning snapshot, got %#v", progress)
	}
	if events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	}); len(events) != 0 {
		t.Fatalf("expected assistant text completion to stay pending until next visible event, got %#v", events)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatal("expected first assistant text delta to seal shared progress state")
	}

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-1",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(events) != 1 || events[0].Kind != eventcontract.KindBlockCommitted || events[0].Block == nil {
		t.Fatalf("expected later tool activity to flush only the pending assistant block, got %#v", events)
	}
	if events[0].Block.Text != "先给你结论。" {
		t.Fatalf("unexpected assistant text block flush: %#v", events[0].Block)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected visible assistant text flush to terminate shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestReasoningSummaryProgressChattyPersistsOnTurnCompletion(t *testing.T) {
	now := time.Date(2026, 4, 17, 10, 30, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Planning",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	coalesced := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    " final answer",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(coalesced) != 0 {
		t.Fatalf("expected dirty reasoning delta before turn completion to be coalesced, got %#v", coalesced)
	}

	finished := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:      agentproto.EventTurnCompleted,
		ThreadID:  "thread-1",
		TurnID:    "turn-1",
		Status:    "completed",
		Initiator: agentproto.Initiator{Kind: agentproto.InitiatorUnknown},
	})
	var progressEvent *control.ExecCommandProgress
	for _, event := range finished {
		if event.Kind == eventcontract.KindExecCommandProgress {
			progressEvent = event.ExecCommandProgress
			break
		}
	}
	if progressEvent == nil {
		t.Fatalf("expected turn completion to finalize reasoning progress, got %#v", finished)
	}
	progress := progressEvent
	if activeProgressMessageID(progress) != "om-progress-1" {
		t.Fatalf("expected final progress snapshot on completion, got %#v", progress)
	}
	if len(progress.Timeline) != 1 ||
		progress.Timeline[0].Kind != "reasoning_summary" ||
		progress.Timeline[0].Summary != "Planning final answer" ||
		progress.Timeline[0].Status != "completed" {
		t.Fatalf("expected reasoning entry to persist and finalize on completion, got %#v", progress.Timeline)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected turn completion to clear shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
}

func TestReasoningSummaryProgressVerboseShowsLatestRealSummary(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	events := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "**Considering Git commands**",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(events) != 1 || events[0].ExecCommandProgress == nil {
		t.Fatalf("expected one reasoning progress event, got %#v", events)
	}
	progress := events[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "reasoning_summary" || progress.Timeline[0].Summary != "Considering Git commands" {
		t.Fatalf("expected verbose reasoning to project the latest real summary, got %#v", progress.Timeline)
	}
	active := svc.root.Surfaces["surface-1"].ActiveExecProgress
	if active == nil || len(active.Entries) != 0 || active.Reasoning == nil || active.Reasoning.Text != "Considering Git commands" {
		t.Fatalf("expected visible reasoning to stay outside ordinary progress entries, got %#v", active)
	}
	if reasoning := svc.root.Surfaces["surface-1"].ActiveReasoning; reasoning == nil || reasoning.Reasoning == nil || !reasoning.Reasoning.Active {
		t.Fatalf("expected surface reasoning state to stay active, got %#v", svc.root.Surfaces["surface-1"].ActiveReasoning)
	}
}

func TestReasoningSummaryProgressVerboseDoesNotReattachAfterContentBoundary(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Thinking",
		Metadata: map[string]any{
			"summaryIndex": 1,
		},
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning progress event, got %#v", first)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	_ = svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	})
	_ = svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
		Delta:    "先给你结论。",
	})
	_ = svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "msg-1",
		ItemKind: "agent_message",
	})

	flushed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-flush",
		ItemKind: "command_execution",
		Status:   "completed",
		Metadata: map[string]any{
			"command": "npm test",
		},
	})
	if len(flushed) != 1 || flushed[0].Block == nil {
		t.Fatalf("expected assistant text flush to seal the current progress card, got %#v", flushed)
	}
	if svc.root.Surfaces["surface-1"].ActiveExecProgress != nil {
		t.Fatalf("expected assistant text flush to terminate shared progress state, got %#v", svc.root.Surfaces["surface-1"].ActiveExecProgress)
	}
	if svc.root.Surfaces["surface-1"].ActiveReasoning != nil {
		t.Fatal("expected content boundary to clear old visible reasoning ownership")
	}

	next := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemStarted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "cmd-2",
		ItemKind: "command_execution",
		Metadata: map[string]any{
			"command": "go test ./...",
		},
	})
	if len(next) != 1 || next[0].ExecCommandProgress == nil {
		t.Fatalf("expected new shared progress to reopen after interruption, got %#v", next)
	}
	progress := next[0].ExecCommandProgress
	if len(progress.Timeline) != 1 || progress.Timeline[0].Kind != "command_execution" {
		t.Fatalf("expected the new progress card to contain only post-boundary activity, got %#v", progress.Timeline)
	}
}

func TestReasoningSummaryProgressVerboseCompletionKeepsRealSummaryCompleted(t *testing.T) {
	now := time.Date(2026, 5, 4, 10, 10, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose

	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Thinking",
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected initial reasoning placeholder event, got %#v", first)
	}
	svc.RecordExecCommandProgressSegment("surface-1", "thread-1", "turn-1", "reasoning-1", "om-progress-1")

	completed := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemCompleted,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Status:   "completed",
	})
	if len(completed) != 1 || completed[0].ExecCommandProgress == nil {
		t.Fatalf("expected reasoning completion to emit one progress update, got %#v", completed)
	}
	progress := completed[0].ExecCommandProgress
	if len(progress.Timeline) != 1 ||
		progress.Timeline[0].Kind != "reasoning_summary" ||
		progress.Timeline[0].Summary != "Thinking" ||
		progress.Timeline[0].Status != "completed" {
		t.Fatalf("expected verbose completion to retain the real summary without a running state, got %#v", progress)
	}
	if svc.root.Surfaces["surface-1"].ActiveReasoning != nil {
		t.Fatalf("expected active reasoning state to clear on completion, got %#v", svc.root.Surfaces["surface-1"].ActiveReasoning)
	}
}

func TestReasoningVerbositySwitchKeepsExistingChattyHistoryAndUsesVerboseTailForFuture(t *testing.T) {
	now := time.Date(2026, 8, 12, 16, 0, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Chatty history",
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected chatty reasoning, got %#v", first)
	}
	surface.Verbosity = state.SurfaceVerbosityVerbose
	now = now.Add(execCommandProgressReasoningFlushInterval)
	next := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Verbose future",
	})
	if len(next) != 0 {
		t.Fatalf("expected throttled reasoning without a delivered card, got %#v", next)
	}
	timeline := execprogress.Snapshot(surface.ActiveExecProgress).Timeline
	if len(timeline) != 2 || timeline[0].Summary != "Chatty history" || timeline[0].Status != "completed" || timeline[1].Summary != "Verbose future" || timeline[1].Status != "running" {
		t.Fatalf("expected preserved chatty history plus future verbose tail, got %#v", timeline)
	}
}

func TestReasoningVerbositySwitchKeepsVerboseSlotWhenFutureBecomesChatty(t *testing.T) {
	now := time.Date(2026, 8, 12, 16, 5, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityVerbose
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Verbose history",
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected verbose reasoning, got %#v", first)
	}
	surface.Verbosity = state.SurfaceVerbosityChatty
	now = now.Add(execCommandProgressReasoningFlushInterval)
	next := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Chatty future",
	})
	if len(next) != 0 {
		t.Fatalf("expected throttled reasoning without a delivered card, got %#v", next)
	}
	timeline := execprogress.Snapshot(surface.ActiveExecProgress).Timeline
	if len(timeline) != 2 || timeline[0].Summary != "Verbose history" || timeline[0].Status != "completed" || timeline[1].Summary != "Chatty future" || timeline[1].Status != "running" {
		t.Fatalf("expected preserved verbose slot plus future chatty segment, got %#v", timeline)
	}
}

func TestReasoningHiddenVerbosityDoesNotReplayEarlierContent(t *testing.T) {
	for _, hidden := range []state.SurfaceVerbosity{state.SurfaceVerbosityQuiet, state.SurfaceVerbosityNormal} {
		t.Run(string(hidden), func(t *testing.T) {
			now := time.Date(2026, 8, 12, 16, 10, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			surface := setupAutoWhipSurface(t, svc)
			surface.Verbosity = hidden
			startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

			hiddenEvents := svc.ApplyAgentEvent("inst-1", agentproto.Event{
				Kind:     agentproto.EventItemDelta,
				ThreadID: "thread-1",
				TurnID:   "turn-1",
				ItemID:   "reasoning-1",
				ItemKind: "reasoning_summary",
				Delta:    "Hidden history",
			})
			if len(hiddenEvents) != 0 || surface.ActiveExecProgress != nil {
				t.Fatalf("expected %s reasoning to stay invisible, events=%#v progress=%#v", hidden, hiddenEvents, surface.ActiveExecProgress)
			}

			surface.Verbosity = state.SurfaceVerbosityVerbose
			visible := svc.ApplyAgentEvent("inst-1", agentproto.Event{
				Kind:     agentproto.EventItemDelta,
				ThreadID: "thread-1",
				TurnID:   "turn-1",
				ItemID:   "reasoning-1",
				ItemKind: "reasoning_summary",
				Delta:    "Visible future",
			})
			if len(visible) != 1 || visible[0].ExecCommandProgress == nil {
				t.Fatalf("expected future verbose reasoning, got %#v", visible)
			}
			timeline := visible[0].ExecCommandProgress.Timeline
			if len(timeline) != 1 || timeline[0].Summary != "Visible future" || strings.Contains(timeline[0].Summary, "Hidden history") {
				t.Fatalf("expected no replay from hidden verbosity, got %#v", timeline)
			}
		})
	}
}

func TestReasoningSwitchToNormalFreezesEarlierVisibleContent(t *testing.T) {
	now := time.Date(2026, 8, 12, 16, 15, 0, 0, time.UTC)
	svc := newServiceForTest(&now)
	surface := setupAutoWhipSurface(t, svc)
	surface.Verbosity = state.SurfaceVerbosityChatty
	startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

	first := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Visible history",
	})
	if len(first) != 1 || first[0].ExecCommandProgress == nil {
		t.Fatalf("expected visible reasoning, got %#v", first)
	}

	surface.Verbosity = state.SurfaceVerbosityNormal
	hidden := svc.ApplyAgentEvent("inst-1", agentproto.Event{
		Kind:     agentproto.EventItemDelta,
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		ItemID:   "reasoning-1",
		ItemKind: "reasoning_summary",
		Delta:    "Hidden future",
	})
	if len(hidden) != 0 {
		t.Fatalf("expected normal reasoning to remain hidden, got %#v", hidden)
	}
	timeline := execprogress.Snapshot(surface.ActiveExecProgress).Timeline
	if len(timeline) != 1 || timeline[0].Summary != "Visible history" || timeline[0].Status != "completed" {
		t.Fatalf("expected earlier visible reasoning to freeze without hidden replay, got %#v", timeline)
	}
}
