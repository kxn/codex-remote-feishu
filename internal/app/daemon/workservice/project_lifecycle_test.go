package workservice

import (
	"context"
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestProjectPauseWaitsForRealCarrierQuiescence(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	paused, err := s.ProjectPause(context.Background(), token, run, ProjectLifecycleRequest{Key: "pause", ProjectID: "project", ExpectedRevision: 1, SourceMessageID: "lifecycle-human"})
	if err != nil || paused.Status != "accepted" {
		t.Fatalf("pause %#v %v", paused, err)
	}
	if err = s.ReconcileProjectLifecycle(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		project, err := tx.GetProject("project")
		if err != nil {
			return err
		}
		if project.State != work.ProjectPausing {
			t.Fatal("live occupied native carrier declared paused")
		}
		attempt, err := tx.GetAttempt("lifecycle-attempt")
		if err != nil {
			return err
		}
		body := work.NewContent("terminal-outcome", project.ScopeID, "text/plain", []byte("Runtime terminal/quiescent fixture"), s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		return tx.CommitOutcome(workstore.OutcomeRequest{AttemptID: attempt.ID, RunRef: attempt.RunRef, ExpectedAttemptRevision: attempt.Revision, OutcomeRef: body.ID, TerminalState: work.AttemptCompleted, ProcessedInputIDs: attempt.InputIDs, Quiescent: true, TerminalEvidence: []work.SourceRef{{Kind: "native_terminal", ID: "runtime-terminal", Version: "1", EvidenceRef: body.ID}}, Now: s.now()})
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileProjectLifecycle(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		project, err := tx.GetProject("project")
		if err != nil {
			return err
		}
		if project.State != work.ProjectPaused {
			t.Fatalf("quiescent project did not settle: %#v", project)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestProjectLeaderChangeMigratesPendingAuthorityAndRejectsOldGeneration(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	agreement, err := s.AgreementApply(context.Background(), token, run, AgreementApplyRequest{Key: "agreement", ID: "participation", SourceMessageID: "lifecycle-human", SubjectKind: work.AgreementProject, SubjectID: "project", DecisionOwner: work.Actor{Kind: work.ActorRole, ID: "leader"}, Reporting: work.ReportingMilestones, Validity: work.AgreementPersistent})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		return tx.PutDecisionRequest(work.DecisionRequest{ID: "pending", ScopeID: "project-scope", Kind: work.DecisionBusiness, State: work.DecisionOpen, Owner: work.Actor{Kind: work.ActorRole, ID: "leader"}, AllowedActors: []work.Actor{{Kind: work.ActorRole, ID: "leader"}}, AgreementID: agreement.ObjectID, AgreementRevision: 1, RequestGeneration: 1, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := s.ProjectLeaderChange(context.Background(), token, run, ProjectLeaderRequest{Key: "new-leader", ProjectID: "project", ExpectedRevision: 1, LeaderRoleID: "expert", SourceMessageID: "lifecycle-human"})
	if err != nil || changed.Revision != 2 {
		t.Fatalf("leader change %#v %v", changed, err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		old, err := tx.GetDecisionRequest("project-scope", "pending")
		if err != nil {
			return err
		}
		next, err := tx.GetDecisionRequest("project-scope", old.SupersededBy)
		if err != nil {
			return err
		}
		if old.State != work.DecisionSuperseded || next.Owner.ID != "expert" || next.RequestGeneration != 2 || next.AgreementRevision != 2 {
			t.Fatalf("pending authority was not migrated: %#v %#v", old, next)
		}
		a, err := tx.GetAgreement("project-scope", agreement.ObjectID)
		if err != nil {
			return err
		}
		if a.DecisionOwner.ID != "expert" {
			t.Fatal("agreement kept old leader")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DecisionRespond(context.Background(), token, run, DecisionRespondRequest{Key: "stale-response", RequestID: "pending", ExpectedRevision: 1, RequestGeneration: 1, AgreementRevision: 1, Choice: "accept"}); err == nil {
		t.Fatal("old leader/request generation responded after migration")
	}
}
func TestProjectRemovalRequiresExplicitSuccessorAndPreservesBusyCarrier(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	task, err := s.AssignTask(context.Background(), token, run, AssignTaskRequest{Key: "expert-task", Title: "Review", OwnerRoleID: "expert"})
	if err != nil {
		t.Fatal(err)
	}
	req := ProjectMemberRequest{Key: "remove", ProjectID: "project", ExpectedRevision: 1, RoleID: "expert", ExpectedMemberRevision: 1, SourceMessageID: "lifecycle-human"}
	if _, err = s.ProjectMemberRemove(context.Background(), token, run, req); err == nil {
		t.Fatal("unfinished work abandoned without selected successor")
	}
	req.ReplacementRoleID = "leader"
	if _, err = s.ProjectMemberRemove(context.Background(), token, run, req); err != nil {
		t.Fatal(err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		v, err := tx.GetTask(task.ObjectID)
		if err != nil {
			return err
		}
		if v.OwnerRoleID != "leader" || v.AssignmentGeneration != 2 {
			t.Fatalf("task not transferred: %#v", v)
		}
		worker, err := tx.GetWorker("lifecycle-worker")
		if err != nil {
			return err
		}
		if worker.State != work.WorkerBusy || worker.Generation != 1 {
			t.Fatal("member removal rewrote running native carrier")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTrustedHumanCanResumePausedProjectWithoutFakeWorker(t *testing.T) {
	s, _, _ := lifecycleProjectService(t, false)
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		p, err := tx.GetProject("project")
		if err != nil {
			return err
		}
		p.State = work.ProjectPausing
		p.Revision++
		if err = tx.PutProject(p, 1); err != nil {
			return err
		}
		p.State = work.ProjectPaused
		p.Revision++
		return tx.PutProject(p, 2)
	}); err != nil {
		t.Fatal(err)
	}
	req := HumanProjectLifecycleRequest{ProjectLifecycleRequest: ProjectLifecycleRequest{Key: "human-resume", ProjectID: "project", ExpectedRevision: 3, SourceMessageID: "lifecycle-human"}, Action: "resume"}
	if _, err := s.ApplyHumanProjectLifecycle(context.Background(), "impostor", req); err == nil {
		t.Fatal("UI principal spoof accepted")
	}
	result, err := s.ApplyHumanProjectLifecycle(context.Background(), "human", req)
	if err != nil || result.Status != "completed" {
		t.Fatalf("trusted resume %#v %v", result, err)
	}
	replay, err := s.ApplyHumanProjectLifecycle(context.Background(), "human", req)
	if err != nil || !replay.Replay {
		t.Fatalf("human resume replay %#v %v", replay, err)
	}
	if err = s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		op, err := tx.GetOperation(result.OperationID)
		if err != nil {
			return err
		}
		if op.Actor.Kind != work.ActorPrincipal || op.Actor.ID != "human" || op.ContextID != "" {
			t.Fatal("trusted UI fabricated a worker authority")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectPauseIgnoresManagementCarrierButArchiveWaitsForIt(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false)
	if _, err := s.ProjectPause(context.Background(), token, run, ProjectLifecycleRequest{Key: "pause", ProjectID: "project", ExpectedRevision: 1, SourceMessageID: "lifecycle-human"}); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		wc := work.WorkContext{ID: "management-context", ScopeID: "project-scope", RoleID: "leader", Workstream: "control", Purpose: work.PurposeControl, Revision: 1}
		if err := tx.PutContext(wc, 0); err != nil {
			return err
		}
		location := work.ExecutionLocation{ID: "control-scratch", HostID: "host", Cwd: "/isolated-control-scratch", IsolatedContextID: wc.ID, Generation: 1, Revision: 1}
		if err := tx.PutLocation(location, 0); err != nil {
			return err
		}
		worker := work.WorkerSession{ID: "management-worker", ContextID: wc.ID, ScopeID: wc.ScopeID, RoleID: wc.RoleID, Backend: work.BackendCodex, State: work.WorkerReady, Generation: 1, Controller: work.ControllerAutomatic, ControllerGeneration: 1, InstanceID: "management-carrier", InstanceGeneration: 1, ProcessGeneration: 1, LocationID: location.ID, LocationGeneration: 1, Revision: 1}
		if err := tx.PutWorker(worker, 0); err != nil {
			return err
		}
		input := work.Inbox{ID: "management-input", ScopeID: wc.ScopeID, RecipientRoleID: wc.RoleID, RecipientContextID: wc.ID, ControlOfInputID: "lifecycle-input", MessageID: "lifecycle-human", EventID: "management-copy", State: work.InboxPending, Revision: 1}
		if err := tx.PutInbox(input, 0); err != nil {
			return err
		}
		attempt := work.ExecutionAttempt{ID: "management-attempt", ScopeID: wc.ScopeID, ContextID: wc.ID, RunRef: work.RunRef{WorkerID: worker.ID, Generation: 1, RunID: "management-run"}, State: work.AttemptDispatchPending, InputIDs: []string{input.ID}, CommandID: "management-start", Revision: 1}
		op := work.Operation{ID: attempt.CommandID, ScopeID: wc.ScopeID, ContextID: wc.ID, Actor: work.Actor{Kind: work.ActorSystem, ID: "scheduler"}, Kind: "native.start", TargetID: worker.ID, TargetGeneration: 1, IdempotencyKey: attempt.CommandID, RequestHash: "management-start", State: work.OperationPrepared, Revision: 1}
		if err := tx.ClaimAttempt(workstore.ClaimRequest{Attempt: attempt, ExpectedWorkerRevision: 1, ControllerGeneration: 1, InstanceID: worker.InstanceID, InstanceGeneration: 1, ProcessGeneration: 1, LocationGeneration: 1, StartOperation: op, Now: s.now()}); err != nil {
			return err
		}
		attempt.State = work.AttemptRunning
		attempt.Revision++
		if err := tx.PutAttempt(attempt, 1); err != nil {
			return err
		}
		business, err := tx.GetAttempt("lifecycle-attempt")
		if err != nil {
			return err
		}
		body := work.NewContent("business-stopped", "project-scope", "text/plain", []byte("native terminal and quiescent"), s.now())
		if err = tx.PutContent(body); err != nil {
			return err
		}
		return tx.CommitOutcome(workstore.OutcomeRequest{AttemptID: business.ID, RunRef: business.RunRef, ExpectedAttemptRevision: business.Revision, OutcomeRef: body.ID, ProcessedInputIDs: business.InputIDs, TerminalState: work.AttemptCompleted, TerminalEvidence: []work.SourceRef{{Kind: "runtime", ID: "business-native-terminal", Version: "1", EvidenceRef: body.ID}}, Quiescent: true, Now: s.now()})
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileProjectLifecycle(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	var project work.Project
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error { var err error; project, err = tx.GetProject("project"); return err }); err != nil {
		t.Fatal(err)
	}
	if project.State != work.ProjectPaused {
		t.Fatal("control worker prevented business pause from settling")
	}
	if _, err := s.ApplyHumanProjectLifecycle(context.Background(), "human", HumanProjectLifecycleRequest{ProjectLifecycleRequest: ProjectLifecycleRequest{Key: "archive", ProjectID: project.ID, ExpectedRevision: project.Revision, SourceMessageID: "lifecycle-human"}, Action: "archive"}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileProjectLifecycle(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	if err := s.store.Within(context.Background(), func(tx *workstore.Tx) error {
		p, err := tx.GetProject("project")
		if err != nil {
			return err
		}
		if p.State != work.ProjectArchiving {
			t.Fatal("archive falsely declared the running control carrier stopped")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
