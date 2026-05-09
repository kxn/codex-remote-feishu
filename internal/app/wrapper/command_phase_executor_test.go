package wrapper

import (
	"context"
	"testing"
	"time"

	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
)

func TestExecuteCommandPhasesWaitsForGateBeforeNextPhase(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writeCh := make(chan []byte, 2)
	tracker := newCommandResponseTracker()
	phases := []runtimeCommandPhase{
		{
			OutboundToChild: [][]byte{[]byte("phase-1\n")},
			ResponseGate: &runtimeCommandResponseGate{
				RequestID:      "req-1",
				RejectProblem:  agentproto.ErrorInfo{Code: "gate_rejected", Message: "gate rejected"},
				Timeout:        time.Second,
				TimeoutProblem: agentproto.ErrorInfo{Code: "gate_timeout", Message: "gate timeout"},
			},
		},
		{
			OutboundToChild: [][]byte{[]byte("phase-2\n")},
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- executeCommandPhases(ctx, writeCh, tracker, "cmd-1", phases, nil)
	}()

	select {
	case line := <-writeCh:
		if string(line) != "phase-1\n" {
			t.Fatalf("first queued frame = %q, want phase-1", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first phase frame")
	}

	select {
	case line := <-writeCh:
		t.Fatalf("second phase should not queue before gate resolves, got %q", line)
	case <-time.After(100 * time.Millisecond):
	}

	tracker.ResolveRequestID("req-1", "")

	select {
	case line := <-writeCh:
		if string(line) != "phase-2\n" {
			t.Fatalf("second queued frame = %q, want phase-2", line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for second phase frame")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("executeCommandPhases: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for phased execution to complete")
	}
}

func TestExecuteCommandPhasesStopsAfterRejectedGate(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	writeCh := make(chan []byte, 2)
	tracker := newCommandResponseTracker()
	phases := []runtimeCommandPhase{
		{
			OutboundToChild: [][]byte{[]byte("phase-1\n")},
			ResponseGate: &runtimeCommandResponseGate{
				RequestID:      "req-1",
				RejectProblem:  agentproto.ErrorInfo{Code: "gate_rejected", Message: "gate rejected"},
				Timeout:        time.Second,
				TimeoutProblem: agentproto.ErrorInfo{Code: "gate_timeout", Message: "gate timeout"},
			},
		},
		{
			OutboundToChild: [][]byte{[]byte("phase-2\n")},
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- executeCommandPhases(ctx, writeCh, tracker, "cmd-1", phases, nil)
	}()

	select {
	case <-writeCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for first phase frame")
	}

	tracker.ResolveRequestID("req-1", "permission mode rejected")

	select {
	case err := <-done:
		problem, ok := err.(agentproto.ErrorInfo)
		if !ok {
			t.Fatalf("expected agentproto.ErrorInfo, got %T (%v)", err, err)
		}
		if problem.Code != "gate_rejected" || problem.Details != "permission mode rejected" {
			t.Fatalf("unexpected rejection problem: %#v", problem)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for phased execution to fail")
	}

	select {
	case line := <-writeCh:
		t.Fatalf("second phase should not queue after rejection, got %q", line)
	case <-time.After(100 * time.Millisecond):
	}
}
