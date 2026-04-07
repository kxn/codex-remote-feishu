package wrapper

import (
	"errors"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

type recordingEventSender struct {
	err     error
	batches [][]agentproto.Event
}

func (s *recordingEventSender) SendEvents(events []agentproto.Event) error {
	if s.err != nil {
		return s.err
	}
	copied := append([]agentproto.Event(nil), events...)
	s.batches = append(s.batches, copied)
	return nil
}

func TestProblemReporterSuppressesDuplicateErrorsWithinWindow(t *testing.T) {
	now := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	sender := &recordingEventSender{}
	reporter := &problemReporter{
		client:       sender,
		now:          func() time.Time { return now },
		dedupeWindow: 5 * time.Second,
		maxRecords:   8,
		recent:       map[string]*problemReportRecord{},
	}
	problem := agentproto.ErrorInfo{
		Code:      "relay_send_server_events_failed",
		Layer:     "wrapper",
		Stage:     "forward_server_events",
		Operation: "codex.stdout",
		Details:   "relay client outbox full",
	}

	reporter.Emit(problem)
	reporter.Emit(problem)

	if len(sender.batches) != 1 {
		t.Fatalf("expected one emitted batch, got %d", len(sender.batches))
	}
	if len(sender.batches[0]) != 1 || sender.batches[0][0].Kind != agentproto.EventSystemError {
		t.Fatalf("expected one system error event, got %#v", sender.batches)
	}
	record := reporter.recent[problemReportKey(problem.Normalize())]
	if record == nil || record.suppressed != 1 {
		t.Fatalf("expected duplicate suppression record, got %#v", record)
	}
}

func TestProblemReporterAllowsSameErrorAfterWindow(t *testing.T) {
	now := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	sender := &recordingEventSender{}
	reporter := &problemReporter{
		client:       sender,
		now:          func() time.Time { return now },
		dedupeWindow: 5 * time.Second,
		maxRecords:   8,
		recent:       map[string]*problemReportRecord{},
	}
	problem := agentproto.ErrorInfo{
		Code:      "relay_send_server_events_failed",
		Layer:     "wrapper",
		Stage:     "forward_server_events",
		Operation: "codex.stdout",
		Details:   "relay client outbox full",
	}

	reporter.Emit(problem)
	now = now.Add(6 * time.Second)
	reporter.Emit(problem)

	if len(sender.batches) != 2 {
		t.Fatalf("expected two emitted batches after dedupe window, got %d", len(sender.batches))
	}
}

func TestProblemReporterKeepsPendingBoundWhenRelaySendFails(t *testing.T) {
	now := time.Date(2026, 4, 7, 15, 0, 0, 0, time.UTC)
	sender := &recordingEventSender{err: errors.New("relay unavailable")}
	reporter := &problemReporter{
		client:       sender,
		now:          func() time.Time { return now },
		dedupeWindow: 5 * time.Second,
		maxRecords:   2,
		recent:       map[string]*problemReportRecord{},
	}

	base := agentproto.ErrorInfo{
		Code:      "relay_send_server_events_failed",
		Layer:     "wrapper",
		Stage:     "forward_server_events",
		Operation: "codex.stdout",
		Details:   "relay client outbox full",
	}
	reporter.Emit(base)
	reporter.Emit(base)
	reporter.Emit(agentproto.ErrorInfo{
		Code:      "relay_send_server_events_failed",
		Layer:     "wrapper",
		Stage:     "forward_server_events",
		Operation: "codex.stdout",
		Details:   "other",
	})
	reporter.Emit(agentproto.ErrorInfo{
		Code:      "relay_send_server_events_failed",
		Layer:     "wrapper",
		Stage:     "forward_server_events",
		Operation: "codex.stdout",
		Details:   "third",
	})

	if len(reporter.pending) != 3 {
		t.Fatalf("expected unique failed reports to remain pending once each, got %#v", reporter.pending)
	}
	if len(reporter.recent) > reporter.maxRecords {
		t.Fatalf("expected reporter cache to stay bounded, got %d", len(reporter.recent))
	}
}
