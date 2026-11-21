package workservice

import (
	"testing"

	"github.com/kxn/codex-remote-feishu/internal/app/daemon/workstore"
	"github.com/kxn/codex-remote-feishu/internal/core/work"
)

func TestAgreementIdenticalRestatementCannotAcquireAuthority(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false, "first", "second")
	first := agreementTestActor(t, s, "first")
	req := AgreementApplyRequest{Key: "first", ID: "original", SourceMessageID: "first", SubjectKind: work.AgreementProject, SubjectID: "project", DecisionOwner: first, Reporting: work.ReportingAll, Validity: work.AgreementPersistent}
	if _, err := s.AgreementApply(t.Context(), token, run, req); err != nil {
		t.Fatal(err)
	}
	req.Key, req.ID, req.SourceMessageID = "copy", "copy", "second"
	copy, err := s.AgreementApply(t.Context(), token, run, req)
	if err != nil || copy.ObjectID != "original" || copy.Status == "needs_coordination" {
		t.Fatalf("copy: %+v %v", copy, err)
	}
	req.Key, req.ID, req.ExpectedRevision = "change", copy.ObjectID, copy.Revision
	req.DecisionOwner = work.Actor{Kind: work.ActorRole, ID: "leader"}
	change, err := s.AgreementApply(t.Context(), token, run, req)
	if err != nil || change.Status != "needs_coordination" || change.DecisionOwner != first {
		t.Fatalf("authority changed: %+v %v", change, err)
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		all, e := tx.ListAgreements("project-scope")
		if len(all) != 1 || all[0].Initiator != first {
			t.Fatalf("duplicate authorities: %+v", all)
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
}

func TestParticipationTaskRevisionReissuesCurrentDecisionAndRejectsAbsentOwner(t *testing.T) {
	s, token, run := lifecycleProjectService(t, false, "participant", "second")
	person := agreementTestActor(t, s, "participant")
	if err := s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		return tx.PutTask(work.Task{ID: "review-task", ScopeID: "project-scope", Title: "Original request", OwnerRoleID: "leader", State: work.TaskTodo, RequirementsRevision: 1, AssignmentGeneration: 1, Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	req := AgreementApplyRequest{Key: "initial", ID: "attention", SourceMessageID: "participant", SubjectKind: work.AgreementProject, SubjectID: "project", DecisionOwner: person, ParticipationPoints: []string{"review"}, Reporting: work.ReportingAll, Validity: work.AgreementPersistent, TaskImpacts: []AgreementTaskImpact{{TaskID: "review-task", TaskRevision: 1, Point: "review", Stage: agreementStageBefore, Facts: "Please review before proceeding", Choices: []work.DecisionChoice{{Value: "approve", Label: "Approve"}, {Value: "revise", Label: "Revise"}}}}}
	created, err := s.AgreementApply(t.Context(), token, run, req)
	if err != nil {
		t.Fatal(err)
	}
	overlap := req
	overlap.Key, overlap.ID, overlap.SubjectKind, overlap.SubjectID = "overlap", "another-name", work.AgreementPhase, "design"
	overlap.DecisionOwner = agreementTestActor(t, s, "second")
	if _, err = s.AgreementApply(t.Context(), token, run, overlap); err == nil {
		t.Fatal("different category name bypassed an existing task participation owner")
	}
	var original, current work.DecisionRequest
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		var e error
		original, e = tx.GetDecisionRequest("project-scope", created.TaskOutcomes[0].DecisionRequestID)
		if e != nil {
			return e
		}
		return tx.PutPrincipal(work.Principal{ID: "absent", DisplayName: "Not in this project", Revision: 1}, 0)
	}); err != nil {
		t.Fatal(err)
	}
	move := req
	move.Key, move.ExpectedRevision, move.TaskImpacts = "move", 1, nil
	move.DecisionOwner = work.Actor{Kind: work.ActorPrincipal, ID: "absent"}
	if _, err = s.AgreementApply(t.Context(), token, run, move); err == nil {
		t.Fatal("decision moved to a person without project access")
	}
	if _, err = s.ReviseTask(t.Context(), token, run, ReviseTaskRequest{Key: "revision", TaskID: "review-task", ExpectedRevision: 1, Title: "New user requirements", OwnerRoleID: "leader"}); err != nil {
		t.Fatal(err)
	}
	if err = s.store.Within(t.Context(), func(tx *workstore.Tx) error {
		old, e := tx.GetDecisionRequest("project-scope", original.ID)
		if e != nil {
			return e
		}
		if old.State != work.DecisionSuperseded {
			t.Fatalf("old decision: %+v", old)
		}
		current, e = tx.GetDecisionRequest("project-scope", old.SupersededBy)
		if e != nil {
			return e
		}
		if current.Owner != person || current.Presentation.TaskRequirementsRevision != 2 {
			t.Fatalf("current request: %+v", current)
		}
		gate, e := tx.GetPauseGate(current.ParticipationGateID)
		if gate.State != work.GateClosed {
			t.Fatal("editing released the participation hold")
		}
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if err = respondAgreementTestCard(t, s, original, "participant", "approve"); err == nil {
		t.Fatal("obsolete decision accepted")
	}
	if err = respondAgreementTestCard(t, s, current, "participant", "approve"); err != nil {
		t.Fatal(err)
	}
}
