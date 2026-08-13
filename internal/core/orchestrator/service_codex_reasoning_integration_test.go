package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/adapter/codex"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/control"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestCodexReasoningCompletionFallbackReachesChattyProgressWithoutDuplicates(t *testing.T) {
	tests := []struct {
		name           string
		observeDelta   bool
		wantEventCount int
	}{
		{name: "completion only", wantEventCount: 3},
		{name: "first summary already streamed", observeDelta: true, wantEventCount: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, 8, 13, 15, 0, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			surface := setupAutoWhipSurface(t, svc)
			surface.Backend = agentproto.BackendCodex
			surface.Verbosity = state.SurfaceVerbosityChatty
			startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

			translator := codex.NewTranslator("inst-1")
			if tt.observeDelta {
				observed, err := translator.ObserveServer([]byte(`{"method":"item/reasoning/summaryTextDelta","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"reason-1","summaryIndex":0,"delta":"**Planning the fix**"}}`))
				if err != nil {
					t.Fatalf("observe reasoning delta: %v", err)
				}
				applyCanonicalEvents(svc, observed.Events)
			}

			completed, err := translator.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"reason-1","type":"reasoning","summary":["**Planning the fix**","**Checking regressions**"],"content":[]}}}`))
			if err != nil {
				t.Fatalf("observe reasoning completion: %v", err)
			}
			if len(completed.Events) != tt.wantEventCount {
				t.Fatalf("canonical completion events = %#v, want %d", completed.Events, tt.wantEventCount)
			}
			lastProgress := applyCanonicalEvents(svc, completed.Events)
			if lastProgress == nil {
				t.Fatal("reasoning completion did not emit final chatty progress")
			}
			if len(lastProgress.Timeline) != 2 {
				t.Fatalf("reasoning timeline = %#v, want two summary rows", lastProgress.Timeline)
			}
			if lastProgress.Timeline[0].Summary != "Planning the fix" || lastProgress.Timeline[1].Summary != "Checking regressions" {
				t.Fatalf("unexpected reasoning timeline: %#v", lastProgress.Timeline)
			}
			for _, item := range lastProgress.Timeline {
				if item.Status != "completed" {
					t.Fatalf("reasoning row did not finalize: %#v", lastProgress.Timeline)
				}
			}
		})
	}
}

func TestCodexReasoningCompletionFallbackRespectsHiddenAndVerboseProjection(t *testing.T) {
	tests := []struct {
		verbosity   state.SurfaceVerbosity
		wantSummary string
	}{
		{verbosity: state.SurfaceVerbosityQuiet},
		{verbosity: state.SurfaceVerbosityNormal},
		{verbosity: state.SurfaceVerbosityVerbose, wantSummary: "Checking regressions"},
	}

	for _, tt := range tests {
		t.Run(string(tt.verbosity), func(t *testing.T) {
			now := time.Date(2026, 8, 13, 15, 10, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			surface := setupAutoWhipSurface(t, svc)
			surface.Backend = agentproto.BackendCodex
			surface.Verbosity = tt.verbosity
			startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续", "turn-1")

			translator := codex.NewTranslator("inst-1")
			completed, err := translator.ObserveServer([]byte(`{"method":"item/completed","params":{"threadId":"thread-1","turnId":"turn-1","item":{"id":"reason-1","type":"reasoning","summary":["**Planning the fix**","**Checking regressions**"],"content":[]}}}`))
			if err != nil {
				t.Fatalf("observe reasoning completion: %v", err)
			}
			lastProgress := applyCanonicalEvents(svc, completed.Events)
			if tt.wantSummary == "" {
				if lastProgress != nil || surface.ActiveExecProgress != nil || surface.ActiveReasoning != nil {
					t.Fatalf("%s projected hidden reasoning: progress=%#v surface=%#v", tt.verbosity, lastProgress, surface)
				}
				return
			}
			if lastProgress == nil || len(lastProgress.Timeline) != 1 {
				t.Fatalf("verbose fallback progress = %#v, want one latest row", lastProgress)
			}
			item := lastProgress.Timeline[0]
			if item.Summary != tt.wantSummary || item.Status != "completed" {
				t.Fatalf("unexpected verbose fallback row: %#v", item)
			}
		})
	}
}

func applyCanonicalEvents(svc *Service, events []agentproto.Event) *control.ExecCommandProgress {
	var lastProgress *control.ExecCommandProgress
	for _, event := range events {
		for _, projected := range svc.ApplyAgentEvent("inst-1", event) {
			if projected.ExecCommandProgress != nil {
				lastProgress = projected.ExecCommandProgress
			}
		}
	}
	return lastProgress
}
