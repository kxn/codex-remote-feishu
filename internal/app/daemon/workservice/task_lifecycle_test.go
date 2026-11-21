package workservice

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func lifecycleTask(t *testing.T, s *Service, token, run string, human bool) Result {
	t.Helper()
	actors := map[string]work.ActorKind(nil)
	if human {
		actors = map[string]work.ActorKind{"approval": work.ActorPrincipal}
	}
	task, err := s.AssignTask(context.Background(), token, run, AssignTaskRequest{Key: "lifecycle-task", Title: "Deliver", OwnerRoleID: "leader", Requirements: []work.TaskRequirementRef{{ID: "requirement", ScopeID: "scope", Source: work.SourceRef{Kind: "spec", ID: "spec", Version: "v1", EvidenceRef: "spec-evidence"}, AdoptedBy: "leader", RequiredEvidence: []string{"approval"}, EvidenceActors: actors}}})
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func submitLifecycleEvidence(t *testing.T, s *Service, token, run, taskID, key string, revision int64) Result {
	t.Helper()
	result, err := s.TaskEvidenceSubmit(context.Background(), token, run, TaskEvidenceSubmitRequest{Key: key, TaskID: taskID, ExpectedRevision: revision, RequirementsRevision: 1, AssignmentGeneration: 1, RequirementID: "requirement", RequirementVersion: "v1", Kind: "approval", Source: work.SourceRef{Kind: "git_commit", ID: "commit", Version: "sha", EvidenceRef: "responsible-role-check"}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestTaskLifecycleSubmissionDoesNotCompleteUntilExactAdoption(t *testing.T) {
	s, token, run := knowledgeService(t)
	task := lifecycleTask(t, s, token, run, false)
	started, err := s.TaskStart(context.Background(), token, run, TaskTransitionRequest{Key: "start", TaskID: task.ObjectID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	submission := submitLifecycleEvidence(t, s, token, run, task.ObjectID, "submission", started.Revision)
	if _, err = s.TaskDone(context.Background(), token, run, TaskTransitionRequest{Key: "premature", TaskID: task.ObjectID, ExpectedRevision: started.Revision}); err == nil {
		t.Fatal("submission counted as adopted completion evidence")
	}
	adopted, err := s.TaskEvidenceAdopt(context.Background(), token, run, TaskEvidenceAdoptRequest{Key: "adopt", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: submission.ObjectID})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.TaskEvidenceAdopt(context.Background(), token, run, TaskEvidenceAdoptRequest{Key: "adopt", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: submission.ObjectID})
	if err != nil || !replay.Replay || replay.ObjectID != adopted.ObjectID {
		t.Fatalf("adoption replay %#v %v", replay, err)
	}
	doneReq := TaskTransitionRequest{Key: "done", TaskID: task.ObjectID, ExpectedRevision: started.Revision}
	done, err := s.TaskDone(context.Background(), token, run, doneReq)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.TaskStart(context.Background(), token, run, TaskTransitionRequest{Key: "late", TaskID: task.ObjectID, ExpectedRevision: done.Revision}); err == nil {
		t.Fatal("terminal task resurrected")
	}
	replay, err = s.TaskDone(context.Background(), token, run, doneReq)
	if err != nil || !replay.Replay {
		t.Fatalf("done replay %#v %v", replay, err)
	}
}
func TestTaskHumanAcceptanceCannotBeForgedByRoleReview(t *testing.T) {
	s, token, run := knowledgeService(t)
	task := lifecycleTask(t, s, token, run, true)
	started, err := s.TaskStart(context.Background(), token, run, TaskTransitionRequest{Key: "start", TaskID: task.ObjectID, ExpectedRevision: 1})
	if err != nil {
		t.Fatal(err)
	}
	submission := submitLifecycleEvidence(t, s, token, run, task.ObjectID, "submission", started.Revision)
	if _, err = s.TaskEvidenceAdopt(context.Background(), token, run, TaskEvidenceAdoptRequest{Key: "role-review", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: submission.ObjectID}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TaskDone(context.Background(), token, run, TaskTransitionRequest{Key: "forged-done", TaskID: task.ObjectID, ExpectedRevision: started.Revision}); err == nil {
		t.Fatal("role review became real human approval")
	}
	if _, err = s.TaskReview(context.Background(), token, run, TaskEvidenceAdoptRequest{Key: "unanchored-human", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: submission.ObjectID, SourceMessageID: "human-input"}); err == nil {
		t.Fatal("unanchored human input was treated as version-specific acceptance")
	}
	installReviewReply(t, s, token, run, task.ObjectID, submission.ObjectID)
	if _, err = s.TaskReview(context.Background(), token, run, TaskEvidenceAdoptRequest{Key: "human-review", TaskID: task.ObjectID, ExpectedRevision: started.Revision, SubmissionID: submission.ObjectID, SourceMessageID: "human-input"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TaskDone(context.Background(), token, run, TaskTransitionRequest{Key: "done", TaskID: task.ObjectID, ExpectedRevision: started.Revision}); err != nil {
		t.Fatal(err)
	}
}
func TestTaskCannotAdoptOldAssignmentAndTaskResumePreservesHumanGate(t *testing.T) {
	s, token, run := knowledgeService(t)
	task := lifecycleTask(t, s, token, run, false)
	submission := submitLifecycleEvidence(t, s, token, run, task.ObjectID, "old", 1)
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		v, err := tx.GetTask(task.ObjectID)
		if err != nil {
			return err
		}
		v.AssignmentGeneration++
		v.Revision++
		return tx.PutTask(v, 1)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TaskEvidenceAdopt(context.Background(), token, run, TaskEvidenceAdoptRequest{Key: "old", TaskID: task.ObjectID, ExpectedRevision: 2, SubmissionID: submission.ObjectID}); err == nil {
		t.Fatal("old assignment evidence adopted")
	}
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutPauseGate(work.PauseGate{ID: "human-gate", ScopeID: "scope", TaskID: task.ObjectID, Source: work.GateHumanControl, SourceID: "human", State: work.GateClosed, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TaskPause(context.Background(), token, run, TaskTransitionRequest{Key: "pause", TaskID: task.ObjectID, ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TaskResume(context.Background(), token, run, TaskTransitionRequest{Key: "resume", TaskID: task.ObjectID, ExpectedRevision: 3}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		gates, err := tx.ListPauseGates("scope")
		if err != nil {
			return err
		}
		allowed, err := work.GatesAllowRun("scope", task.ObjectID, gates)
		if err != nil {
			return err
		}
		if allowed {
			t.Fatal("task resume erased human pause")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
