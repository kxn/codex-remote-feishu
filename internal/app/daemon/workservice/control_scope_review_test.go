package workservice

import (
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workauth"
	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
	"testing"
	"time"
)

func controlReviewService(t *testing.T) (*Service, string) {
	t.Helper()
	s, _, _ := lifecycleProjectService(t, false)
	err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		context := work.WorkContext{ID: "review-control-context", ScopeID: "project-scope", RoleID: "leader", Purpose: work.PurposeControl, Workstream: "control-review", Revision: 1}
		if e := tx.PutContext(context, 0); e != nil {
			return e
		}
		location := work.ExecutionLocation{ID: "review-control-scratch", HostID: "local", Cwd: "/control-review", IsolatedContextID: context.ID, Generation: 1, Revision: 1}
		if e := tx.PutLocation(location, 0); e != nil {
			return e
		}
		worker := work.WorkerSession{ID: "review-control-worker", ContextID: context.ID, ScopeID: context.ScopeID, RoleID: "leader", Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "review-control-instance", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: location.ID, LocationGeneration: 1, Revision: 1}
		if e := tx.PutWorker(worker, 0); e != nil {
			return e
		}
		original := work.Inbox{ID: "review-business", ScopeID: context.ScopeID, RecipientRoleID: "expert", EventID: "review-business", MessageID: "lifecycle-human", State: work.InboxPending, Revision: 1}
		if e := tx.PutInbox(original, 0); e != nil {
			return e
		}
		input := work.Inbox{ID: "review-control-input", ScopeID: context.ScopeID, RecipientRoleID: "leader", RecipientContextID: context.ID, ControlOfInputID: original.ID, MessageID: original.MessageID, EventID: "review-control", State: work.InboxPending, Revision: 1}
		if e := tx.PutInbox(input, 0); e != nil {
			return e
		}
		attempt := work.ExecutionAttempt{ID: "review-control-attempt", ScopeID: context.ScopeID, ContextID: context.ID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "review-control-run"}, State: work.AttemptDispatchPending, CommandID: "review-control-start", InputIDs: []string{input.ID}, Revision: 1}
		operation := work.Operation{ID: attempt.CommandID, ScopeID: context.ScopeID, ContextID: context.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, RequestHash: "start", IdempotencyKey: attempt.CommandID, State: work.OperationPrepared, Revision: 1}
		if e := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: operation, Now: s.now()}); e != nil {
			return e
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		return tx.PutAttempt(attempt, 1)
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.signer.Issue(workauth.Claims{Purpose: "worker", RoleID: "leader", BotGeneration: 1, ScopeID: "project-scope", ContextID: "review-control-context", WorkerID: "review-control-worker", SessionGeneration: 1, ControllerGeneration: 1}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return s, token
}
func TestReviewControlCannotDiscardUnprocessedBusinessInput(t *testing.T) {
	s, token := controlReviewService(t)
	if _, err := s.ConsumeControlInput(t.Context(), token, "review-control-run", ControlConsumeRequest{Key: "consume", SourceMessageID: "lifecycle-human"}); err == nil {
		t.Fatal("control context discarded original business without management evidence")
	}
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		input, e := tx.GetInbox("review-business")
		if e != nil {
			return e
		}
		if input.State != work.InboxPending {
			t.Fatal("business input lost after rejection")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestReviewControlNeedsSameSourceManagementReceipt(t *testing.T) {
	s, token := controlReviewService(t)
	resumed, err := s.ProjectPause(t.Context(), token, "review-control-run", ProjectLifecycleRequest{Key: "pause", ProjectID: "project", ExpectedRevision: 1, SourceMessageID: "lifecycle-human"})
	if err != nil {
		t.Fatal(err)
	}
	consumed, err := s.ConsumeControlInput(t.Context(), token, "review-control-run", ControlConsumeRequest{Key: "consume", SourceMessageID: "lifecycle-human", ManagementOperationID: resumed.OperationID})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.ConsumeControlInput(t.Context(), token, "review-control-run", ControlConsumeRequest{Key: "consume", SourceMessageID: "lifecycle-human", ManagementOperationID: resumed.OperationID})
	if err != nil || !replay.Replay || replay.ObjectID != consumed.ObjectID {
		t.Fatalf("consume replay %+v %v", replay, err)
	}
	if _, err = s.AssignTask(t.Context(), token, "review-control-run", AssignTaskRequest{Key: "bypass", Title: "business", OwnerRoleID: "leader"}); err == nil {
		t.Fatal("control context acquired business writer after management action")
	}
}
func TestReviewControlStopsAuthorizingAfterProjectArchives(t *testing.T) {
	s, token := controlReviewService(t)
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		project, e := tx.GetProject("project")
		if e != nil {
			return e
		}
		project.State = work.ProjectArchiving
		project.Revision++
		if e = tx.PutProject(project, 1); e != nil {
			return e
		}
		project.State = work.ProjectArchived
		project.Revision++
		return tx.PutProject(project, 2)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProjectInspect(t.Context(), token, "review-control-run", ProjectInspectRequest{ProjectID: "project"}); err == nil {
		t.Fatal("archived project retained active management worker authority")
	}
}
