package orchestrator

import (
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/state"
)

func TestIsAutoContinueEligibleProblem(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		problem *agentproto.ErrorInfo
		want    bool
	}{
		{
			name: "nil",
			want: false,
		},
		{
			name: "response stream disconnected",
			problem: &agentproto.ErrorInfo{
				Code:  codexProblemResponseStreamDisconnected,
				Layer: "codex",
			},
			want: true,
		},
		{
			name: "too many failed attempts",
			problem: &agentproto.ErrorInfo{
				Code:  codexProblemResponseTooManyFailedAttempts,
				Layer: "codex",
			},
			want: true,
		},
		{
			name: "server overloaded",
			problem: &agentproto.ErrorInfo{
				Code:  codexProblemServerOverloaded,
				Layer: "gateway",
			},
			want: true,
		},
		{
			name: "other runtime error",
			problem: &agentproto.ErrorInfo{
				Code:  codexProblemOther,
				Layer: "codex",
			},
			want: true,
		},
		{
			name: "unknown code",
			problem: &agentproto.ErrorInfo{
				Code:  "totally_new_error",
				Layer: "codex",
			},
			want: false,
		},
		{
			name: "transport layer stays out",
			problem: &agentproto.ErrorInfo{
				Code:  codexProblemOther,
				Layer: "wrapper",
			},
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isAutoContinueEligibleProblem(tc.problem); got != tc.want {
				t.Fatalf("isAutoContinueEligibleProblem(%#v) = %t, want %t", tc.problem, got, tc.want)
			}
		})
	}
}

func TestKnownCodexProblemFamiliesDispatchAutoContinueImmediately(t *testing.T) {
	tests := []struct {
		name         string
		code         string
		message      string
		details      string
		errorMessage string
	}{
		{
			name:         "response stream disconnected",
			code:         codexProblemResponseStreamDisconnected,
			message:      "stream disconnected before completion",
			errorMessage: "stream disconnected before completion",
		},
		{
			name:         "too many failed attempts",
			code:         codexProblemResponseTooManyFailedAttempts,
			message:      "exceeded retry limit, last status: 429 Too Many Requests",
			errorMessage: "exceeded retry limit, last status: 429 Too Many Requests",
		},
		{
			name:         "server overloaded",
			code:         codexProblemServerOverloaded,
			message:      "Selected model is at capacity. Please try a different model.",
			errorMessage: "Selected model is at capacity. Please try a different model.",
		},
		{
			name:         "other upstream failure",
			code:         codexProblemOther,
			message:      "unexpected status 502 Bad Gateway: Upstream service temporarily unavailable",
			errorMessage: "unexpected status 502 Bad Gateway: Upstream service temporarily unavailable",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 4, 27, 12, 5, 0, 0, time.UTC)
			svc := newServiceForTest(&now)
			surface := setupAutoWhipSurface(t, svc)
			surface.AutoWhip.Enabled = false
			surface.AutoContinue.Enabled = true

			startRemoteTurnForAutoWhipTest(t, svc, "msg-1", "继续处理", "turn-1")
			events := completeRemoteTurnWithFinalText(t, svc, "turn-1", "failed", tc.errorMessage, "", &agentproto.ErrorInfo{
				Code:      tc.code,
				Layer:     "codex",
				Stage:     "runtime_error",
				Message:   tc.message,
				Details:   tc.details,
				ThreadID:  "thread-1",
				TurnID:    "turn-1",
				Retryable: false,
			})

			episode := surface.AutoContinue.Episode
			if episode == nil {
				t.Fatal("expected autocontinue episode")
			}
			if episode.State != state.AutoContinueEpisodeRunning || episode.AttemptCount != 1 || episode.ConsecutiveDryFailureCount != 1 {
				t.Fatalf("expected immediate first autocontinue attempt, got %#v", episode)
			}
			if surface.ActiveQueueItemID == "" {
				t.Fatalf("expected immediate autocontinue dispatch to occupy active queue")
			}
			active := surface.QueueItems[surface.ActiveQueueItemID]
			if active == nil || active.SourceKind != state.QueueItemSourceAutoContinue || active.AutoContinueEpisodeID != episode.EpisodeID {
				t.Fatalf("expected autocontinue queue item to dispatch, got %#v", active)
			}
			var sawTurnFailedNotice bool
			var sawAutoContinueCard bool
			var sawAutoContinuePrompt bool
			for _, event := range events {
				if event.Notice != nil && event.Notice.Code == "turn_failed" {
					sawTurnFailedNotice = true
				}
				if event.PageView != nil && event.PageView.TrackingKey == episode.EpisodeID {
					sawAutoContinueCard = true
				}
				if event.Command != nil && event.Command.Kind == agentproto.CommandPromptSend && len(event.Command.Prompt.Inputs) == 1 && event.Command.Prompt.Inputs[0].Text == autoContinuePromptText {
					sawAutoContinuePrompt = true
				}
			}
			if sawTurnFailedNotice {
				t.Fatalf("expected autocontinue path to suppress direct turn_failed notice, got %#v", events)
			}
			if !sawAutoContinueCard || !sawAutoContinuePrompt {
				t.Fatalf("expected autocontinue card plus prompt dispatch, got %#v", events)
			}
		})
	}
}
