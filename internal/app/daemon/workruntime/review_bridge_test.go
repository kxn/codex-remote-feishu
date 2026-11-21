package workruntime

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/agentproto"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestReviewBridgePersistsOriginalNativeResultAndWaitsForActualQuiescence(t *testing.T) {
	f := newFixture(t, work.BackendClaude, true)
	f.start()
	const original = "Finding: preserve exact ownership.\nNo source files were modified."
	if err := f.handle(agentproto.Event{Kind: agentproto.EventItemCompleted, ThreadID: "session", TurnID: "turn-1", ItemID: "final-review", ItemKind: "agent_message", Status: "completed", Metadata: map[string]any{"text": original}}); err != nil {
		t.Fatal(err)
	}
	f.driver.inspect = func(request InspectionRequest) (NativeObservation, error) {
		o := stoppedObservation(request)
		o.BackgroundWorkStopped = false
		return o, nil
	}
	terminal := f.durable(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: "completed", TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime})
	if err := f.coordinator.HandleEvent(context.Background(), "instance-1", terminal); err == nil {
		t.Fatal("unknown background was released")
	}
	f.tx(func(tx *workstore.Tx) error {
		result, err := tx.GetReviewResult("attempt")
		if err != nil {
			return err
		}
		body, err := tx.GetContent("scope", result.BodyRef)
		if err != nil {
			return err
		}
		if string(body.Data) != original || result.Delivered || result.Spec.SourceContextID != "review-source" {
			t.Fatal("review result was altered or prematurely delivered")
		}
		if _, err = tx.GetInbox("review-result-input:attempt"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatal("review woke original work without full proof")
		}
		report, err := tx.GetProcessingReport("attempt")
		if err != nil {
			return err
		}
		if len(report.ProcessedInputIDs) != 1 || report.ProcessedInputIDs[0] != "input" {
			t.Fatal("review consumed unrelated input")
		}
		return nil
	})
	f.driver.inspect = nil
	if err := f.coordinator.Reconcile(context.Background(), "attempt"); err != nil {
		t.Fatal(err)
	}
	if err := f.coordinator.HandleEvent(context.Background(), "instance-1", terminal); err != nil {
		t.Fatal(err)
	}
	f.tx(func(tx *workstore.Tx) error {
		result, err := tx.GetReviewResult("attempt")
		if err != nil {
			return err
		}
		if !result.Delivered {
			t.Fatal("quiescent review was never delivered")
		}
		input, err := tx.GetInbox("review-result-input:attempt")
		if err != nil {
			return err
		}
		if input.RecipientContextID != "review-source" || input.State != work.InboxPending {
			t.Fatal("review result crossed context or was auto-consumed")
		}
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerReady {
			t.Fatal("normal read-only review permanently gated")
		}
		return nil
	})
}

func TestReviewFailureReturnsToSourceWithoutSuccessfulFindingsOrConsumption(t *testing.T) {
	for _, status := range []string{"failed", "interrupted", "completed"} {
		t.Run(status, func(t *testing.T) {
			f := newFixture(t, work.BackendClaude, true)
			f.start()
			terminal := f.durable(agentproto.Event{Kind: agentproto.EventTurnCompleted, ThreadID: "session", TurnID: "turn-1", Status: status, TurnCompletionOrigin: agentproto.TurnCompletionOriginRuntime})
			if err := f.coordinator.HandleEvent(context.Background(), "instance-1", terminal); err != nil {
				t.Fatal(err)
			}
			if err := f.coordinator.HandleEvent(context.Background(), "instance-1", terminal); err != nil {
				t.Fatal(err)
			}
			f.tx(func(tx *workstore.Tx) error {
				input, err := tx.GetInbox("review-failure:attempt:input")
				if err != nil {
					return err
				}
				if input.RecipientContextID != "review-source" || input.State != work.InboxPending {
					t.Fatal("hidden review failure was not delivered to its source")
				}
				original, err := tx.GetInbox("input")
				if err != nil {
					return err
				}
				if original.State != work.InboxPending {
					t.Fatal("failed review falsely consumed the human request")
				}
				if _, err = tx.GetReviewResult("attempt"); !errors.Is(err, workstore.ErrNotFound) {
					t.Fatal("failed or empty review invented findings")
				}
				attempt, err := tx.GetAttempt("attempt")
				if err != nil {
					return err
				}
				want := work.AttemptFailed
				if status == "interrupted" {
					want = work.AttemptInterrupted
				}
				if attempt.State != want {
					t.Fatalf("state=%s want=%s", attempt.State, want)
				}
				return nil
			})
		})
	}
}
