package workservice

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestPreparedLocationStagesHandoffAndContinuationWithoutReleasingWorker(t *testing.T) {
	s, token, run := runningService(t)
	ctx := context.Background()
	root := t.TempDir()
	prepared := filepath.Join(root, "agent-created")
	if err := os.Mkdir(prepared, 0700); err != nil {
		t.Fatal(err)
	}
	err := s.store.Within(ctx, func(tx *workstore.Tx) error {
		if err := tx.PutLocation(work.ExecutionLocation{ID: "human-choice", HostID: "host", Cwd: root, Generation: 1, Revision: 1}, 0); err != nil {
			return err
		}
		body := work.NewContent("choice-source", "scope", "application/json", []byte(`{"selected_by":"human"}`), s.now())
		if err := tx.PutContent(body); err != nil {
			return err
		}
		return tx.PutExecutionPreference(workstore.ExecutionPreference{ScopeID: "scope", SelectedLocationIDs: []string{"human-choice"}, DefaultLocationID: "human-choice", SelectedBy: work.Actor{Kind: work.ActorPrincipal, ID: "human"}, SourceRef: body.ID, Revision: 1}, 0)
	})
	if err != nil {
		t.Fatal(err)
	}
	req := ExecutionLocationRequest{Key: "prepared-work", RootLocationID: "human-choice", Path: prepared}
	result, err := s.UseExecutionLocation(ctx, token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "accepted_for_next_execution" {
		t.Fatalf("premature handoff result: %+v", result)
	}
	err = s.store.Within(ctx, func(tx *workstore.Tx) error {
		worker, err := tx.GetWorker("worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerBusy || worker.Generation != 1 || worker.LocationID != "" {
			t.Fatalf("staging moved live owner: %+v", worker)
		}
		if slots, e := tx.ActiveSlotCount(); e != nil || slots != 1 {
			t.Fatalf("staging released active occupancy: %d %v", slots, e)
		}
		desired, err := tx.GetDesiredExecutionLocation("context")
		if err != nil {
			return err
		}
		if desired.AuthorizingAttemptID != "attempt" || desired.LocationID != result.ObjectID {
			t.Fatalf("wrong authorization: %+v", desired)
		}
		inputs, err := tx.ListInbox("scope")
		if err != nil {
			return err
		}
		if len(inputs) != 1 || inputs[0].RecipientContextID != "context" || inputs[0].State != work.InboxPending {
			t.Fatalf("missing exact continuation: %+v", inputs)
		}
		message, err := tx.GetMessage(inputs[0].MessageID)
		if err != nil {
			return err
		}
		if message.Source.Kind != work.ActorSystem {
			t.Fatal("handoff invented a human request")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(prepared); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.UseExecutionLocation(ctx, token, run, req)
	if err != nil || !replayed.Replay || replayed.Status != result.Status || replayed.ObjectID != result.ObjectID {
		t.Fatalf("replay reran filesystem effects: %+v %v", replayed, err)
	}
}

func TestLocationUseCannotSelectUnapprovedRoot(t *testing.T) {
	s, token, run := runningService(t)
	if _, err := s.UseExecutionLocation(context.Background(), token, run, ExecutionLocationRequest{Key: "guess", RootLocationID: "unselected", Path: t.TempDir()}); err == nil {
		t.Fatal("model selected an unapproved resource")
	}
}
