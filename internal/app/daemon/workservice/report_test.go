package workservice

import (
	"context"
	"errors"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestReportPreservesWorkDuringPauseWithoutFreeingWorker(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		return tx.PutPauseGate(work.PauseGate{ID: "pause", ScopeID: "scope", Source: work.GateProject, SourceID: "human", State: work.GateClosed, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AssignTask(ctx, token, run, AssignTaskRequest{Key: "no", Title: "should block", OwnerRoleID: "leader"}); !errors.Is(err, workauth.ErrUnauthorized) {
		t.Fatalf("new work under pause: %v", err)
	}
	req := ReportOutcomeRequest{Key: "save", Summary: "Saved partial research", Checkpoint: "Need to compare two options"}
	result, err := s.ReportOutcome(ctx, token, run, req)
	if err != nil || result.Revision != 1 {
		t.Fatalf("save=%+v err=%v", result, err)
	}
	replayed, err := s.ReportOutcome(ctx, token, run, req)
	if err != nil || !replayed.Replay {
		t.Fatalf("report replay=%+v err=%v", replayed, err)
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerBusy {
			t.Fatalf("report released worker: %s", worker.State)
		}
		attempt, err := tx.GetAttempt("attempt")
		if err != nil {
			return err
		}
		if attempt.State != work.AttemptRunning {
			t.Fatalf("report terminated attempt: %s", attempt.State)
		}
		report, err := tx.GetProcessingReport(attempt.ID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent("scope", report.OutcomeRef)
		if err != nil {
			return err
		}
		if string(body.Data) != req.Summary {
			t.Fatalf("body=%s", body.Data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMessageIsDurableAndReportCannotConsumeUnclaimedInput(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	result, err := s.SendMessage(ctx, token, run, SendMessageRequest{Key: "note", RecipientRoleID: "leader", ConversationID: "discussion", Body: "A follow-up arrived during work"})
	if err != nil {
		t.Fatal(err)
	}
	var inputID string
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		message, err := tx.GetMessage(result.ObjectID)
		if err != nil {
			return err
		}
		body, err := tx.GetContent("scope", message.BodyRef)
		if err != nil {
			return err
		}
		if string(body.Data) != "A follow-up arrived during work" {
			t.Fatalf("body=%s", body.Data)
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		inputID = inputs[0].ID
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReportOutcome(ctx, token, run, ReportOutcomeRequest{Key: "bad consumption", Summary: "Done", ProcessedInputIDs: []string{inputID}}); err == nil {
		t.Fatal("late input incorrectly consumed")
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		if _, err := tx.GetProcessingReport("attempt"); !errors.Is(err, workstore.ErrNotFound) {
			t.Fatalf("failed report survived: %v", err)
		}
		input, err := tx.GetInbox(inputID)
		if err != nil {
			return err
		}
		if input.State != work.InboxPending {
			t.Fatalf("late input=%s", input.State)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWorkDispositionIsExplicitAndDoesNotAcceptTaskOrReleaseExecution(t *testing.T) {
	for _, disposition := range []string{"complete", "unfinished", "unknown", ""} {
		t.Run(disposition, func(t *testing.T) {
			s, token, run := runningService(t)
			task, err := s.AssignTask(t.Context(), token, run, AssignTaskRequest{Key: "authorized", Title: "An authorized task", OwnerRoleID: "leader"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ReportOutcome(t.Context(), token, run, ReportOutcomeRequest{Key: "bad", Summary: "Guess", WorkDisposition: "probably-finished"}); err == nil {
				t.Fatal("invalid disposition accepted")
			}
			if _, err = s.ReportOutcome(t.Context(), token, run, ReportOutcomeRequest{Key: "report", Summary: "Actual report", WorkDisposition: disposition}); err != nil {
				t.Fatal(err)
			}
			err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
				r, e := tx.GetProcessingReport("attempt")
				if e != nil {
					return e
				}
				if r.WorkDisposition != disposition {
					t.Fatal("lost explicit disposition")
				}
				current, e := tx.GetTask(task.ObjectID)
				if e != nil {
					return e
				}
				if current.State == work.TaskDone {
					t.Fatal("agent report accepted task")
				}
				attempt, e := tx.GetAttempt("attempt")
				if e != nil {
					return e
				}
				if attempt.State != work.AttemptRunning {
					t.Fatal("report released running attempt")
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
