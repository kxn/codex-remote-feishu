package daemon

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/feishu"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/eventcontract"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func activeProgressMessageID(progress *state.ExecCommandProgressRecord) string {
	if progress == nil || len(progress.Segments) == 0 {
		return ""
	}
	if progress.ActiveSegmentID != "" {
		for _, segment := range progress.Segments {
			if segment.SegmentID == progress.ActiveSegmentID {
				return segment.MessageID
			}
		}
	}
	return progress.Segments[len(progress.Segments)-1].MessageID
}

func activeProgressStartSeq(progress *state.ExecCommandProgressRecord) int {
	if progress == nil || len(progress.Segments) == 0 {
		return 0
	}
	if progress.ActiveSegmentID != "" {
		for _, segment := range progress.Segments {
			if segment.SegmentID == progress.ActiveSegmentID {
				return segment.StartSeq
			}
		}
	}
	return progress.Segments[len(progress.Segments)-1].StartSeq
}

func progressEntryLastSeq(progress *state.ExecCommandProgressRecord, itemID string) int {
	if progress == nil {
		return 0
	}
	for _, entry := range progress.Entries {
		if entry.ItemID == itemID {
			return entry.LastSeq
		}
	}
	return 0
}

func TestRecordUIEventDeliveryTracksExecProgressPatchedWindowStart(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 {
		t.Fatalf("expected one surface, got %d", len(surfaces))
	}
	surface := surfaces[0]
	surface.ActiveExecProgress = &state.ExecCommandProgressRecord{
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		ItemID:          "cmd-3",
		ActiveSegmentID: "segment-1",
		Segments: []state.ExecCommandProgressSegmentRecord{{
			SegmentID: "segment-1",
			MessageID: "om-progress-1",
			StartSeq:  1,
		}},
		Status:        "running",
		LastEmittedAt: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
	}

	app.recordUIEventDelivery(eventcontract.Event{
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-3",
		},
	}, []feishu.Operation{
		{Kind: feishu.OperationUpdateCard, MessageID: "om-progress-1", ProgressCardStartSeq: 73},
	})

	if activeProgressMessageID(surface.ActiveExecProgress) != "om-progress-1" || activeProgressStartSeq(surface.ActiveExecProgress) != 73 {
		t.Fatalf("expected patched shared progress card to keep same message and advance visible window, got %#v", surface.ActiveExecProgress)
	}
}

func TestRecordUIEventDeliveryTracksExecProgressFromPayloadFirstEvent(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 4, 19, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 {
		t.Fatalf("expected one surface, got %d", len(surfaces))
	}
	surface := surfaces[0]
	surface.ActiveExecProgress = &state.ExecCommandProgressRecord{
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		ItemID:          "cmd-3",
		ActiveSegmentID: "segment-1",
		Segments: []state.ExecCommandProgressSegmentRecord{{
			SegmentID: "segment-1",
			MessageID: "om-progress-1",
			StartSeq:  1,
		}},
		Status:        "running",
		LastEmittedAt: time.Date(2026, 4, 19, 10, 1, 0, 0, time.UTC),
	}

	app.recordUIEventDelivery(eventcontract.Event{
		SurfaceSessionID: "surface-1",
		Payload: eventcontract.ExecCommandProgressPayload{
			Progress: control.ExecCommandProgress{
				ThreadID: "thread-1",
				TurnID:   "turn-1",
				ItemID:   "cmd-3",
			},
		},
	}, []feishu.Operation{
		{Kind: feishu.OperationUpdateCard, MessageID: "om-progress-1", ProgressCardStartSeq: 88},
	})

	if activeProgressMessageID(surface.ActiveExecProgress) != "om-progress-1" || activeProgressStartSeq(surface.ActiveExecProgress) != 88 {
		t.Fatalf("expected payload-first progress event to keep same message and advance visible window, got %#v", surface.ActiveExecProgress)
	}
}

func TestRecordUIEventDeliveryAppendsNewProgressSegmentAndRollsOverRunningEntries(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 {
		t.Fatalf("expected one surface, got %d", len(surfaces))
	}
	surface := surfaces[0]
	surface.ActiveExecProgress = &state.ExecCommandProgressRecord{
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		ItemID:          "cmd-latest",
		ActiveSegmentID: "segment-1",
		Segments: []state.ExecCommandProgressSegmentRecord{{
			SegmentID: "segment-1",
			MessageID: "om-progress-1",
			StartSeq:  1,
		}},
		Entries: []state.ExecCommandProgressEntryRecord{
			{ItemID: "cmd-active", Kind: "command_execution", Summary: "go test ./active", Status: "running", LastSeq: 2},
			{ItemID: "cmd-latest", Kind: "command_execution", Summary: "go test ./latest", Status: "completed", LastSeq: 120},
		},
		LastVisibleSeq: 120,
		Status:         "running",
		LastEmittedAt:  time.Date(2026, 5, 2, 10, 1, 0, 0, time.UTC),
	}

	app.recordUIEventDelivery(eventcontract.Event{
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "cmd-latest",
		},
	}, []feishu.Operation{
		{Kind: feishu.OperationSendCard, MessageID: "om-progress-2", ProgressCardStartSeq: 90, ProgressCardEndSeq: 120},
	})

	progress := surface.ActiveExecProgress
	if progress == nil {
		t.Fatal("expected active progress to remain after segment rollover")
	}
	if len(progress.Segments) != 2 {
		t.Fatalf("expected a new progress segment to be recorded, got %#v", progress.Segments)
	}
	if progress.ActiveSegmentID != "segment-2" || activeProgressMessageID(progress) != "om-progress-2" || activeProgressStartSeq(progress) != 90 {
		t.Fatalf("expected second segment to become active, got %#v", progress)
	}
	if progress.Segments[0].EndSeq != 89 {
		t.Fatalf("expected previous segment to seal before the new start seq, got %#v", progress.Segments)
	}
	if seq := progressEntryLastSeq(progress, "cmd-active"); seq <= 120 {
		t.Fatalf("expected running entry to roll into the new segment, got seq=%d progress=%#v", seq, progress)
	}
}

func TestRecordUIEventDeliveryClearsDeletedExecProgressSegmentAndAllowsReuse(t *testing.T) {
	app := New(":0", ":0", &recordingGateway{}, agentproto.ServerIdentity{
		PID:       42,
		StartedAt: time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC),
	})
	app.service.MaterializeSurface("surface-1", "app-1", "chat-1", "user-1")
	surfaces := app.service.Surfaces()
	if len(surfaces) != 1 {
		t.Fatalf("expected one surface, got %d", len(surfaces))
	}
	surface := surfaces[0]
	surface.ActiveExecProgress = &state.ExecCommandProgressRecord{
		ThreadID:        "thread-1",
		TurnID:          "turn-1",
		ItemID:          "reasoning-1",
		ActiveSegmentID: "segment-1",
		Segments: []state.ExecCommandProgressSegmentRecord{{
			SegmentID: "segment-1",
			MessageID: "om-progress-1",
			StartSeq:  1,
		}},
		Status:        "running",
		LastEmittedAt: time.Date(2026, 5, 4, 10, 1, 0, 0, time.UTC),
	}
	event := eventcontract.Event{
		SurfaceSessionID: "surface-1",
		ExecCommandProgress: &control.ExecCommandProgress{
			ThreadID: "thread-1",
			TurnID:   "turn-1",
			ItemID:   "reasoning-1",
		},
	}

	app.recordUIEventDelivery(event, []feishu.Operation{
		{Kind: feishu.OperationDeleteMessage, MessageID: "om-progress-1"},
	})

	progress := surface.ActiveExecProgress
	if progress == nil {
		t.Fatal("expected active progress state to survive delete delivery")
	}
	if activeProgressMessageID(progress) != "" {
		t.Fatalf("expected deleted progress card to detach from active segment, got %#v", progress)
	}
	if len(progress.Segments) != 1 || progress.ActiveSegmentID != "segment-1" || activeProgressStartSeq(progress) != 1 {
		t.Fatalf("expected segment identity and window to remain reusable after delete, got %#v", progress)
	}

	app.recordUIEventDelivery(event, []feishu.Operation{
		{Kind: feishu.OperationSendCard, MessageID: "om-progress-2", ProgressCardStartSeq: 7, ProgressCardEndSeq: 12},
	})

	if activeProgressMessageID(progress) != "om-progress-2" {
		t.Fatalf("expected deleted progress segment to accept a new card message, got %#v", progress)
	}
	if len(progress.Segments) != 1 || activeProgressStartSeq(progress) != 7 || progress.Segments[0].EndSeq != 12 {
		t.Fatalf("expected resend to reuse the same segment with updated window, got %#v", progress)
	}
}
